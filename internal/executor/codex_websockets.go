package executor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/telemetry"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Codex upstream WebSocket constants.
// The HTTP base is https://chatgpt.com/backend-api/codex/responses; the
// WebSocket transport uses the same path with the scheme upgraded to wss.
// This matches CLIProxyAPI's buildCodexResponsesWebsocketURL.
const (
	defaultCodexBaseURL                    = "https://chatgpt.com/backend-api/codex"
	defaultCodexResponsesPath              = "/responses"
	codexResponsesWebsocketBetaHeaderValue = "responses_websockets=2026-02-06"
	codexResponsesWebsocketIdleTimeout     = 5 * time.Minute
	codexResponsesWebsocketHandshakeTO     = 30 * time.Second
	codexWebsocketReadLimit                = 50 * 1024 * 1024 // 50 MB, matching Codex SSE scanner max

	codexResponsesLiteHeader   = "X-OpenAI-Internal-Codex-Responses-Lite"
	codexResponsesLiteMetadata = "client_metadata.ws_request_header_x_openai_internal_codex_responses_lite"
	codexInputItemIDLimit      = 64
)

// CodexWebsocketsExecutor executes Codex Responses requests over a WebSocket
// transport. It is intended to be consumed by the auto-router as an Executor
// implementation while maintaining a connection lifecycle that supports
// incremental/multi-turn sessions.
//
// It now implements CLIProxyAPI parity for responses websocket message
// normalization: response.create/response.append handling, incremental input
// state tracking, previous_response_id propagation, and
// responses_websocket_lite mode.
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

// codexWebsocketSession tracks an upstream websocket connection plus the
// incremental input state accumulated across turns on that session.
type codexWebsocketSession struct {
	sessionID string
	mu        sync.Mutex
	conn      *websocket.Conn
	closer    *websocketConnCloser
	wsURL     string
	authID    string

	// inputState captures the merged incremental input across turns. Each
	// completed response with output items is folded in so a follow-up
	// response.append can replay only the new user input while still
	// preserving the full conversation upstream.
	inputState *codexWebsocketInputState
}

// codexWebsocketInputState tracks partial incremental inputs across messages.
// The state records the input items from each request plus the emitted output
// items from completed responses, matching CLIProxyAPI transcript behavior.
type codexWebsocketInputState struct {
	mu       sync.Mutex
	items    []json.RawMessage
	previous string // upstream previous_response_id of the last completed response
}

func newCodexWebsocketInputState() *codexWebsocketInputState {
	return &codexWebsocketInputState{
		items: make([]json.RawMessage, 0),
	}
}

// recordRequest records the input items sent in a request, resetting the
// transcript when a fresh response.create with no previous_response_id starts
// a new conversation.
func (s *codexWebsocketInputState) recordRequest(payload []byte, reset bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if reset {
		s.items = s.items[:0]
	}
	for _, it := range jsonRawMessages(gjson.GetBytes(payload, "input")) {
		if len(it) > 0 {
			s.items = append(s.items, append(json.RawMessage(nil), it...))
		}
	}
}

// recordCompletedResponse appends output items from a completed response to the
// running transcript. This lets the state machine rebuild the conversation for
// future response.create requests that lack previous_response_id, matching
// CLIProxyAPI incremental websocket behavior.
func (s *codexWebsocketInputState) recordCompletedResponse(payload []byte) {
	if s == nil || len(payload) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	root := gjson.ParseBytes(payload)
	if resp := root.Get("response"); resp.Exists() {
		root = resp
	}
	for _, it := range jsonRawMessages(root.Get("output")) {
		if len(it) > 0 {
			s.items = append(s.items, append(json.RawMessage(nil), it...))
		}
	}
}

// prependTranscript returns a payload with the current transcript prepended
// to its input array. Used when a downstream message carries no
// previous_response_id but we still need the upstream to see full context.
func (s *codexWebsocketInputState) prependTranscript(payload []byte) []byte {
	if s == nil || len(payload) == 0 {
		return payload
	}
	s.mu.Lock()
	prefix := make([]json.RawMessage, 0, len(s.items))
	for _, it := range s.items {
		prefix = append(prefix, append(json.RawMessage(nil), it...))
	}
	s.mu.Unlock()
	if len(prefix) == 0 {
		return payload
	}
	current := jsonRawMessages(gjson.GetBytes(payload, "input"))
	merged := append(prefix, current...)
	out, err := sjson.SetRawBytes(payload, "input", marshalRawMessages(merged))
	if err != nil {
		return payload
	}
	return out
}

