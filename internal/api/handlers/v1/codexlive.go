package v1

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

const (
	defaultCodexLiveModel = "cx/gpt-live-1-codex"
	codexLiveUpstreamURL  = "https://chatgpt.com/backend-api/codex/realtime/calls?intent=quicksilver&architecture=avas"
	codexLiveSidebandBase = "wss://api.openai.com/v1"
	codexLiveMaxBodySize  = 16 << 20
	codexLiveSessionTTL   = time.Hour
	codexLiveResponseSize = 32 << 20
)

var (
	codexLiveProtocolHeaders = []string{
		"OpenAI-Alpha",
		"X-Session-Id",
		"Session-Id",
		"Thread-Id",
		"Originator",
		"X-Oai-Attestation",
	}
	codexLiveCallIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
)

var errCodexLiveBodyTooLarge = errors.New("codex live request body too large")

// codexLiveSession represents a single live call sideband session.
type codexLiveSession struct {
	callID    string
	connID    string
	connToken string
	model     string
	createdAt time.Time
}

// newCodexLiveSessionStore creates a session store. The optional database
// argument is used by NewHandler in production; test helpers keep it nil to
// remain lightweight. See codexlive_session.go for the persistence-aware store
// implementation.
func newCodexLiveSessionStore(database ...*sql.DB) *codexLiveSessionStore {
	return newCodexLiveSessionStoreImpl(database...)
}

// CodexLive handles POST /v1/live and POST /v1/realtime/calls. It forwards the
// WebRTC SDP bootstrap request to the Codex realtime calls endpoint using a
// selected Codex connection.
func (h *Handler) CodexLive(c *gin.Context) {
	body, contentType, err := h.readCodexLiveBody(c)
	if err != nil {
		if errors.Is(err, errCodexLiveBodyTooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": gin.H{
				"message": err.Error(),
				"type":    "invalid_request_error",
			}})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"message": err.Error(),
			"type":    "invalid_request_error",
		}})
		return
	}

	model := extractCodexLiveModel(body, contentType)
	if model == "" {
		model = defaultCodexLiveModel
	}
	if !h.isModelAllowed(c.Request.Context(), model) {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{
			"message": "model not allowed for this API key",
			"type":    "invalid_request_error",
		}})
		return
	}

	provider, modelName := executor.SplitModel(model)
	if provider == "" {
		provider = "cx"
		modelName = model
	}

	sessionID := h.sessionIDForAffinity(c, provider, modelName, body)
	conn, err := h.getConnection(c.Request.Context(), provider, modelName, sessionID)
	if err != nil {
		logUnavailable(c, "no available Codex connection", model)
		return
	}

	ctx := c.Request.Context()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexLiveUpstreamURL, bytes.NewReader(body))
	if err != nil {
		logging.Logger.Warn("codex live request build failed", "error", err.Error())
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to build upstream request"})
		return
	}
	req.Header = h.codexLiveUpstreamHeaders(c.Request, conn)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	h.trackDevice(c)
	h.logRequest(c, &usage.LogEntry{
		ApiKeyID:       c.GetString("api_key_id"),
		ConnectionID:   conn.ID,
		ProviderTypeID: provider,
		ModelID:        model,
		ApiType:        apiTypeFromPath(c.Request.URL.Path),
		Modality:       modalityFromPath(c.Request.URL.Path),
		Stream:         false,
	})

	client := http.DefaultClient
	if h.codexLiveHTTPClient != nil {
		client = h.codexLiveHTTPClient
	}
	resp, err := client.Do(req)
	if err != nil {
		logging.Logger.Warn("codex live upstream request failed", "error", err.Error())
		c.JSON(http.StatusBadGateway, gin.H{"error": "Codex live upstream unavailable"})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, codexLiveResponseSize))
	if err != nil {
		logging.Logger.Warn("codex live response read failed", "error", err.Error())
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read Codex live response"})
		return
	}

	callID := extractCodexLiveCallID(resp.Header.Get("Location"))
	if callID != "" {
		token := conn.AccessToken
		if token == "" {
			token = conn.APIKey
		}
		h.codexLiveSessions.put(callID, conn.ID, token, model)
	}

	for _, name := range []string{"Content-Type", "Location"} {
		if v := resp.Header.Get(name); v != "" {
			c.Header(name, v)
		}
	}
	c.Status(resp.StatusCode)
	if _, err := c.Writer.Write(respBody); err != nil {
		logging.Logger.Debug("codex live response write failed", "error", err.Error())
	}
}

