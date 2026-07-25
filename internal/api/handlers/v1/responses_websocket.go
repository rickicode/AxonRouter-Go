package v1

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/tidwall/gjson"
)

const codexResponsesWebsocketBetaHeader = "responses_websockets=2026-02-06"

// ResponsesWebsocket upgrades GET /v1/responses to a WebSocket and proxies it
// to Codex's Responses WebSocket endpoint. The first client message must
// include a model field so the gateway can resolve credentials and a backend
// upstream connection.
func (h *Handler) ResponsesWebsocket(c *gin.Context) {
	clientConn, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		logging.Logger.Debug("responses websocket accept failed", "error", err.Error())
		return
	}
	defer func() {
		_ = clientConn.Close(websocket.StatusInternalError, "handler exiting")
	}()

	ctx := c.Request.Context()

	// Wait for the first client message so we can resolve the model/connection.
	msgType, firstMsg, err := clientConn.Read(ctx)
	if err != nil {
		_ = clientConn.Close(websocket.StatusGoingAway, "failed to read first message")
		return
	}
	if len(firstMsg) == 0 {
		_ = clientConn.Close(websocket.StatusUnsupportedData, "empty first message")
		return
	}

	model := strings.TrimSpace(gjson.GetBytes(firstMsg, "model").String())
	if model == "" {
		_ = clientConn.Close(websocket.StatusUnsupportedData, "model is required")
		return
	}

	if !h.isModelAllowed(ctx, model) {
		_ = clientConn.Close(websocket.StatusPolicyViolation, "model not allowed for this API key")
		return
	}

	provider, modelName := executor.SplitModel(model)
	if provider == "" {
		provider = "cx"
		modelName = model
	}
	if provider != "cx" {
		_ = clientConn.Close(websocket.StatusUnsupportedData, "websocket responses are only supported for Codex models")
		return
	}

	sessionID := h.sessionIDForAffinity(c, provider, modelName, firstMsg)
	conn, err := h.getConnection(ctx, provider, modelName, sessionID)
	if err != nil {
		_ = clientConn.Close(websocket.StatusServiceRestart, "no available connection")
		return
	}

	wsURL, err := codexResponsesWebsocketURL(conn.BaseURL)
	if err != nil {
		logging.Logger.Error("invalid codex websocket base url", "base_url", conn.BaseURL, "error", err.Error())
		_ = clientConn.Close(websocket.StatusInternalError, "invalid upstream websocket url")
		return
	}

	headers := codexWebsocketHeaders(c.Request, conn)
	upConn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: headers,
	})
	if err != nil {
		logging.Logger.Warn("codex websocket dial failed", "url", wsURL, "error", err.Error())
		_ = clientConn.Close(websocket.StatusBadGateway, "upstream websocket connection failed")
		return
	}
	defer func() {
		_ = upConn.Close(websocket.StatusGoingAway, "closing")
	}()

	// Forward the message that carried the model selection.
	if err := upConn.Write(ctx, msgType, firstMsg); err != nil {
		_ = clientConn.Close(websocket.StatusBadGateway, "failed to forward first message")
		return
	}

	// Bidirectional relay. The first error from either direction ends the session.
	errCh := make(chan error, 2)
	go relayWebsocketMessages(ctx, clientConn, upConn, errCh)
	go relayWebsocketMessages(ctx, upConn, clientConn, errCh)

	select {
	case <-ctx.Done():
	case <-errCh:
	}
}

// codexResponsesWebsocketURL converts a Codex HTTP base URL to the WebSocket
// URL used for the Responses streaming endpoint.
func codexResponsesWebsocketURL(base string) (string, error) {
	if strings.TrimSpace(base) == "" {
		base = "https://chatgpt.com/backend-api/codex/responses"
	}
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return "", err
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
		// already websocket
	default:
		return "", http.ErrNotSupported
	}
	if !strings.HasSuffix(u.Path, "/responses") {
		u.Path = strings.TrimSuffix(u.Path, "/") + "/responses"
	}
	return u.String(), nil
}

// codexWebsocketHeaders builds the headers used to dial the upstream Codex
// Responses websocket. It mirrors the HTTP Codex executor's Authorization,
// User-Agent, and beta-feature headers.
func codexWebsocketHeaders(r *http.Request, conn *Connection) http.Header {
	h := make(http.Header)

	token := conn.AccessToken
	if token == "" {
		token = conn.APIKey
	}
	if token != "" {
		h.Set("Authorization", "Bearer "+token)
	}

	ua := r.UserAgent()
	if ua == "" {
		ua = "codex-tui/0.135.0 (Mac OS 26.5.0; arm64) iTerm.app/3.6.10 (codex-tui; 0.135.0)"
	}
	h.Set("User-Agent", ua)
	h.Set("OpenAI-Beta", codexResponsesWebsocketBetaHeader)
	h.Set("Origin", "https://chatgpt.com")
	h.Set("Originator", "codex-tui")

	if strings.Contains(ua, "Mac OS") {
		h.Set("Session_id", uuid.NewString())
	}

	if accountID := codexAccountIDFromConnection(conn); accountID != "" {
		h.Set("Chatgpt-Account-Id", accountID)
	}

	return h
}

// codexAccountIDFromConnection extracts the ChatGPT account id from connection
// metadata or the OAuth access token.
func codexAccountIDFromConnection(conn *Connection) string {
	if psd := conn.ProviderSpecificData; psd != "" {
		if v := gjson.Get(psd, "accountId").String(); v != "" {
			return v
		}
		if v := gjson.Get(psd, "workspaceId").String(); v != "" {
			return v
		}
	}
	if conn.AccessToken != "" {
		return executor.CodexAccountIDFromToken(conn.AccessToken)
	}
	return ""
}

// relayWebsocketMessages copies messages from src to dst until an error or
// context cancellation. The first error is sent to errCh.
func relayWebsocketMessages(ctx context.Context, dst, src *websocket.Conn, errCh chan<- error) {
	for {
		typ, data, err := src.Read(ctx)
		if err != nil {
			errCh <- err
			return
		}
		if err := dst.Write(ctx, typ, data); err != nil {
			errCh <- err
			return
		}
	}
}