// jsonRawMessages extracts a slice of valid JSON raw messages from a gjson array result.
func jsonRawMessages(result gjson.Result) []json.RawMessage {
	if !result.Exists() || !result.IsArray() {
		return nil
	}
	items := result.Array()
	out := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		raw := bytes.TrimSpace([]byte(item.Raw))
		if len(raw) == 0 {
			continue
		}
		out = append(out, bytes.Clone(raw))
	}
	return out
}

// marshalRawMessages joins raw JSON messages into a single JSON array.
func marshalRawMessages(items []json.RawMessage) []byte {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, item := range items {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(bytes.TrimSpace(item))
	}
	buf.WriteByte(']')
	return buf.Bytes()
}

// getOrCreateSession returns the websocket session for the given sessionID,
// creating it if necessary. The sessionID is used by the auto-router to reuse
// a single upstream websocket across incremental turns.
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
	sess := &codexWebsocketSession{
		sessionID:  sessionID,
		inputState: newCodexWebsocketInputState(),
	}
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
			if sess.inputState == nil {
				sess.inputState = newCodexWebsocketInputState()
			}
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

// invalidateSessionConn closes the websocket connection for a session and
// clears it from the session so the next turn will dial a fresh upstream
// socket. The session incremental input state is preserved.
func (e *CodexWebsocketsExecutor) invalidateSessionConn(sessionID string) {
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
	store.mu.Unlock()
	if sess == nil {
		return
	}
	sess.mu.Lock()
	closer := sess.closer
	sess.conn = nil
	sess.closer = nil
	sess.mu.Unlock()
	if closer != nil {
		_ = closer.Close()
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

// executionSessionID returns a stable session identifier for incremental
// websocket turns. It prefers ConnectionID (persistent per HTTP/WS connection)
// so sequential requests on the same connection reuse the upstream socket and
// the incremental input state.
func executionSessionID(req *Request) string {
	if req == nil {
		return ""
	}
	return strings.TrimSpace(req.ConnectionID)
}

// codexAuthID returns a stable identifier for the request's credentials so that
// session reuse only happens when the same credential is used across turns.
func codexAuthID(req *Request) string {
	if req == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(req.AccessToken + "\x00" + req.APIKey))
	return hex.EncodeToString(sum[:])
}


// isCodexResponsesLiteRequest reports whether the client requested the
// responses_websocket_lite mode, either via the dedicated header or via the
// client_metadata mirror that Codex Desktop sends.
func isCodexResponsesLiteRequest(body []byte, headers map[string]string) bool {
	if headers != nil {
		if strings.EqualFold(strings.TrimSpace(headers[codexResponsesLiteHeader]), "true") {
			return true
		}
	}
	value := gjson.GetBytes(body, codexResponsesLiteMetadata)
	if !value.Exists() {
		return false
	}
	return value.Type == gjson.True || (value.Type == gjson.String && strings.EqualFold(strings.TrimSpace(value.String()), "true"))
}

// normalizeCodexWebsocketRequest performs CLIProxyAPI-style normalization:
//   - applies native Responses request normalization
//   - ensures image generation tools when appropriate
//   - forces stream/store
//   - sets response.create / response.append type based on the existing type
//   - removes/restricts fields as required by the websocket protocol
//   - applies responses_websocket_lite mode constraints
//
// The returned type indicates whether the caller should treat this as a fresh
// response.create (reset input state) or as an incremental response.append.
func normalizeCodexWebsocketRequest(req *Request, body []byte) (out []byte, reqType string) {
	if len(body) == 0 {
		body = []byte(`{}`)
	}
	preType := gjson.GetBytes(body, "type")
	prePrev := gjson.GetBytes(body, "previous_response_id")
	body = normalizeCodexResponsesRequest(body)
	if preType.Exists() {
		body, _ = sjson.SetBytes(body, "type", preType.Value())
	}
	if prePrev.Exists() {
		body, _ = sjson.SetBytes(body, "previous_response_id", prePrev.Value())
	}
	body = JSONSet(body, "stream", true)
	body = JSONSet(body, "store", false)
	body = ensureImageGenerationTool(body, req.Model, codexImageGenerationToolModel(req))
	body, _ = applyCodexIdentityConfuseBody(body, req.ConnectionID)

	// Preserve explicit type from clients so they can send response.append.
	reqType = strings.TrimSpace(gjson.GetBytes(body, "type").String())
	switch reqType {
	case "response.append":
		// Incremental append: leave type alone, avoid model drift.
	case "response.create":
		// Already explicit.
	default:
		// Default to response.create when not specified.
		reqType = "response.create"
		body = JSONSet(body, "type", "response.create")
	}

	// WebSocket protocol deletes fields that only make sense over HTTP SSE.
	body, _ = sjson.DeleteBytes(body, "stream")
	body, _ = sjson.DeleteBytes(body, "stream_options")
	body, _ = sjson.DeleteBytes(body, "background")

	if reqType == "response.create" {
		// codebuddy: every response.create is stored upstream.
		body = JSONSet(body, "store", true)
		if prev := strings.TrimSpace(gjson.GetBytes(body, "previous_response_id").String()); prev == "" {
			body, _ = sjson.DeleteBytes(body, "instructions")
		}
	}

	// If previous_response_id is set, instructions are not allowed together.
	if prev := strings.TrimSpace(gjson.GetBytes(body, "previous_response_id").String()); prev != "" {
		body, _ = sjson.DeleteBytes(body, "instructions")
	}

	// responses_websocket_lite disables parallel_tool_calls and tool injection.
	if isCodexResponsesLiteRequest(body, req.Headers) {
		body = JSONSet(body, "parallel_tool_calls", false)
		// Drop injected image generation tools for lite mode.
		if tools := gjson.GetBytes(body, "tools"); tools.Exists() && tools.IsArray() {
			kept := []string{}
			for _, t := range tools.Array() {
				if strings.ToLower(t.Get("type").String()) == "image_generation" {
					continue
				}
				if strings.ToLower(t.Get("type").String()) == "function" && strings.HasPrefix(t.Get("name").String(), "image_gen") {
					continue
				}
				kept = append(kept, t.Raw)
			}
			body, _ = sjson.SetRawBytes(body, "tools", []byte("["+strings.Join(kept, ",")+"]"))
		}
	}

	// Deterministically shorten overlong input item IDs to match CLIProxyAPI.
	body = sanitizeCodexInputItemIDs(body)

	return body, reqType
}

// normalizeCodexWebsocketResponse rewrites response.done to response.completed
// for downstream compatibility and restores identity-exposed continuity keys.
func normalizeCodexWebsocketResponse(payload []byte) []byte {
	if strings.TrimSpace(gjson.GetBytes(payload, "type").String()) == "response.done" {
		updated, err := sjson.SetBytes(payload, "type", "response.completed")
		if err == nil && len(updated) > 0 {
			payload = updated
		}
	}
	return payload
}

// prepareCodexWebsocketPayload finalizes the raw JSON into a websocket message
// payload that respects incremental input state and previous_response_id
// propagation. It returns the normalized body, the request type, and a session
// reset flag.
func (e *CodexWebsocketsExecutor) prepareCodexWebsocketPayload(req *Request, body []byte) (payload []byte, reqType string, reset bool) {
	body, reqType = normalizeCodexWebsocketRequest(req, body)

	// Treat any response.create without an explicit previous_response_id as reset,
	// but only when the state already has transcript items will we prepend them.
	reset = false

	session := e.getOrCreateSession(executionSessionID(req))
	if session != nil && session.inputState != nil {
		// propagate previous_response_id from state when missing
		if strings.TrimSpace(gjson.GetBytes(body, "previous_response_id").String()) == "" && !reset {
			prev := ""
			session.inputState.mu.Lock()
			prev = session.inputState.previous
			session.inputState.mu.Unlock()
			if prev != "" {
				body, _ = sjson.SetBytes(body, "previous_response_id", prev)
				body, _ = sjson.DeleteBytes(body, "instructions")
			}
		}
		if reset {
			// A fresh response.create with no previous_response_id implies a new
			// conversation. However, if the state already has transcript items
			// (e.g., a prior compact/conversation), mirror CLIProxyAI: when
			// previous_response_id is omitted, prepend the accumulated transcript
			// so the upstream still sees the full context.
			body = session.inputState.prependTranscript(body)
		}
	}

	// Final safety: drop instructions when previous_response_id exists.
	if strings.TrimSpace(gjson.GetBytes(body, "previous_response_id").String()) != "" {
		body, _ = sjson.DeleteBytes(body, "instructions")
	}

	return body, reqType, reset
}

// Execute performs a single-shot Codex Responses request over websocket. It
// now normalizes the request using the shared Responses request logic,
// maintains incremental input state across turns using session affinity, and
// propagates previous_response_id end-to-end.
func (e *CodexWebsocketsExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if req == nil {
		return nil, fmt.Errorf("codex websockets executor: request is nil")
	}
	telemetry.DefaultCodexCounters.RequestsTotal.Add(1)

	httpURL := codexWebsocketHTTPURL(req)
	wsURL, err := buildCodexResponsesWebsocketURL(httpURL)
	if err != nil {
		return nil, err
	}

	upstreamBody, reqType, reset := e.prepareCodexWebsocketPayload(req, req.Body)
	identityState := codexIdentityConfuseStateFromBody(upstreamBody, req.ConnectionID)
	headers := codexWebsocketHeaders(req)
	codexApplyIdentityConfuseHeaders(headers, identityState)

	sessionID := executionSessionID(req)
	conn, _, err := e.ensureSessionConn(ctx, sessionID, codexAuthID(req), wsURL, headers)
	if err != nil {
		return nil, err
	}


	if err := conn.Write(ctx, websocket.MessageText, upstreamBody); err != nil {
		e.invalidateSessionConn(sessionID)
		return nil, fmt.Errorf("codex websockets write: %w", err)
	}

	session := e.getOrCreateSession(sessionID)
	if session != nil && session.inputState != nil {
		session.inputState.recordRequest(upstreamBody, reset)
	}

	outputItemsByIndex := make(map[int64][]byte)
	var outputItemsFallback [][]byte
	var completedPayload []byte
	var statusCode int
	var usage CodexUsage

readLoop:
	for {
		if err := ctx.Err(); err != nil {
			e.invalidateSessionConn(sessionID)
			return nil, err
		}
		conn.SetReadLimit(codexWebsocketReadLimit)
		msgType, payload, err := conn.Read(ctx)
		if err != nil {
			e.invalidateSessionConn(sessionID)
			return nil, fmt.Errorf("codex websockets read: %w", err)
		}
		if msgType != websocket.MessageText {
			continue
		}
		payload = []byte(strings.TrimSpace(string(payload)))
		if len(payload) == 0 {
			continue
		}

		payload = normalizeCodexWebsocketResponse(payload)
		payload = applyCodexIdentityExposeResponsePayload(payload, identityState)

		eventData, eventType := parseCodexEvent(encodeCodexWebsocketAsSSE(payload))
		switch eventType {
		case "response.output_item.done":
			if item := gjson.GetBytes(eventData, "item"); item.Exists() && item.Type == gjson.JSON {
				idx := gjson.GetBytes(eventData, "output_index").Int()
				if gjson.GetBytes(eventData, "output_index").Exists() {
					outputItemsByIndex[idx] = []byte(item.Raw)
				} else {
					outputItemsFallback = append(outputItemsFallback, []byte(item.Raw))
				}
			}
		case "response.completed", "response.done":
			payload = patchCodexCompletedOutput(encodeCodexWebsocketAsSSE(payload), outputItemsByIndex, outputItemsFallback)
			_, eventType = parseCodexEvent(payload)
			if eventType == "response.completed" {
				completedPayload = payload
				if u := extractCodexUsage(payload); u.TotalTokens > 0 || u.InputTokens > 0 || u.OutputTokens > 0 {
					usage = u
				}
				if session != nil && session.inputState != nil {
					data, _ := parseCodexEvent(payload)
					session.inputState.recordCompletedResponse(data)
				}
				break readLoop
			}
		case "error":
			status := int(gjson.GetBytes(payload, "status").Int())
			if status <= 0 {
				status = int(gjson.GetBytes(payload, "status_code").Int())
			}
			if status <= 0 {
				status = http.StatusInternalServerError
			}
			e.invalidateSessionConn(sessionID)
			bodyErr := buildCodexWebsocketErrorPayload(payload, status)
			return nil, &UpstreamError{StatusCode: status, Body: bodyErr, RawBody: bodyErr, Headers: http.Header{}}
		}
	}

	if session != nil && reqType == "response.create" {
		// The upstream response id becomes the next turn's previous_response_id.
		if data, _ := parseCodexEvent(completedPayload); len(data) > 0 {
			if respID := strings.TrimSpace(gjson.GetBytes(data, "response.id").String()); respID != "" {
				session.inputState.mu.Lock()
				session.inputState.previous = respID
				session.inputState.mu.Unlock()
			}
		}
	}

	responseBody, _ := parseCodexEvent(completedPayload)
	responseBody = applyCodexIdentityExposeResponsePayload(responseBody, identityState)
	if statusCode <= 0 {
		statusCode = http.StatusOK
	}
	return &Response{
		StatusCode: statusCode,
		Body:       responseBody,
		Headers:    http.Header{},
		Usage:      usage.ToMap(),
	}, nil
}

// ExecuteStream performs a streaming Codex Responses API call over websocket.
// It mirrors the non-stream normalization and incremental input state,
// returning upstream websocket text messages as StreamChunk payloads.
func (e *CodexWebsocketsExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if req == nil {
		return nil, fmt.Errorf("codex websockets executor: request is nil")
	}
	telemetry.DefaultCodexCounters.RequestsTotal.Add(1)

	httpURL := codexWebsocketHTTPURL(req)
	wsURL, err := buildCodexResponsesWebsocketURL(httpURL)
	if err != nil {
		return nil, err
	}

	upstreamBody, reqType, reset := e.prepareCodexWebsocketPayload(req, req.Body)
	identityState := codexIdentityConfuseStateFromBody(upstreamBody, req.ConnectionID)
	headers := codexWebsocketHeaders(req)
	codexApplyIdentityConfuseHeaders(headers, identityState)

	sessionID := executionSessionID(req)
	session := e.getOrCreateSession(sessionID)
	if session != nil && session.inputState != nil {
		session.inputState.recordRequest(upstreamBody, reset)
	}

	conn, _, err := e.ensureSessionConn(ctx, sessionID, codexAuthID(req), wsURL, headers)
	if err != nil {
		return nil, err
	}

	if err := conn.Write(ctx, websocket.MessageText, upstreamBody); err != nil {
		e.invalidateSessionConn(sessionID)
		return nil, fmt.Errorf("codex websockets write: %w", err)
	}

	out := &StreamResult{
		StatusCode: http.StatusOK,
		Headers:    http.Header{},
		Chunks:     make(chan StreamChunk),
	}
	go func() {
		defer close(out.Chunks)

		outputItemsByIndex := make(map[int64][]byte)
		var outputItemsFallback [][]byte
		var sawCompleted bool
		for {
			if err := ctx.Err(); err != nil {
				e.invalidateSessionConn(sessionID)
				select {
				case out.Chunks <- StreamChunk{Err: ctx.Err()}:
				default:
				}
				return
			}
			conn.SetReadLimit(codexWebsocketReadLimit)
			msgType, payload, err := conn.Read(ctx)
			if err != nil {
				e.invalidateSessionConn(sessionID)
				if !sawCompleted {
					select {
					case out.Chunks <- StreamChunk{Err: newCodexIncompleteStreamError()}:
					default:
					}
				}
				return
			}
			if msgType != websocket.MessageText {
				continue
			}
			payload = []byte(strings.TrimSpace(string(payload)))
			if len(payload) == 0 {
				continue
			}

			payload = normalizeCodexWebsocketResponse(payload)
			payload = applyCodexIdentityExposeResponsePayload(payload, identityState)

			eventData, eventType := parseCodexEvent(encodeCodexWebsocketAsSSE(payload))
			switch eventType {
			case "response.output_item.done":
				if item := gjson.GetBytes(eventData, "item"); item.Exists() && item.Type == gjson.JSON {
					idx := gjson.GetBytes(eventData, "output_index").Int()
					if gjson.GetBytes(eventData, "output_index").Exists() {
						outputItemsByIndex[idx] = []byte(item.Raw)
					} else {
						outputItemsFallback = append(outputItemsFallback, []byte(item.Raw))
					}
				}
			case "response.completed", "response.done":
				sawCompleted = true
				payload = patchCodexCompletedOutput(encodeCodexWebsocketAsSSE(payload), outputItemsByIndex, outputItemsFallback)
				_, eventType = parseCodexEvent(payload)
				if eventType == "response.completed" && session != nil && session.inputState != nil {
					data, _ := parseCodexEvent(payload)
					session.inputState.recordCompletedResponse(data)
					if reqType == "response.create" {
						if respID := strings.TrimSpace(gjson.GetBytes(data, "response.id").String()); respID != "" {
							session.inputState.mu.Lock()
							session.inputState.previous = respID
							session.inputState.mu.Unlock()
						}
					}
				}
			case "error":
				status := int(gjson.GetBytes(payload, "status").Int())
				if status <= 0 {
					status = int(gjson.GetBytes(payload, "status_code").Int())
				}
				if status <= 0 {
					status = http.StatusInternalServerError
				}
				e.invalidateSessionConn(sessionID)
				bodyErr := buildCodexWebsocketErrorPayload(payload, status)
				select {
				case out.Chunks <- StreamChunk{Err: &UpstreamError{StatusCode: status, Body: bodyErr, RawBody: bodyErr, Headers: http.Header{}}}:
				default:
				}
				return
			}

			select {
			case out.Chunks <- StreamChunk{Payload: payload}:
			case <-ctx.Done():
				return
			}

			if sawCompleted {
				return
			}
		}
	}()

	return out, nil
}

// encodeCodexWebsocketAsSSE wraps a websocket JSON payload as a single SSE data
// line so existing Codex response parsing helpers (parseCodexEvent,
// patchCodexCompletedOutput) can be reused unchanged.
func encodeCodexWebsocketAsSSE(payload []byte) []byte {
	if len(payload) == 0 {
		return nil
	}
	line := make([]byte, 0, len("data: ")+len(payload))
	line = append(line, []byte("data: ")...)
	line = append(line, payload...)
	return line
}

// buildCodexWebsocketErrorPayload constructs a JSON error body from an upstream
// websocket error frame.
func buildCodexWebsocketErrorPayload(payload []byte, status int) []byte {
	out := []byte(`{}`)
	out, _ = sjson.SetBytes(out, "status", status)
	if bodyNode := gjson.GetBytes(payload, "body"); bodyNode.Exists() {
		out, _ = sjson.SetRawBytes(out, "body", []byte(bodyNode.Raw))
		if bodyErrorNode := bodyNode.Get("error"); bodyErrorNode.Exists() {
			out, _ = sjson.SetRawBytes(out, "error", []byte(bodyErrorNode.Raw))
			return out
		}
	}
	if errNode := gjson.GetBytes(payload, "error"); errNode.Exists() {
		out, _ = sjson.SetRawBytes(out, "error", []byte(errNode.Raw))
		return out
	}
	out, _ = sjson.SetBytes(out, "error.type", "server_error")
	out, _ = sjson.SetBytes(out, "error.message", http.StatusText(status))
	return out
}

// codexIdentityConfuseStateFromBody reconstructs an identity confuse state from
// a request body that has already been processed by applyCodexIdentityConfuseBody.
func codexIdentityConfuseStateFromBody(body []byte, connID string) codexIdentityConfuseState {
	connID = strings.TrimSpace(connID)
	if connID == "" || len(body) == 0 {
		return codexIdentityConfuseState{}
	}
	state := codexIdentityConfuseState{enabled: true, connID: connID}
	if key := strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String()); key != "" {
		state.originalPromptCacheKey = key
		state.promptCacheKey = key
	}
	return state
}

