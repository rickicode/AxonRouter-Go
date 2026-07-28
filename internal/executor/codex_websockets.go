package executor

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
)

// Codex upstream WebSocket constants.
// The HTTP base is https://chatgpt.com/backend-api/codex/responses; the
// WebSocket transport uses the same path with the scheme upgraded to wss.
// This matches CLIProxyAPI's buildCodexResponsesWebsocketURL.
const (
	defaultCodexBaseURL       = "https://chatgpt.com/backend-api/codex"
	defaultCodexResponsesPath = "/responses"

	codexResponsesWebsocketBetaHeaderValue = "responses_websockets=2026-02-06"
	codexResponsesWebsocketIdleTimeout     = 5 * time.Minute
	codexResponsesWebsocketHandshakeTO     = 30 * time.Second

	codexWebsocketReadLimit = 50 * 1024 * 1024 // 50 MB, matching Codex SSE scanner max
)

// CodexWebsocketsExecutor executes Codex Responses requests over a WebSocket
// transport. It is intended to be consumed by the auto-router as an Executor
// implementation while maintaining a connection lifecycle that supports
// incremental/multi-turn sessions.
type CodexWebsocketsExecutor struct {
	*BaseExecutor

	store *codexWebsocketSessionStore
}

// NewCodexWebsocketsExecutor creates a new Codex WebSocket executor.
func NewCodexWebsocketsExecutor(base *BaseExecutor) *CodexWebsocketsExecutor {
	return &CodexWebsocketsExecutor{
		BaseExecutor: base,
		store:        globalCodexWebsocketSessionStore,
	}
}

// Identifier returns the executor provider name.
func (e *CodexWebsocketsExecutor) Identifier() string { return "codex" }

type codexWebsocketSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*codexWebsocketSession
}

var globalCodexWebsocketSessionStore = &codexWebsocketSessionStore{
	sessions: make(map[string]*codexWebsocketSession),
}

type websocketConnCloser struct {
	conn *websocket.Conn
	once sync.Once
	err  error
}

func newWebsocketConnCloser(conn *websocket.Conn) *websocketConnCloser {
	if conn == nil {
		return nil
	}
	return &websocketConnCloser{conn: conn}
}

func (c *websocketConnCloser) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	c.once.Do(func() {
		// Use CloseNow to avoid blocking during teardown/shutdown contexts.
		c.err = c.conn.CloseNow()
	})
	return c.err
}

type codexWebsocketSession struct {
	sessionID string

	mu     sync.Mutex
	conn   *websocket.Conn
	closer *websocketConnCloser
	wsURL  string
	authID string
}

// getOrCreateSession returns the websocket session for the given sessionID,
// creating it if necessary. The sessionID is used by the auto-router to reuse a
// single upstream websocket across incremental turns.
func (e *CodexWebsocketsExecutor) getOrCreateSession(sessionID string) *codexWebsocketSession {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	if e == nil {
		return nil
	}

	store := e.store
	if store == nil {
		store = globalCodexWebsocketSessionStore
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.sessions == nil {
		store.sessions = make(map[string]*codexWebsocketSession)
	}
	if sess, ok := store.sessions[sessionID]; ok && sess != nil {
		return sess
	}
	sess := &codexWebsocketSession{sessionID: sessionID}
	store.sessions[sessionID] = sess
	return sess
}

// codexWebsocketHTTPURL returns the HTTP URL for Codex's /responses endpoint.
func codexWebsocketHTTPURL(req *Request) string {
	if req != nil && req.BaseURL != "" {
		base := strings.TrimSuffix(req.BaseURL, "/")
		base = strings.TrimSuffix(base, "/responses")
		if base == "" {
			base = defaultCodexBaseURL
		}
		return base + "/responses"
	}
	return defaultCodexBaseURL + "/responses"
}

// buildCodexResponsesWebsocketURL mirrors CLIProxyAPI: it converts an http/https
// Codex /responses URL into ws/wss. The canonical upstream is
// wss://chatgpt.com/backend-api/codex/responses.
func buildCodexResponsesWebsocketURL(httpURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(httpURL))
	if err != nil {
		return "", err
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	default:
		return "", fmt.Errorf("codex websockets executor: unsupported responses websocket URL scheme %q", parsed.Scheme)
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return "", fmt.Errorf("codex websockets executor: responses websocket URL host is empty")
	}
	return parsed.String(), nil
}

