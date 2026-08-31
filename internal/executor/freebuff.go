package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	freebuffDefaultBaseURL = "https://www.codebuff.com/api/v1/chat/completions"
	freebuffSessionPath    = "/api/v1/freebuff/session"
	freebuffAgentRunsPath  = "/api/v1/agent-runs"
	freebuffSystemMarker   = "You are Buffy, the strategic coding assistant."
	freebuffUserAgent      = "codebuff-cli/0.0.138"
	freebuffChatUserAgent  = "ai-sdk/openai-compatible/1.0/codebuff"

	// Active sessions live ~1h; used when the server omits expiresAt.
	freebuffSessionDefaultTTL = time.Hour

	// model_locked sessions (~1h) are re-checked every 1h to match the server
	// session TTL. The lock is account-wide: when any model is locked, all
	// models for the same account are blocked for the full hour.
	freebuffModelLockCooldown = time.Hour
	// limited-tier IP refusals are re-checked after 5 min.
	freebuffPoolLimitedCooldown = 5 * time.Minute

	freebuffNetworkRetries    = 3
	freebuffNetworkRetryDelay = 750 * time.Millisecond
)

// freebuffRootSystemOpenings are the canonical openings the server gate accepts
// as a byte-exact prefix on the first system message. The marker we inject must
// be one of these verbatim.
var freebuffRootSystemOpenings = []string{
	freebuffSystemMarker,
	"You are Buffy, the Freebuff Cloud project planner.",
	"You are Buffy, a strategic assistant that orchestrates complex coding tasks through specialized sub-agents.",
}

// freebuffRootAgentByModel mirrors the CLI's FREEBUFF_ROOT_AGENT_ID_BY_MODEL.
var freebuffRootAgentByModel = map[string]string{
	"deepseek/deepseek-v4-flash": "base3-free-deepseek-flash",
	"deepseek/deepseek-v4-pro":   "base3-free-deepseek",
	"mimo/mimo-v2.5":             "base3-free-mimo",
	"minimax/minimax-m3":         "base3-free-minimax-m3",
	"openai/gpt-5.6-luna":        "base3-free-luna",
}

// freebuffSessionStaleCodes are chat statuses that mean the claimed session is
// stale and must be re-claimed before retrying (mirrors the CLI's
// FreebuffGateErrorKind statuses).
var freebuffSessionStaleCodes = map[int]bool{
	426: true,
	428: true, // waiting_room_required — no session row / instance id missing
	409: true, // session_superseded / session_model_mismatch
	410: true, // session_expired — the active session's expires_at passed
}

// freebuffGate classifies a session gate from a chat/session response body.
//   - model_locked: session is bound to another model — NOT reclaimable
//   - limited_ip:   the IP tier refuses this model — pool-scoped cooldown
//   - stale:        session expired/superseded — force re-claim then retry
type freebuffGate struct {
	kind         string
	currentModel string
}

// freebuffPoolScopedError marks a limited-tier IP refusal as pool-scoped so the
// outer proxy-candidate loop can mark the pool unfit and fail over to the next
// candidate instead of failing the whole request (mirrors the reference's
// err.poolScoped + markPoolUnfit).
type freebuffPoolScopedError struct {
	message string
}

func (e *freebuffPoolScopedError) Error() string { return e.message }

// FreebuffExecutor routes Freebuff chat requests through the Codebuff/Freebuff
// backend. It claims a per-(account,model) session row, registers an agent run
// whose id the backend resolves in codebuff_metadata.run_id, and injects the
// free-tier CLI system marker so the server gate admits the request.
type FreebuffExecutor struct {
	*BaseExecutor
	mu                 sync.Mutex
	sessions           map[string]freebuffSession // token::model -> claimed session row
	inflight           map[string]*freebuffClaim  // token::model -> in-flight claim (dedupe)
	modelLockCooldowns map[string]time.Time       // token::model -> until (legacy per-model)
	accountLockUntil   map[string]time.Time       // token -> until (account-wide lock)
	poolLimitCooldowns map[string]time.Time       // proxyKey::model -> until
}

type freebuffSession struct {
	InstanceID string
	ExpiresAt  time.Time
}

// freebuffClaim is a broadcast result for one in-flight session claim: the
// claimer stores the result and closes done, waking every concurrent waiter
// (mirrors the JS reference, where all callers await the same promise).
type freebuffClaim struct {
	done   chan struct{}
	result freebuffClaimRes
}

type freebuffClaimRes struct {
	session freebuffSession
	err     error
}

// NewFreebuffExecutor creates a new Freebuff executor.
func NewFreebuffExecutor(base *BaseExecutor) *FreebuffExecutor {
	return &FreebuffExecutor{
		BaseExecutor:       base,
		sessions:           make(map[string]freebuffSession),
		inflight:           make(map[string]*freebuffClaim),
		modelLockCooldowns: make(map[string]time.Time),
		accountLockUntil:   make(map[string]time.Time),
		poolLimitCooldowns: make(map[string]time.Time),
	}
}

// StartPruner launches a background goroutine that periodically prunes
// stale session rows and expired cooldowns. Call once at startup.
func (e *FreebuffExecutor) StartPruner(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			e.pruneSessionState()
		}
	}()
}

// Execute performs a non-streaming chat completion.
func (e *FreebuffExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	res, err := e.run(ctx, req, false)
	if err != nil {
		return nil, err
	}
	return res.resp, nil
}

// ExecuteStream performs a streaming chat completion.
func (e *FreebuffExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	res, err := e.run(ctx, req, true)
	if err != nil {
		return nil, err
	}
	return res.stream, nil
}

type chatResult struct {
	status  int
	body    []byte
	headers http.Header
	resp    *Response
	stream  *StreamResult
}