// reasonReplaySessionKey mirrors codexReasoningReplaySessionKey but using the
// normalized websocket body.
func reasonReplaySessionKey(body []byte, headers map[string]string) string {
	return codexReasoningReplaySessionKey(body, headers)
}

// codexReasoningReplayCache injects cached reasoning and function_call items
// into the websocket request body. It uses the shared cache and normalization.
func codexReasoningReplayCache(ctx context.Context, body []byte, sessionKey string) ([]byte, bool) {
	return codexInjectReasoningReplay(body, sessionKey)
}

// sanitizeCodexInputItemIDs removes encrypted reasoning items whose IDs exceed
// the Codex limit and deterministically shortens other overlong input item IDs.
// This matches CLIProxyAPI's helps.SanitizeCodexInputItemIDs behavior.
func sanitizeCodexInputItemIDs(body []byte) []byte {
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return body
	}
	items := input.Array()
	occupied := make(map[string]struct{}, len(items))
	for _, item := range items {
		if shouldDropEncryptedReasoningItem(item) {
			continue
		}
		itemID := item.Get("id")
		if itemID.Type != gjson.String {
			continue
		}
		id := itemID.String()
		if len([]rune(id)) <= codexInputItemIDLimit {
			occupied[id] = struct{}{}
		}
	}

	mapped := make(map[string]string, len(items))
	rebuilt := make([]string, 0, len(items))
	changed := false
	for _, item := range items {
		if shouldDropEncryptedReasoningItem(item) {
			changed = true
			continue
		}
		raw := item.Raw
		itemID := item.Get("id")
		if itemID.Type == gjson.String {
			id := itemID.String()
			if len([]rune(id)) > codexInputItemIDLimit {
				shortened, ok := mapped[id]
				if !ok {
					shortened = shortenCodexInputItemID(id)
					for attempt := 1; ; attempt++ {
						if _, exists := occupied[shortened]; !exists {
							break
						}
						shortened = shortenCodexInputItemIDWithAttempt(id, attempt)
					}
					mapped[id] = shortened
					occupied[shortened] = struct{}{}
				}
				next, errSet := sjson.SetBytes([]byte(raw), "id", shortened)
				if errSet == nil {
					raw = string(next)
					changed = true
				}
			}
		}
		rebuilt = append(rebuilt, raw)
	}
	if !changed {
		return body
	}
	updated, errSet := sjson.SetRawBytes(body, "input", []byte("["+strings.Join(rebuilt, ",")+"]"))
	if errSet != nil {
		return body
	}
	return updated
}