// codexWebsocketHeaders builds the handshake headers for a Codex upstream
// websocket. It mirrors the HTTP executor's Codex header logic and adds the
// OpenAI-Beta responses_websockets flag required for the upstream protocol.
func codexWebsocketHeaders(req *Request) http.Header {
	ua := DefaultCodexUserAgent
	if req != nil {
		if req.Headers != nil && req.Headers["User-Agent"] != "" {
			ua = req.Headers["User-Agent"]
		} else if req.ProviderSpecificData != nil && req.ProviderSpecificData["userAgent"] != "" {
			ua = req.ProviderSpecificData["userAgent"]
		}
	}

	headers := http.Header{}
	headers.Set("User-Agent", ua)
	headers.Set("Origin", "https://chatgpt.com")

	if req != nil {
		if req.AccessToken != "" {
			headers.Set("Authorization", "Bearer "+req.AccessToken)
		} else if req.APIKey != "" {
			headers.Set("Authorization", "Bearer "+req.APIKey)
		}

		accountID := ""
		if req.ProviderSpecificData != nil {
			accountID = req.ProviderSpecificData["accountId"]
			if accountID == "" {
				accountID = req.ProviderSpecificData["workspaceId"]
			}
		}
		if accountID == "" {
			accountID = CodexAccountIDFromToken(req.AccessToken)
		}
		if accountID != "" {
			headers.Set("Chatgpt-Account-Id", accountID)
		}

		// Forward remaining client headers that are not already set and are not
		// on the upstream blocklist.
		for k, v := range req.Headers {
			if _, already := headers[http.CanonicalHeaderKey(k)]; already || v == "" {
				continue
			}
			if isCodexUpstreamHeaderBlocked(k) {
				continue
			}
			headers.Set(k, v)
		}
	}

	// The upstream protocol requires this OpenAI-Beta flag to enable the
	// responses websocket transport.
	headers.Set("OpenAI-Beta", codexResponsesWebsocketBetaHeaderValue)
	return headers
}

// DialResult is the result of a successful websocket dial.
type DialResult struct {
	Conn     *websocket.Conn
	Response *http.Response
}

// Dial opens a Codex upstream websocket connection. If sessionID is non-empty,
// the connection is stored on the session for reuse by ensureSessionConn.
func (e *CodexWebsocketsExecutor) Dial(ctx context.Context, sessionID, authID string, req *Request) (*DialResult, error) {
	httpURL := codexWebsocketHTTPURL(req)
	wsURL, err := buildCodexResponsesWebsocketURL(httpURL)
	if err != nil {
		return nil, err
	}
	return e.dialWebsocket(ctx, sessionID, authID, wsURL, codexWebsocketHeaders(req))
}

func (e *CodexWebsocketsExecutor) dialWebsocket(ctx context.Context, sessionID, authID, wsURL string, headers http.Header) (*DialResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	dialCtx, cancel := context.WithTimeout(ctx, codexResponsesWebsocketHandshakeTO)
	defer cancel()

	opts := &websocket.DialOptions{
		HTTPHeader: headers,
	}
	conn, resp, err := websocket.Dial(dialCtx, wsURL, opts)
	if err != nil {
		return nil, fmt.Errorf("codex websockets dial %s: %w", wsURL, err)
	}
	conn.SetReadLimit(codexWebsocketReadLimit)

	if sessionID != "" {
		sess := e.getOrCreateSession(sessionID)
		if sess != nil {
			sess.mu.Lock()
			oldConn := sess.conn
			sess.conn = conn
			sess.wsURL = wsURL
			sess.authID = authID
			sess.closer = newWebsocketConnCloser(conn)
			sess.mu.Unlock()
			if oldConn != nil {
				_ = oldConn.Close(websocket.StatusNormalClosure, "replaced")
			}
		}
	}

	logging.Logger.Info("codex websockets: upstream connected",
		"session", strings.TrimSpace(sessionID),
		"auth", strings.TrimSpace(authID),
		"url", strings.TrimSpace(wsURL))

	return &DialResult{Conn: conn, Response: resp}, nil
}

// ensureSessionConn returns an existing websocket connection for the session if
// it matches the target authID and wsURL, otherwise dials a new one. This is the
// foundation the auto-router will use to keep a single upstream socket open
// across incremental turns.
func (e *CodexWebsocketsExecutor) ensureSessionConn(ctx context.Context, sessionID, authID, wsURL string, headers http.Header) (*websocket.Conn, *websocketConnCloser, error) {
	sess := e.getOrCreateSession(sessionID)
	if sess == nil {
		res, err := e.dialWebsocket(ctx, "", authID, wsURL, headers)
		if err != nil {
			return nil, nil, err
		}
		return res.Conn, newWebsocketConnCloser(res.Conn), nil
	}

	sess.mu.Lock()
	conn := sess.conn
	closer := sess.closer
	matches := conn != nil && closer != nil &&
		strings.TrimSpace(sess.authID) == strings.TrimSpace(authID) &&
		strings.TrimSpace(sess.wsURL) == strings.TrimSpace(wsURL)
	sess.mu.Unlock()

	if matches {
		return conn, closer, nil
	}

	// Target mismatch or missing: replace the session connection.
	res, err := e.dialWebsocket(ctx, sessionID, authID, wsURL, headers)
	if err != nil {
		return nil, nil, err
	}
	sess.mu.Lock()
	closer = sess.closer
	sess.mu.Unlock()
	return res.Conn, closer, nil
}