// run executes the full freebuff flow: fail-fast cooldowns, session claim,
// agent-run registration, chat POST with retry, stale-session reclaim, run
// FINISH accounting, and cooldown/session-cache maintenance.
func (e *FreebuffExecutor) run(ctx context.Context, req *Request, stream bool) (*chatResult, error) {
	token := req.AccessToken
	if token == "" {
		token = req.APIKey
	}
	if token == "" {
		return nil, fmt.Errorf("freebuff: missing OAuth access token")
	}
	model := req.Model
	if model == "" {
		model = JSONGet(req.Body, "model")
	}
	model = strings.TrimPrefix(model, "freebuff/")

	// Fail fast while the account is locked to another model — no session claim,
	// no run registration, no upstream spam. The account-wide lock covers ALL
	// models for this token, not just the one that was rejected.
	if until := e.getAccountLock(token); until != nil {
		return nil, freebuffError(http.StatusConflict, fmt.Sprintf(
			"Freebuff account locked to another model — retry after %s.", until.Format(time.Kitchen)))
	}
	// Legacy per-model cooldown (kept for backward compat).
	if until := e.getCooldown(e.modelLockCooldowns, token+"::"+model); until != nil {
		return nil, freebuffError(http.StatusConflict, fmt.Sprintf(
			"Freebuff session locked to another model — retry after %s.", until.Format(time.Kitchen)))
	}

	// Pool-scoped limited-IP failover: iterate the ordered proxy candidates and
	// skip pools that are in cooldown. A limited_ip refusal marks the current
	// pool unfit and continues with the next candidate; only when every pool is
	// blocked (or the failure repeats on the last candidate) does the request
	// fail with a 409. Single-pool / direct setups behave exactly as before.
	cands := ProxyCandidatesFromContext(ctx)
	if len(cands) == 0 {
		cands = []ProxyConfig{{}}
	}
	var firstBlocked *time.Time
	for _, cand := range cands {
		proxyKey := freebuffPoolKey(cand)
		// A strict-proxy candidate with no egress is the resolver's
		// direct-fallback marker. When all pools are dead, the resolver
		// emits ONLY this marker — attempting it would leak the session
		// claim to the gateway's real IP (freebuff tiers are per-egress-IP).
		// However, when NO pools are configured at all, the resolver returns
		// an empty list and the caller adds an empty candidate to allow
		// direct connections — so we only skip when there ARE real pool
		// candidates (len(cands) > 1).
		if cand.StrictProxy && cand.ProxyURL == "" && cand.RelayURL == "" && len(cands) > 1 {
			continue
		}
		if until := e.getCooldown(e.poolLimitCooldowns, proxyKey+"::"+model); until != nil {
			if firstBlocked == nil {
				firstBlocked = until
			}
			continue
		}
		attemptCtx := freebuffSinglePoolCtx(ctx, cand)
		result, err := e.attemptPool(attemptCtx, req, model, token, stream)
		if err == nil {
			return result, nil
		}
		var poolErr *freebuffPoolScopedError
		if errors.As(err, &poolErr) {
			// Mark this pool unfit for the model and try the next candidate.
			until := time.Now().Add(freebuffPoolLimitedCooldown)
			e.setCooldown(e.poolLimitCooldowns, proxyKey+"::"+model, until)
			if firstBlocked == nil {
				firstBlocked = &until
			}
			continue
		}
		return nil, err
	}

	if firstBlocked != nil {
		return nil, freebuffError(http.StatusConflict, fmt.Sprintf(
			"Freebuff limited-mode IP rejected %s — retry with a full-access proxy after %s.", model, firstBlocked.Format(time.Kitchen)))
	}
	return nil, freebuffError(http.StatusConflict, fmt.Sprintf(
		"Freebuff limited-mode IP rejected %s — all configured proxy pools are blocked for this model.", model))
}