// CodexLiveSideband handles GET /v1/live/:call_id, GET /v1/realtime/calls/:call_id,
// and GET /v1/realtime. It upgrades the client connection to WebSocket and relays
// frames bidirectionally to the Codex live sideband endpoint.
func (h *Handler) CodexLiveSideband(c *gin.Context) {
	base := codexLiveSidebandBase
	if h.codexLiveSidebandBaseURL != "" {
		base = h.codexLiveSidebandBaseURL
	}
	callID, sidebandBase, ok := codexLiveSidebandTarget(c, base)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"message": "invalid Codex live call ID",
			"type":    "invalid_request_error",
		}})
		return
	}

	sess, ok := h.codexLiveSessions.get(callID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{
			"message": "Codex live session not found",
			"type":    "invalid_request_error",
		}})
		return
	}

	provider, _ := executor.SplitModel(sess.model)
	if provider == "" {
		provider = "cx"
	}
	conn, err := h.getConnection(c.Request.Context(), provider, "", "")
	if err != nil {
		token := sess.connToken
		if token == "" {
			logUnavailable(c, "Codex live session connection unavailable", sess.model)
			return
		}
		conn = &Connection{
			ID:          sess.connID,
			Provider:    "cx",
			AccessToken: token,
			APIKey:      "",
		}
	}

	clientConn, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		logging.Logger.Debug("codex live sideband accept failed", "error", err.Error())
		return
	}
	defer func() {
		_ = clientConn.Close(websocket.StatusInternalError, "handler exiting")
	}()

	ctx := c.Request.Context()
	upURL, err := codexLiveSidebandUpstreamURL(sidebandBase, callID)
	if err != nil {
		logging.Logger.Warn("codex live sideband url invalid", "error", err.Error())
		_ = clientConn.Close(websocket.StatusInternalError, "invalid upstream url")
		return
	}

	headers := h.codexLiveUpstreamHeaders(c.Request, conn)
	upConn, _, err := websocket.Dial(ctx, upURL, &websocket.DialOptions{
		HTTPHeader: headers,
	})
	if err != nil {
		logging.Logger.Warn("codex live sideband dial failed", "url", upURL, "error", err.Error())
		_ = clientConn.Close(websocket.StatusBadGateway, "upstream websocket unavailable")
		return
	}
	defer func() {
		_ = upConn.Close(websocket.StatusGoingAway, "closing")
	}()

	if err := h.relayCodexLiveSideband(ctx, clientConn, upConn); err != nil {
		logging.Logger.Debug("codex live sideband relay closed", "error", err.Error())
	}
	h.codexLiveSessions.delete(callID)
}

func (h *Handler) relayCodexLiveSideband(ctx context.Context, clientConn, upConn *websocket.Conn) error {
	errCh := make(chan error, 2)
	go codexLiveCopyWebSocket(ctx, upConn, clientConn, errCh)
	go codexLiveCopyWebSocket(ctx, clientConn, upConn, errCh)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func codexLiveCopyWebSocket(ctx context.Context, dst, src *websocket.Conn, errCh chan<- error) {
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

func (h *Handler) readCodexLiveBody(c *gin.Context) ([]byte, string, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, codexLiveMaxBodySize)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		if strings.Contains(err.Error(), "http: request body too large") {
			return nil, "", fmt.Errorf("%w (max %d bytes)", errCodexLiveBodyTooLarge, codexLiveMaxBodySize)
		}
		return nil, "", err
	}
	if len(raw) > codexLiveMaxBodySize {
		return nil, "", fmt.Errorf("%w (max %d bytes)", errCodexLiveBodyTooLarge, codexLiveMaxBodySize)
	}

	contentType := strings.TrimSpace(c.GetHeader("Content-Type"))
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err == nil && strings.EqualFold(mediaType, "multipart/form-data") {
		return normalizeCodexLiveMultipart(raw, params["boundary"])
	}
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/json"
	}
	return raw, contentType, nil
}