// CloseConnection closes the websocket connection associated with sessionID.
// It does not remove the session from the store; use CloseExecutionSession for
// full lifecycle cleanup.
func (e *CodexWebsocketsExecutor) CloseConnection(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if e == nil || sessionID == "" {
		return nil
	}

	store := e.store
	if store == nil {
		store = globalCodexWebsocketSessionStore
	}
	store.mu.Lock()
	sess := store.sessions[sessionID]
	store.mu.Unlock()
	if sess == nil {
		return nil
	}

	sess.mu.Lock()
	closer := sess.closer
	sess.mu.Unlock()
	if closer != nil {
		return closer.Close()
	}
	return nil
}

// CloseExecutionSession closes the connection and removes the session from the
// store. Passing "" is a no-op; passing a special close-all constant (if used
// by callers) will close every session.
func (e *CodexWebsocketsExecutor) CloseExecutionSession(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if e == nil || sessionID == "" {
		return
	}

	store := e.store
	if store == nil {
		store = globalCodexWebsocketSessionStore
	}
	store.mu.Lock()
	sess := store.sessions[sessionID]
	delete(store.sessions, sessionID)
	store.mu.Unlock()

	if sess == nil {
		return
	}
	sess.mu.Lock()
	closer := sess.closer
	wsURL := sess.wsURL
	authID := sess.authID
	sess.conn = nil
	sess.closer = nil
	sess.mu.Unlock()

	logging.Logger.Info("codex websockets: upstream disconnected",
		"session", strings.TrimSpace(sessionID),
		"auth", strings.TrimSpace(authID),
		"url", strings.TrimSpace(wsURL),
		"reason", "session_closed")
	if closer != nil {
		if err := closer.Close(); err != nil {
			logging.Logger.Error("codex websockets executor: close websocket error", "error", err)
		}
	}
}

// Close closes every tracked websocket session. It is safe to call during
// process shutdown.
func (e *CodexWebsocketsExecutor) Close() error {
	if e == nil {
		return nil
	}
	store := e.store
	if store == nil {
		store = globalCodexWebsocketSessionStore
	}

	store.mu.Lock()
	sessions := make([]*codexWebsocketSession, 0, len(store.sessions))
	for _, sess := range store.sessions {
		if sess != nil {
			sessions = append(sessions, sess)
		}
	}
	store.sessions = make(map[string]*codexWebsocketSession)
	store.mu.Unlock()

	var firstErr error
	for _, sess := range sessions {
		sess.mu.Lock()
		closer := sess.closer
		sess.conn = nil
		sess.closer = nil
		sess.mu.Unlock()
		if closer != nil {
			if err := closer.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// Execute performs a single-shot Codex Responses request over websocket. It is a
// best-effort implementation of the Executor interface: it dials, sends the
// request body as a text message, and reads until response.completed. Full
// incremental-turn state machine normalisation is out of scope for this issue.
func (e *CodexWebsocketsExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if req == nil {
		return nil, fmt.Errorf("codex websockets executor: request is nil")
	}

	httpURL := codexWebsocketHTTPURL(req)
	wsURL, err := buildCodexResponsesWebsocketURL(httpURL)
	if err != nil {
		return nil, err
	}
	headers := codexWebsocketHeaders(req)

	res, err := e.dialWebsocket(ctx, "", "", wsURL, headers)
	if err != nil {
		return nil, err
	}
	conn := res.Conn

	body := req.Body
	if len(body) == 0 {
		body = []byte(`{}`)
	}

	if err := conn.Write(ctx, websocket.MessageText, body); err != nil {
		_ = conn.Close(websocket.StatusInternalError, "write_error")
		return nil, fmt.Errorf("codex websockets write: %w", err)
	}

	var completed []byte
	for {
		if err := ctx.Err(); err != nil {
			_ = conn.Close(websocket.StatusGoingAway, "context_done")
			return nil, err
		}

		conn.SetReadLimit(codexWebsocketReadLimit)
		msgType, payload, err := conn.Read(ctx)
		if err != nil {
			_ = conn.Close(websocket.StatusInternalError, "read_error")
			return nil, fmt.Errorf("codex websockets read: %w", err)
		}
		if msgType != websocket.MessageText {
			continue
		}
		payload = []byte(strings.TrimSpace(string(payload)))
		if len(payload) == 0 {
			continue
		}

		_ = payload
		if strings.Contains(string(payload), `"type":"response.completed"`) {
			completed = payload
			break
		}
	}

	_ = conn.Close(websocket.StatusNormalClosure, "completed")
	return &Response{
		StatusCode: http.StatusOK,
		Body:       completed,
		Headers:    http.Header{},
	}, nil
}

// ExecuteStream is intentionally not supported for the initial websocket executor;
// streaming wire format handling will land as the auto-router integration matures.
func (e *CodexWebsocketsExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	return nil, fmt.Errorf("codex websockets executor: ExecuteStream is not implemented")
}