// attemptPool runs the full freebuff flow (session claim, agent-run
// registration, chat with retry, stale-session reclaim, FINISH accounting) for
// one proxy candidate. A limited-tier IP refusal is returned as a
// *freebuffPoolScopedError so the caller can fail over to the next pool.
func (e *FreebuffExecutor) attemptPool(ctx context.Context, req *Request, model, token string, stream bool) (*chatResult, error) {
	proxyKey := freebuffProxyKey(ctx)

	session, err := e.ensureSession(ctx, req, model, false)
	if err != nil {
		gate := sessionGateFromError(err)
		if gate.kind == "limited_ip" {
			// Pool-scoped: the outer loop marks this pool unfit and fails over.
			return nil, &freebuffPoolScopedError{message: gateErrorMessage(gate, model)}
		}
		if gate.kind == "model_locked" {
			// Account-scoped: lock the ENTIRE account (all models) so later
			// requests for ANY model fail fast instead of hammering upstream.
			until := time.Now().Add(freebuffModelLockCooldown)
			e.setAccountLock(token, until)
			e.setCooldown(e.modelLockCooldowns, token+"::"+model, until)
			return nil, freebuffError(http.StatusConflict, gateErrorMessage(gate, model))
		}
		return nil, err
	}

	runID, err := e.startRun(ctx, req, model)
	if err != nil {
		return nil, err
	}
	traceSessionID := uuid.NewString()

	// The run currently in flight. Only this one is FINISH-able: after a stale
	// session the old run is FINISH'd "cancelled" and cleared, so a later
	// failure can never double-FINISH it (the server rejects duplicate FINISHes).
	activeRunID := runID
	markFinished := func(status string) {
		if activeRunID == "" {
			return
		}
		e.finishRun(ctx, req, activeRunID, status)
		activeRunID = ""
	}

	// Safety net mirroring the reference's try/finally: if the run is still
	// active when attemptPool exits — e.g. the client cancelled the request
	// context during a retry backoff sleep — FINISH it "failed" so the upstream
	// never keeps a dangling run (the server only sweeps stale runs lazily).
	// Streaming is exempt: the lifecycle watcher (freebuffFinalizeStreamRun)
	// owns the FINISH and runs after this function returns, so the streaming
	// success branch sets the flag before the defer observes it.
	handedOff := false
	defer func() {
		if !handedOff && activeRunID != "" {
			e.finishRun(ctx, req, activeRunID, "failed")
		}
	}()

	url := freebuffURL(req)
	headers := e.chatHeaders(req, stream)
	buildBody := func() []byte {
		return e.buildChatBody(req, model, runID, traceSessionID, session.InstanceID, stream)
	}

	for attempt := 0; ; attempt++ {
		result, err := e.sendChat(ctx, req, url, headers, buildBody(), stream)
		if err != nil {
			// Transient network error — a couple of quick retries so a blip
			// doesn't fail the request and lock the model for 10 minutes.
			if attempt < 2 {
				if !sleepCtx(ctx, freebuffNetworkRetryDelay) {
					return nil, ctx.Err()
				}
				continue
			}
			markFinished("failed")
			return nil, err
		}

		// Session gates mean our claimed session is stale/absent:
		//   428 waiting_room_required — no session row / instance id missing
		//   409 session_superseded — another instance took over the session
		//   409 session_model_mismatch — session bound to a different model
		//   410 session_expired — the active session's expires_at passed
		// model_locked / limited-tier mismatches are NOT reclaimable — the server
		// keeps refusing until the session expires or the IP tier changes, so we
		// set a cooldown and fail fast instead of force re-claiming in a loop.
		if freebuffSessionStaleCodes[result.status] {
			gate := sessionGateFromText(result.body)
			if gate.kind == "limited_ip" {
				// Pool-scoped: mark this pool unfit and let the caller fail over.
				markFinished("cancelled")
				return nil, &freebuffPoolScopedError{message: gateErrorMessage(gate, model)}
			}
			if gate.kind == "model_locked" {
				markFinished("cancelled")
				until := time.Now().Add(freebuffModelLockCooldown)
				e.setAccountLock(token, until)
				e.setCooldown(e.modelLockCooldowns, token+"::"+model, until)
				return nil, freebuffError(http.StatusConflict, gateErrorMessage(gate, model))
			}
			markFinished("cancelled")
			session, err = e.ensureSession(ctx, req, model, true)
			if err != nil {
				gate := sessionGateFromError(err)
				if gate.kind == "limited_ip" {
					return nil, &freebuffPoolScopedError{message: gateErrorMessage(gate, model)}
				}
				if gate.kind == "model_locked" {
					return nil, freebuffError(http.StatusConflict, gateErrorMessage(gate, model))
				}
				return nil, err
			}
			runID, err = e.startRun(ctx, req, model)
			if err != nil {
				return nil, err
			}
			activeRunID = runID
			result, err = e.sendChat(ctx, req, url, headers, buildBody(), stream)
			if err != nil {
				markFinished("failed")
				return nil, err
			}
			if freebuffSessionStaleCodes[result.status] {
				gate := sessionGateFromText(result.body)
				if gate.kind == "limited_ip" {
					markFinished("cancelled")
					return nil, &freebuffPoolScopedError{message: gateErrorMessage(gate, model)}
				}
				if gate.kind == "model_locked" {
					markFinished("cancelled")
					until := time.Now().Add(freebuffModelLockCooldown)
					e.setAccountLock(token, until)
					e.setCooldown(e.modelLockCooldowns, token+"::"+model, until)
					return nil, freebuffError(http.StatusConflict, gateErrorMessage(gate, model))
				}
				markFinished("failed")
				return nil, freebuffError(result.status, fmt.Sprintf(
					"Freebuff session gate refused (%d) — another freebuff instance may be holding the session. %s",
					result.status, strings.TrimSpace(string(result.body))))
			}
		}

		// Registry retry: 429 / 502 / 503 / 504 with a constant delay per attempt
		// (mirrors the reference: `attempt < entry.attempts`, fixed delayMs),
		// keeping the same run_id so the backend resolves the registered run on
		// retries.
		maxRetries, retryDelay, retryable := freebuffRetryConfig(result.status)
		if retryable && attempt < maxRetries {
			if !sleepCtx(ctx, retryDelay) {
				return nil, ctx.Err()
			}
			continue
		}

		// The authToken has no refresh path — when it dies, the user re-logs in.
		// Drop the cached session for this token so a re-login starts clean.
		if result.status == http.StatusUnauthorized {
			e.dropSession(token, model)
			markFinished("failed")
			return nil, freebuffError(http.StatusUnauthorized, fmt.Sprintf(
				"Freebuff auth failed (401) — re-login in the dashboard. %s",
				strings.TrimSpace(string(result.body))))
		}

		if result.status >= 400 {
			markFinished("failed")
			return nil, &UpstreamError{StatusCode: result.status, Body: result.body, RawBody: result.body, Headers: result.headers}
		}

		// A successful chat means the pair is healthy again — lift any cooldowns.
		e.clearCooldowns(token, model, proxyKey)
		if stream && result.stream != nil {
			// Streaming: the run is only "completed" once the stream actually
			// finishes. A lifecycle watcher observes a TEE copy of the chunk
			// channel (clean close -> completed, error chunk -> failed); the
			// caller receives the other tee output, so every payload chunk still
			// reaches the handler. Previously FINISH was sent the moment the
			// connection opened, leaving an inaccurate "completed" run on the
			// upstream for mid-stream failures or client disconnects.
			handedOff = true // the watcher owns the FINISH now — exempt the defer
			result.stream = freebuffFinalizeStreamRun(ctx, result.stream, markFinished)
			return result, nil
		}
		markFinished("completed")
		return result, nil
	}
}

// freebuffFinalizeStreamRun arms the streaming FINISH lifecycle for a live SSE
// chunk channel and returns the StreamResult the caller should hand to the
// handler. The original channel is fanned out (tee): EVERY chunk is delivered
// to BOTH the watcher (which decides the FINISH status) and the returned
// channel (which the handler drains) — the watcher never steals a chunk from
// the handler.
//
// Status decision (made purely from the chunk stream):
//
//	error chunk observed            -> "failed"  (mid-stream failure/abort)
//	clean close                     -> "completed"
//	ctx cancelled before drain ends -> "failed"  (client disconnect)
//
// The base executor guarantees the original channel is always eventually
// closed (defer close(chunks)), so the fan-out and the watcher terminate. The
// ctx guard aborts the fan-out on client disconnect so a stalled consumer can
// never deadlock the request; on abort the remainder of the source is drained
// so the base stream goroutine never blocks forever on a full buffer, and the
// watcher is signalled with an error chunk.
//
// The watcher deliberately never consults ctx.Err() AFTER the channel closes:
// a stream that closed cleanly is "completed" even if the request context is
// cancelled moments later by normal handler teardown (the v1 handler cancels
// its stream sub-context via defer on return). Flipping it to "failed" then
// would misreport successful runs to the upstream FINISH endpoint.
func freebuffFinalizeStreamRun(ctx context.Context, stream *StreamResult, markFinished func(status string)) *StreamResult {
	if stream == nil || stream.Chunks == nil {
		// No observable stream — fail the run rather than leaving it dangling.
		markFinished("failed")
		return stream
	}

	handlerCh := make(chan StreamChunk, 64)
	watchCh := make(chan StreamChunk, 64)
	filter := freebuffToolCallStreamFilter{}

	// abort signals the watcher that the stream was interrupted (client
	// disconnect / cancellation), then drains the remainder of the source so
	// the base executor's stream goroutine can exit. Called only when
	// ctx.Err() != nil, so the marker error is always meaningful.
	abort := func() {
		watchCh <- StreamChunk{Err: fmt.Errorf("freebuff stream aborted: %w", ctx.Err())}
		for range stream.Chunks {
		}
	}

	// Fan-out: read the original stream once and deliver each chunk to both
	// consumers. Sequential sends mean the watcher sees exactly what the
	// handler sees; both sides drain promptly (the handler through holdback,
	// the watcher through the loop below), so this never blocks for long.
	go func() {
		defer close(handlerCh)
		defer close(watchCh)
		forward := func(chunk StreamChunk) bool {
			select {
			case handlerCh <- chunk:
			case <-ctx.Done():
				abort()
				return false
			}
			select {
			case watchCh <- chunk:
			case <-ctx.Done():
				abort()
				return false
			}
			return true
		}
		for chunk := range stream.Chunks {
			if ctx.Err() != nil {
				abort()
				return
			}
			for _, filtered := range filter.filter(chunk) {
				if !forward(filtered) {
					return
				}
			}
		}
		for _, chunk := range filter.finish() {
			if !forward(chunk) {
				return
			}
		}
	}()

	// Watcher: observe the tee copy and finalize the run exactly once. The
	// status comes solely from the chunk stream — an error chunk (including
	// the abort marker) means the run failed; a clean close means it
	// completed.
	go func() {
		status := "completed"
		for chunk := range watchCh {
			if chunk.Err != nil {
				status = "failed"
			}
		}
		markFinished(status)
	}()

	return &StreamResult{Chunks: handlerCh, Headers: stream.Headers, StatusCode: stream.StatusCode}
}