func normalizeCodexLiveMultipart(raw []byte, boundary string) ([]byte, string, error) {
	if boundary == "" {
		return nil, "", errors.New("codex live multipart boundary is missing")
	}
	reader := multipart.NewReader(bytes.NewReader(raw), boundary)
	var sdp *string
	var session json.RawMessage
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("failed to parse Codex live multipart body: %w", err)
		}
		partBody, err := io.ReadAll(part)
		_ = part.Close()
		if err != nil {
			return nil, "", fmt.Errorf("failed to read Codex live multipart field: %w", err)
		}
		switch part.FormName() {
		case "sdp":
			s := string(partBody)
			sdp = &s
		case "session":
			if !json.Valid(partBody) {
				return nil, "", errors.New("codex live session field must contain valid JSON")
			}
			session = append(json.RawMessage(nil), partBody...)
		}
	}
	if sdp == nil {
		return nil, "", errors.New("codex live multipart body requires an sdp field")
	}

	payload := struct {
		SDP     string          `json:"sdp"`
		Session json.RawMessage `json:"session,omitempty"`
	}{
		SDP:     *sdp,
		Session: session,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("failed to encode Codex live request: %w", err)
	}
	return encoded, "application/json", nil
}

func extractCodexLiveModel(body []byte, contentType string) string {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if strings.EqualFold(mediaType, "application/sdp") || strings.EqualFold(mediaType, "text/plain") {
		return defaultCodexLiveModel
	}
	var payload struct {
		Model   string `json:"model"`
		Session struct {
			Model string `json:"model"`
		} `json:"session"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	if m := strings.TrimSpace(payload.Session.Model); m != "" {
		return m
	}
	return strings.TrimSpace(payload.Model)
}

func logUnavailable(c *gin.Context, message, model string) {
	logging.Logger.Warn(message, "model", model)
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{
		"message": message,
		"type":    "server_error",
	}})
}

func (h *Handler) codexLiveUpstreamHeaders(r *http.Request, conn *Connection) http.Header {
	hdr := make(http.Header)

	token := conn.AccessToken
	if token == "" {
		token = conn.APIKey
	}
	if token != "" {
		hdr.Set("Authorization", "Bearer "+token)
	}

	ua := r.UserAgent()
	if ua == "" {
		ua = executor.DefaultCodexUserAgent
	}
	hdr.Set("User-Agent", ua)
	hdr.Set("Originator", "codex-tui")
	hdr.Set("Origin", "https://chatgpt.com")

	for _, name := range codexLiveProtocolHeaders {
		if v := r.Header.Get(name); v != "" {
			hdr.Set(name, v)
		}
	}

	if accountID := executor.CodexAccountIDFromConnection(conn.ProviderSpecificData, conn.AccessToken); accountID != "" {
		hdr.Set("Chatgpt-Account-Id", accountID)
	}

	if strings.Contains(ua, "Mac OS") {
		hdr.Set("Session_id", uuid.NewString())
	}

	return hdr
}

type codexLiveSidebandStyle int

const (
	codexLiveSidebandFrameless codexLiveSidebandStyle = iota
	codexLiveSidebandRealtimeCalls
	codexLiveSidebandRealtimeQuery
)

func codexLiveSidebandTarget(c *gin.Context, base string) (string, string, bool) {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return "", "", false
	}
	if base == "" {
		base = codexLiveSidebandBase
	}
	base = strings.TrimRight(base, "/")
	if callID := strings.TrimSpace(c.Param("call_id")); callID != "" {
		style := codexLiveSidebandFrameless
		if strings.Contains(c.Request.URL.Path, "/realtime/calls/") {
			style = codexLiveSidebandRealtimeCalls
		}
		if style == codexLiveSidebandRealtimeCalls {
			base += "/realtime/calls/" + callID
		} else {
			base += "/live/" + callID
		}
		return callID, base, codexLiveCallIDPattern.MatchString(callID)
	}
	callID := strings.TrimSpace(c.Query("call_id"))
	return callID, base + "/realtime?intent=quicksilver&call_id=" + url.QueryEscape(callID), codexLiveCallIDPattern.MatchString(callID)
}

func codexLiveSidebandUpstreamURL(base, callID string) (string, error) {
	if !strings.HasPrefix(base, "ws") {
		base = codexLiveSidebandBase + base
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported websocket scheme: %s", u.Scheme)
	}
	return u.String(), nil
}

func extractCodexLiveCallID(location string) string {
	location = strings.TrimSpace(location)
	if codexLiveCallIDPattern.MatchString(location) {
		return location
	}
	u, err := url.Parse(location)
	if err != nil {
		return ""
	}
	if callID := strings.TrimSpace(u.Query().Get("call_id")); codexLiveCallIDPattern.MatchString(callID) {
		return callID
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	callID := parts[len(parts)-1]
	previous := parts[len(parts)-2]
	if !codexLiveCallIDPattern.MatchString(callID) || (previous != "live" && previous != "calls") {
		return ""
	}
	return callID
}