func shouldDropEncryptedReasoningItem(item gjson.Result) bool {
	if item.Get("type").String() != "reasoning" {
		return false
	}
	itemID := item.Get("id")
	if itemID.Type != gjson.String || len([]rune(itemID.String())) <= codexInputItemIDLimit {
		return false
	}
	encryptedContent := item.Get("encrypted_content")
	return encryptedContent.Type == gjson.String && encryptedContent.String() != ""
}

func shortenCodexInputItemID(id string) string {
	return shortenCodexInputItemIDWithAttempt(id, 0)
}

func shortenCodexInputItemIDWithAttempt(id string, attempt int) string {
	runes := []rune(id)
	if len(runes) <= codexInputItemIDLimit {
		return id
	}
	hashInput := id
	if attempt > 0 {
		hashInput += "\x00" + fmt.Sprintf("%d", attempt)
	}
	sum := sha256.Sum256([]byte(hashInput))
	suffix := "_" + hex.EncodeToString(sum[:8])
	prefixLength := codexInputItemIDLimit - len(suffix)
	return string(runes[:prefixLength]) + suffix
}

// static helpers for deterministic UUID generation used when identity confuse
// is not applied. We keep a tiny local namespace to avoid importing extra code.
func codexWebsocketIdentityConfuseUUID(connID, kind, value string) string {
	name := "axonrouter:codex:identity-confuse:" + kind + ":" + strings.TrimSpace(connID) + ":" + strings.TrimSpace(value)
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(name)).String()
}