// sendChat performs a single chat POST attempt. HTTP error responses are
// normalized into a chatResult with status/body/headers; only transport-level
// failures return an error.
func (e *FreebuffExecutor) sendChat(ctx context.Context, req *Request, url string, headers map[string]string, body []byte, stream bool) (*chatResult, error) {
	if !stream {
		resp, err := e.DoRequest(ctx, http.MethodPost, url, headers, body)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 400 {
			return &chatResult{status: resp.StatusCode, body: resp.Body, headers: resp.Headers}, nil
		}
		stripFreebuffToolCalls(resp)
		return &chatResult{status: resp.StatusCode, resp: resp}, nil
	}
	result, err := e.DoStreamRequestWithConfig(ContextWithProvider(ctx, req.Provider), http.MethodPost, url, headers, body, req.StreamConfig)
	if err != nil {
		var upErr *UpstreamError
		if errors.As(err, &upErr) {
			return &chatResult{status: upErr.StatusCode, body: upErr.Body, headers: upErr.Headers}, nil
		}
		return nil, err
	}
	return &chatResult{status: result.StatusCode, stream: result}, nil
}

// freebuffRetryConfig returns the chat retry policy for a given upstream status
// code. Mirrors the reference's merged retry config: the freebuff registry entry
// ({429: {attempts:2, delayMs:2000}, 503: {attempts:2, delayMs:1500}}) spread
// over DEFAULT_RETRY_CONFIG ({502: {attempts:3, delayMs:3000},
// 504: {attempts:2, delayMs:3000}}). `attempts` is the number of retries, and
// each retry sleeps a CONSTANT delayMs (no exponential backoff).
func freebuffRetryConfig(statusCode int) (attempts int, delay time.Duration, retryable bool) {
	switch statusCode {
	case http.StatusTooManyRequests:
		return 2, 2 * time.Second, true
	case http.StatusBadGateway:
		return 3, 3 * time.Second, true
	case http.StatusServiceUnavailable:
		return 2, 1500 * time.Millisecond, true
	case http.StatusGatewayTimeout:
		return 2, 3 * time.Second, true
	}
	return 0, 0, false
}

// buildChatBody assembles the wire body: model, canonical system marker,
// top-level codebuff_metadata (run_id/client_id/cost_mode/trace_session_id/
// freebuff_instance_id) and provider.allow_fallbacks=false.
func (e *FreebuffExecutor) buildChatBody(req *Request, model, runID, traceSessionID, instanceID string, stream bool) []byte {
	body := JSONSet(req.Body, "model", model)
	body = ensureFreebuffSystemMarker(body)
	body, _ = sjson.DeleteBytes(body, "reasoning_effort")
	body, _ = sjson.DeleteBytes(body, "reasoning")
	body, _ = sjson.DeleteBytes(body, "functions")
	body = ensureFreebuffEndTurnTool(body)

	clientID := providerData(req, "fingerprintId", "freebuffClientID")
	if clientID == "" {
		clientID = uuid.NewString()
	}
	metadata := map[string]any{
		"run_id":           runID,
		"client_id":        clientID,
		"cost_mode":        "free",
		"trace_session_id": traceSessionID,
	}
	if instanceID != "" {
		metadata["freebuff_instance_id"] = instanceID
	}
	body, _ = sjson.SetBytes(body, "codebuff_metadata", metadata)
	body, _ = sjson.SetBytes(body, "provider", map[string]any{"allow_fallbacks": false})
	if stream {
		body = JSONSet(body, "stream", true)
	} else {
		body = JSONSet(body, "stream", false)
	}
	return body
}

func ensureFreebuffEndTurnTool(body []byte) []byte {
	result := gjson.GetBytes(body, "tools")
	if !result.Exists() || !result.IsArray() || len(result.Array()) == 0 {
		return body
	}
	var tools []map[string]any
	if err := json.Unmarshal([]byte(result.Raw), &tools); err != nil {
		return body
	}
	for _, tool := range tools {
		if fn, ok := tool["function"].(map[string]any); ok && fn["name"] == "end_turn" {
			return body
		}
	}
	tools = append(tools, map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        "end_turn",
			"description": "Signal the end of the current task.",
			"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
		},
	})
	updated, err := sjson.SetBytes(body, "tools", tools)
	if err != nil {
		return body
	}
	return updated
}

// ensureFreebuffSystemMarker ensures messages[0] opens with a canonical Freebuff
// root prompt (idempotent). The server gate runs a byte-exact prefix test on
// position 0 and rejects requests without it (403 free_mode_cli_required).
// A leading system message with non-string content (multimodal array parts) is
// left untouched and a NEW marker message is inserted instead, so the original
// content is never destroyed.
func ensureFreebuffSystemMarker(body []byte) []byte {
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return body
	}
	messages, ok := payload["messages"].([]any)
	if !ok {
		return body
	}
	if len(messages) > 0 {
		msg, ok := messages[0].(map[string]any)
		if ok && msg["role"] == "system" {
			if content, isStr := msg["content"].(string); isStr {
				trimmed := strings.TrimLeft(content, " \t\r\n")
				for _, opening := range freebuffRootSystemOpenings {
					if strings.HasPrefix(trimmed, opening) {
						return body // already marked
					}
				}
				msg["content"] = freebuffSystemMarker + "\n\n" + content
				out, err := json.Marshal(payload)
				if err != nil {
					return body
				}
				return out
			}
			// Non-string content — insert a new marker message instead of
			// clobbering the multimodal parts.
		}
	}
	messages = append([]any{map[string]any{"role": "system", "content": freebuffSystemMarker}}, messages...)
	payload["messages"] = messages
	out, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return out
}

// freebuffSinglePoolCtx attaches the chosen pool to ctx and REMOVES the
// candidate list so the base executor performs a single attempt with exactly
// this pool. Pool failover is driven by the outer candidate loop in run(); if
// the candidate list leaked through, DoRequest would re-iterate every pool and
// the failover bookkeeping (which pool refused) would be wrong.
func freebuffSinglePoolCtx(ctx context.Context, cand ProxyConfig) context.Context {
	ctx = ContextWithProxy(ctx, cand)
	return context.WithValue(ctx, proxyCandidatesKey{}, []ProxyConfig(nil))
}

// freebuffProxyKey derives a stable identity for the proxy in use so pool-scoped
// limited-IP cooldowns are scoped per egress path, not per account.
func freebuffProxyKey(ctx context.Context) string {
	cfg, ok := ProxyConfigFromContext(ctx)
	if !ok {
		return "direct"
	}
	return freebuffPoolKey(cfg)
}

// freebuffPoolKey derives the stable cooldown key for one proxy candidate.
func freebuffPoolKey(cfg ProxyConfig) string {
	if cfg.RelayURL != "" {
		return cfg.RelayURL
	}
	if cfg.ProxyURL != "" {
		return cfg.ProxyURL
	}
	return "direct"
}

// ensureSession returns a claimed session instance id for (token, model),
// re-claiming when the cached row is expired. Concurrent claims for the same
// key are deduplicated through the inflight map so we never POST /session
// twice for the same row: waiters subscribe to the same claim and are all
// woken when the claimer finishes.
func (e *FreebuffExecutor) ensureSession(ctx context.Context, req *Request, model string, force bool) (freebuffSession, error) {
	token := req.AccessToken
	if token == "" {
		token = req.APIKey
	}
	key := token + "::" + model

	e.mu.Lock()
	// Lazy prune: drop stale rows so the cache never accumulates expired entries.
	if s, ok := e.sessions[key]; ok && !s.ExpiresAt.IsZero() && time.Now().After(s.ExpiresAt) {
		delete(e.sessions, key)
	}
	if !force {
		if s, ok := e.sessions[key]; ok {
			e.mu.Unlock()
			return s, nil
		}
		if claim, ok := e.inflight[key]; ok {
			e.mu.Unlock()
			<-claim.done
			return claim.result.session, claim.result.err
		}
	}
	// force (or miss): drop both the cached row and any in-flight claim so a
	// fresh POST can't race a stale one back into the cache.
	delete(e.sessions, key)
	delete(e.inflight, key)
	claim := &freebuffClaim{done: make(chan struct{})}
	e.inflight[key] = claim
	e.mu.Unlock()

	session, cacheable, err := e.requestSession(ctx, req, model)
	if err == nil && cacheable {
		e.mu.Lock()
		e.sessions[key] = session
		e.mu.Unlock()
	}
	res := freebuffClaimRes{session: session, err: err}
	e.mu.Lock()
	// Only remove our own entry — a force re-claim may have replaced it, and
	// deleting the new claim would break its dedupe guarantee.
	if cur, ok := e.inflight[key]; ok && cur == claim {
		delete(e.inflight, key)
	}
	claim.result = res
	e.mu.Unlock()
	// Broadcast to every concurrent waiter. The result write happens before
	// close, so waiters reading claim.result after <-claim.done are safe.
	close(claim.done)
	return session, err
}

// requestSession POSTs /api/v1/freebuff/session with the model header to claim
// a session row (retrying transient network errors). "active" rows are cached;
// "none" means not gated (no instance id needed); gate statuses become errors.
func (e *FreebuffExecutor) requestSession(ctx context.Context, req *Request, model string) (freebuffSession, bool, error) {
	token := req.AccessToken
	if token == "" {
		token = req.APIKey
	}
	payload, _ := json.Marshal(map[string]any{})
	headers := e.sessionHeaders(req)
	headers["x-freebuff-model"] = model

	var resp *Response
	var err error
	for attempt := 0; attempt < freebuffNetworkRetries; attempt++ {
		resp, err = e.DoRequest(ctx, http.MethodPost, freebuffSessionURL(req), headers, payload)
		if err == nil {
			break
		}
		if attempt+1 < freebuffNetworkRetries {
			if !sleepCtx(ctx, freebuffNetworkRetryDelay) {
				return freebuffSession{}, false, ctx.Err()
			}
		}
	}
	if err != nil {
		return freebuffSession{}, false, fmt.Errorf("freebuff session claim: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return freebuffSession{}, false, freebuffError(http.StatusUnauthorized,
			"Freebuff session auth failed (401) — re-login in the dashboard.")
	}
	if resp.StatusCode >= 400 {
		// Non-gate HTTP failure: preserve the real status code and a friendly
		// message, while keeping the raw body for gate classification.
		return freebuffSession{}, false, &UpstreamError{
			StatusCode: resp.StatusCode,
			Body:       freebuffErrorBody(fmt.Sprintf("Freebuff session request failed: %d %s", resp.StatusCode, strings.TrimSpace(string(resp.Body)))),
			RawBody:    resp.Body,
			Headers:    resp.Headers,
		}
	}

	status := gjson.GetBytes(resp.Body, "status").String()
	instanceID := gjson.GetBytes(resp.Body, "instanceId").String()
	switch status {
	case "active":
		expiresAt := time.Now().Add(freebuffSessionDefaultTTL)
		if exp := gjson.GetBytes(resp.Body, "expiresAt").String(); exp != "" {
			if parsed, perr := time.Parse(time.RFC3339, exp); perr == nil {
				expiresAt = parsed
			}
		}
		return freebuffSession{InstanceID: instanceID, ExpiresAt: expiresAt}, true, nil
	case "none":
		// Not session-gated right now — proceed without an instance id; a 428
		// on chat tells us the admission gate actually requires a session.
		return freebuffSession{}, false, nil
	}

	// Gate statuses reported by the session endpoint. The error keeps the raw
	// upstream body in RawBody so sessionGateFromError can classify
	// model_locked/limited_ip, while Body carries the friendly client-facing
	// message (same shape as every other Freebuff error path).
	msg := freebuffGateMessage(status)
	if msg == "" {
		msg = fmt.Sprintf("Freebuff session rejected (%s): %s", status, strings.TrimSpace(string(resp.Body)))
	}
	return freebuffSession{}, false, &UpstreamError{
		StatusCode: freebuffGateStatus(status),
		Body:       freebuffErrorBody(msg),
		RawBody:    resp.Body,
		Headers:    resp.Headers,
	}
}