// codexApplyIdentityConfuseHeaders applies identity confusion to an http.Header
// by translating the canonical keys used by the HTTP executor.
func codexApplyIdentityConfuseHeaders(headers http.Header, state codexIdentityConfuseState) {
	if !state.enabled || state.promptCacheKey == "" || headers == nil {
		return
	}
	headers.Set("Session_id", state.promptCacheKey)
	headers.Set("X-Client-Request-Id", state.promptCacheKey)
	headers.Set("Thread-Id", state.promptCacheKey)
	headers.Set("X-Codex-Window-Id", state.promptCacheKey+":0")
	if turn := strings.TrimSpace(headerValueCaseInsensitiveFromHTTPHeader(headers, "X-Codex-Turn-Metadata")); turn != "" {
		headers.Set("X-Codex-Turn-Metadata", applyCodexTurnMetadataIdentityConfuse(turn, &state))
	}
}

// headerValueCaseInsensitiveFromHTTPHeader performs a case-insensitive lookup
// against an http.Header map.
func headerValueCaseInsensitiveFromHTTPHeader(headers http.Header, name string) string {
	if headers == nil {
		return ""
	}
	name = strings.ToLower(strings.TrimSpace(name))
	for k, v := range headers {
		if strings.ToLower(k) != name {
			continue
		}
		for _, val := range v {
			if trimmed := strings.TrimSpace(val); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}