// startRun registers an agent run so the chat backend can resolve the run_id we
// send (the backend rejects unknown ids with 400 "runId Not Found").
func (e *FreebuffExecutor) startRun(ctx context.Context, req *Request, model string) (string, error) {
	agentID := freebuffRootAgentByModel[model]
	if agentID == "" {
		agentID = "base2-free"
	}
	payload, _ := json.Marshal(map[string]any{
		"action":         "START",
		"agentId":        agentID,
		"ancestorRunIds": []string{},
	})

	var resp *Response
	var err error
	for attempt := 0; attempt < freebuffNetworkRetries; attempt++ {
		resp, err = e.DoRequest(ctx, http.MethodPost, freebuffRunsURL(req), e.sessionHeaders(req), payload)
		if err == nil {
			break
		}
		if attempt+1 < freebuffNetworkRetries {
			if !sleepCtx(ctx, freebuffNetworkRetryDelay) {
				return "", ctx.Err()
			}
		}
	}
	if err != nil {
		return "", fmt.Errorf("freebuff agent run: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return "", freebuffError(http.StatusUnauthorized,
			"Freebuff run auth failed (401) — re-login in the dashboard.")
	}
	if resp.StatusCode >= 400 {
		return "", freebuffError(resp.StatusCode,
			fmt.Sprintf("Freebuff run start failed: %d %s", resp.StatusCode, strings.TrimSpace(string(resp.Body))))
	}
	runID := gjson.GetBytes(resp.Body, "runId").String()
	if runID == "" {
		return "", fmt.Errorf("freebuff run start returned no runId: %s", strings.TrimSpace(string(resp.Body)))
	}
	return runID, nil
}

// finishRun is best-effort run accounting mirroring the CLI's finishAgentRun.
// It never throws and is fire-and-forget (the server sweeps stale runs), so a
// hanging FINISH POST can never add latency to the request hot path.
func (e *FreebuffExecutor) finishRun(ctx context.Context, req *Request, runID, status string) {
	if runID == "" {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"action": "FINISH",
		"runId":  runID,
		"status": status,
	})
	// Resolve the URL and headers synchronously so the async goroutine never
	// touches mutable executor state (or the test hook) after this call returns.
	url := freebuffRunsURL(req)
	headers := e.sessionHeaders(req)
	go func() {
		finishCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = e.DoRequest(finishCtx, http.MethodPost, url, headers, payload)
	}()
}

// dropSession removes a cached session row and account lock for a dead token
// so a re-login starts clean.
func (e *FreebuffExecutor) dropSession(token, model string) {
	e.mu.Lock()
	delete(e.sessions, token+"::"+model)
	delete(e.accountLockUntil, token)
	e.mu.Unlock()
}

// getCooldown returns the cooldown expiry for a key, pruning expired entries on
// read so long-running servers never accumulate state for pairs no longer in use.
func (e *FreebuffExecutor) getCooldown(m map[string]time.Time, key string) *time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	until, ok := m[key]
	if !ok {
		return nil
	}
	if time.Now().After(until) {
		delete(m, key)
		return nil
	}
	return &until
}

// setCooldown stores a cooldown expiry, sweeping expired entries on write.
func (e *FreebuffExecutor) setCooldown(m map[string]time.Time, key string, until time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	for k, v := range m {
		if now.After(v) {
			delete(m, k)
		}
	}
	m[key] = until
}

// getAccountLock returns the account-wide lock expiry for a token.
func (e *FreebuffExecutor) getAccountLock(token string) *time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	until, ok := e.accountLockUntil[token]
	if !ok {
		return nil
	}
	if time.Now().After(until) {
		delete(e.accountLockUntil, token)
		return nil
	}
	return &until
}

// setAccountLock sets an account-wide lock for a token (all models blocked).
func (e *FreebuffExecutor) setAccountLock(token string, until time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.accountLockUntil[token] = until
}

// clearCooldowns lifts any model-lock / account-lock / pool-limit cooldowns
// after a successful chat response.
func (e *FreebuffExecutor) clearCooldowns(token, model, proxyKey string) {
	e.mu.Lock()
	delete(e.modelLockCooldowns, token+"::"+model)
	delete(e.accountLockUntil, token)
	delete(e.poolLimitCooldowns, proxyKey+"::"+model)
	e.mu.Unlock()
}

// pruneSessionState drops stale session rows + expired cooldowns so
// long-running servers never accumulate state for accounts/models no
// longer in use. Returns how many entries were removed.
func (e *FreebuffExecutor) pruneSessionState() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	removed := 0
	for k, s := range e.sessions {
		if !s.ExpiresAt.IsZero() && now.After(s.ExpiresAt) {
			delete(e.sessions, k)
			removed++
		}
	}
	for k, until := range e.modelLockCooldowns {
		if now.After(until) {
			delete(e.modelLockCooldowns, k)
			removed++
		}
	}
	for k, until := range e.accountLockUntil {
		if now.After(until) {
			delete(e.accountLockUntil, k)
			removed++
		}
	}
	for k, until := range e.poolLimitCooldowns {
		if now.After(until) {
			delete(e.poolLimitCooldowns, k)
			removed++
		}
	}
	return removed
}

// gateErrorMessage returns the client-facing message for a session gate.
// model_locked is account-scoped; limited_ip is pool-scoped (the pool-level
// cooldown is applied by the outer candidate loop, not here).
func gateErrorMessage(gate freebuffGate, model string) string {
	if gate.kind == "model_locked" {
		label := gate.currentModel
		if label == "" {
			label = "another model"
		}
		return fmt.Sprintf(
			"Freebuff account is locked to %s — it cannot serve %s. All models on this account are blocked for ~1h. End the session on freebuff.com or wait for it to expire.",
			label, model)
	}
	if gate.kind == "limited_ip" {
		return fmt.Sprintf(
			"Freebuff limited-mode IP rejected %s — this IP only allows DeepSeek V4 Flash / MiMo 2.5. Use a full-access proxy or a different model.",
			model)
	}
	return "Freebuff session gate refused."
}

// classifySessionGate maps a session status code to a gate kind. Matches the
// CLI's FreebuffGateErrorKind classification.
func classifySessionGate(code, message, currentModel string) freebuffGate {
	switch code {
	case "session_superseded":
		return freebuffGate{kind: "superseded"}
	case "model_locked":
		return freebuffGate{kind: "model_locked", currentModel: currentModel}
	case "session_model_mismatch":
		// The limited-tier refusal has a distinctive message; without it treat
		// it as a model lock so we don't reclaim in a loop.
		if strings.Contains(strings.ToLower(message), "limited") {
			return freebuffGate{kind: "limited_ip"}
		}
		return freebuffGate{kind: "model_locked", currentModel: currentModel}
	case "session_expired", "waiting_room_required", "session_is_active":
		return freebuffGate{kind: "stale"}
	}
	return freebuffGate{kind: "stale"}
}

// sessionGateFromText parses a gate from a chat/session response body. The
// session API reports gates via the `status` field, but some bodies carry
// error/message instead — try both so a status-shaped gate is never misread.
func sessionGateFromText(body []byte) freebuffGate {
	var parsed struct {
		Status        string `json:"status"`
		Error         string `json:"error"`
		ErrorType     string `json:"error_type"`
		Message       string `json:"message"`
		ErrorMessage  string `json:"error_message"`
		CurrentModel  string `json:"currentModel"`
		CurrentModel2 string `json:"current_model"`
	}
	_ = json.Unmarshal(body, &parsed)
	code := parsed.Status
	if code == "" {
		code = parsed.Error
	}
	if code == "" {
		code = parsed.ErrorType
	}
	msg := parsed.Message
	if msg == "" {
		msg = parsed.ErrorMessage
	}
	model := parsed.CurrentModel
	if model == "" {
		model = parsed.CurrentModel2
	}
	return classifySessionGate(code, msg, model)
}

// sessionGateFromError extracts a gate from an error, preferring UpstreamError
// bodies so a status-shaped gate is never misread as a generic stale session.
// RawBody is inspected first: requestSession keeps the raw upstream body there
// so gate statuses (model_locked / limited_ip) stay classifiable even though
// Body carries a friendly client-facing message.
func sessionGateFromError(err error) freebuffGate {
	if err == nil {
		return freebuffGate{kind: "stale"}
	}
	var upErr *UpstreamError
	if errors.As(err, &upErr) {
		if len(upErr.RawBody) > 0 {
			if gate := sessionGateFromText(upErr.RawBody); gate.kind != "stale" {
				return gate
			}
		}
		if gate := sessionGateFromText(upErr.Body); gate.kind != "stale" {
			return gate
		}
	}
	// Plain error message with an embedded JSON tail.
	msg := err.Error()
	if i := strings.Index(msg, "{"); i >= 0 {
		return sessionGateFromText([]byte(msg[i:]))
	}
	return freebuffGate{kind: "stale"}
}

// freebuffGateMessage returns a friendly message for a session gate status.
func freebuffGateMessage(status string) string {
	switch status {
	case "country_blocked":
		return "Freebuff is not available in your region (country blocked)."
	case "banned":
		return "Your Freebuff account has been banned."
	case "ip_capped":
		return "Freebuff IP cap reached — try again later."
	case "rate_limited":
		return "Freebuff session limit reached for this model — try again later."
	case "spend_limited":
		return "Freebuff spend limit reached — add credits or wait for the window to reset."
	case "model_locked":
		return "Freebuff account is locked to another model — all models are blocked for ~1h. End the session on freebuff.com or wait for it to expire."
	case "model_unavailable":
		return "This model is not available on Freebuff right now."
	case "premium_slot_taken":
		return "Freebuff premium slot is taken — try another model."
	}
	return ""
}

// freebuffGateStatus maps a session gate status to a suitable HTTP status.
func freebuffGateStatus(status string) int {
	switch status {
	case "country_blocked", "banned":
		return http.StatusForbidden
	case "spend_limited":
		return http.StatusPaymentRequired
	case "model_unavailable":
		return http.StatusNotFound
	case "model_locked", "premium_slot_taken":
		return http.StatusConflict
	default:
		return http.StatusTooManyRequests
	}
}

// freebuffErrorBody builds the OpenAI-style error JSON used for client-facing
// Freebuff errors.
func freebuffErrorBody(msg string) []byte {
	body, _ := json.Marshal(map[string]any{
		"error": map[string]any{"message": msg, "type": "invalid_request_error"},
	})
	return body
}

// freebuffError builds an OpenAI-style UpstreamError with a friendly message.
func freebuffError(status int, msg string) *UpstreamError {
	return &UpstreamError{StatusCode: status, Body: freebuffErrorBody(msg), RawBody: freebuffErrorBody(msg)}
}

// freebuffURL returns the chat completions endpoint for a request.
func freebuffURL(req *Request) string {
	if strings.TrimSpace(req.BaseURL) == "" {
		return freebuffDefaultBaseURL
	}
	return strings.TrimRight(req.BaseURL, "/")
}

// testFreebuffOrigin is a test-only hook that overrides the fixed session/run
// origin. It lets integration tests using httptest local servers reach mocked
// session/run endpoints without weakening production origin selection.
var testFreebuffOrigin func(*Request) string

// freebuffOrigin returns the fixed Codebuff origin for session/run registration.
// The reference's sessionOrigin() is always the registry baseUrl's origin
// (https://www.codebuff.com): proxies are selected via proxyOptions, never by
// changing the chat baseUrl, so a user-pointed relay BaseURL must not redirect
// session claims / run registration away from the real backend.
func freebuffOrigin(req *Request) string {
	if testFreebuffOrigin != nil {
		return testFreebuffOrigin(req)
	}
	u, err := http.NewRequest(http.MethodGet, freebuffDefaultBaseURL, nil)
	if err == nil && u.URL != nil {
		return u.URL.Scheme + "://" + u.URL.Host
	}
	return "https://www.codebuff.com"
}

func freebuffSessionURL(req *Request) string {
	return freebuffOrigin(req) + freebuffSessionPath
}

func freebuffRunsURL(req *Request) string {
	return freebuffOrigin(req) + freebuffAgentRunsPath
}

func (e *FreebuffExecutor) sessionHeaders(req *Request) map[string]string {
	h := map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json",
		"User-Agent":   freebuffUserAgent,
	}
	SetAuthHeader(h, req.APIKey, req.AccessToken)
	return h
}

func (e *FreebuffExecutor) chatHeaders(req *Request, stream bool) map[string]string {
	h := map[string]string{
		"Content-Type": "application/json",
		"User-Agent":   freebuffChatUserAgent,
	}
	if stream {
		h["Accept"] = "text/event-stream"
	}
	SetAuthHeader(h, req.APIKey, req.AccessToken)
	if req.Headers != nil {
		for k, v := range req.Headers {
			if v != "" {
				h[k] = v
			}
		}
	}
	return h
}

// providerData returns the first non-empty provider-specific value for the keys.
func providerData(req *Request, keys ...string) string {
	for _, key := range keys {
		if req.ProviderSpecificData != nil && req.ProviderSpecificData[key] != "" {
			return req.ProviderSpecificData[key]
		}
	}
	return ""
}

// sleepCtx sleeps for d, aborting early when ctx is done. Returns false when ctx
// was cancelled during the sleep.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// freebuffToolCallRe matches <tool_call>...</tool_call> blocks that the freebuff
// model emits when no real tools are provided. These XML-style tool calls are
// not OpenAI-compatible and break clients like the pi CLI.
var freebuffToolCallRe = regexp.MustCompile(`(?s)<tool_call>.*?</tool_call>\s*`)

const (
	freebuffToolCallOpen  = "<tool_call>"
	freebuffToolCallClose = "</tool_call>"
)

type freebuffToolCallStreamFilter struct {
	pending string
	inTool  bool
}

func (f *freebuffToolCallStreamFilter) filter(chunk StreamChunk) []StreamChunk {
	if chunk.Err != nil || len(chunk.Payload) == 0 {
		return []StreamChunk{chunk}
	}
	payload := string(chunk.Payload)
	if !strings.HasPrefix(payload, "data:") {
		return []StreamChunk{chunk}
	}
	data := strings.TrimSpace(strings.TrimPrefix(payload, "data:"))
	if data == "[DONE]" {
		if pending := f.flush(); pending != "" {
			return []StreamChunk{{Payload: freebuffStreamContent(pending)}, chunk}
		}
		return []StreamChunk{chunk}
	}
	updatedData := []byte(data)
	fieldsChanged := false
	choices := gjson.Get(data, "choices")
	if choices.IsArray() {
		for i, choice := range choices.Array() {
			toolCalls := choice.Get("delta.tool_calls")
			if toolCalls.IsArray() && len(toolCalls.Array()) == 0 {
				updatedData, _ = sjson.DeleteBytes(updatedData, fmt.Sprintf("choices.%d.delta.tool_calls", i))
				fieldsChanged = true
			}
		}
	}
	data = string(updatedData)
	content := gjson.Get(data, "choices.0.delta.content")
	if !content.Exists() || content.Type != gjson.String {
		if fieldsChanged {
			chunk.Payload = []byte("data: " + data)
		}
		return []StreamChunk{chunk}
	}
	cleaned := f.filterContent(content.String())
	updated, _ := sjson.Set(data, "choices.0.delta.content", cleaned)
	chunk.Payload = []byte("data: " + updated)
	return []StreamChunk{chunk}
}

func (f *freebuffToolCallStreamFilter) finish() []StreamChunk {
	if pending := f.flush(); pending != "" {
		return []StreamChunk{{Payload: freebuffStreamContent(pending)}}
	}
	return nil
}

func (f *freebuffToolCallStreamFilter) filterContent(content string) string {
	f.pending += content
	var visible strings.Builder
	for {
		if f.inTool {
			if end := strings.Index(f.pending, freebuffToolCallClose); end >= 0 {
				f.pending = f.pending[end+len(freebuffToolCallClose):]
				f.inTool = false
				continue
			}
			f.pending = freebuffToolCallSuffix(f.pending, freebuffToolCallClose)
			return visible.String()
		}
		if start := strings.Index(f.pending, freebuffToolCallOpen); start >= 0 {
			visible.WriteString(f.pending[:start])
			f.pending = f.pending[start+len(freebuffToolCallOpen):]
			f.inTool = true
			continue
		}
		pending := freebuffToolCallSuffix(f.pending, freebuffToolCallOpen)
		visible.WriteString(f.pending[:len(f.pending)-len(pending)])
		f.pending = pending
		return visible.String()
	}
}

func (f *freebuffToolCallStreamFilter) flush() string {
	if f.inTool {
		f.pending = ""
		f.inTool = false
		return ""
	}
	pending := f.pending
	f.pending = ""
	return pending
}

func freebuffToolCallSuffix(value, marker string) string {
	for n := min(len(value), len(marker)-1); n > 0; n-- {
		if strings.HasSuffix(value, marker[:n]) {
			return value[len(value)-n:]
		}
	}
	return ""
}

func freebuffStreamContent(content string) []byte {
	payload, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"delta": map[string]string{"content": content}}},
	})
	return append([]byte("data: "), payload...)
}

// stripFreebuffToolCalls removes <tool_call>...</tool_call> blocks from the
// response body's choices[0].message.content field. Non-JSON bodies and
// responses without the pattern are returned unchanged.
func stripFreebuffToolCalls(resp *Response) {
	if resp == nil || !gjson.ValidBytes(resp.Body) {
		return
	}
	content := gjson.GetBytes(resp.Body, "choices.0.message.content").String()
	if content == "" || !freebuffToolCallRe.MatchString(content) {
		return
	}
	cleaned := freebuffToolCallRe.ReplaceAllString(content, "")
	cleaned = strings.TrimSpace(cleaned)
	resp.Body, _ = sjson.SetBytes(resp.Body, "choices.0.message.content", cleaned)
}
