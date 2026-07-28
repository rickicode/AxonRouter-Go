package executor

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// CodexAutoExecutor routes Codex requests between the HTTP CodexExecutor and
// the WebSocket CodexWebsocketsExecutor based on request characteristics. It
// selects the WebSocket executor when the downstream transport is WebSocket
// AND the auth/connection enables websockets; otherwise it falls back to the
// HTTP executor. This mirrors CLIProxyAPI's CodexAutoExecutor semantics.
type CodexAutoExecutor struct {
	httpExec *CodexExecutor
	wsExec   *CodexWebsocketsExecutor
}

// NewCodexAutoExecutor creates a new Codex auto-router executor.
func NewCodexAutoExecutor(base *BaseExecutor) *CodexAutoExecutor {
	return &CodexAutoExecutor{
		httpExec: NewCodexExecutor(base),
		wsExec:   NewCodexWebsocketsExecutor(base),
	}
}

// Execute routes the request to the appropriate Codex executor.
func (e *CodexAutoExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	if e == nil || e.httpExec == nil || e.wsExec == nil {
		return nil, fmt.Errorf("codex auto executor: executor is nil")
	}
	use := codexUseWebsocketTransport(ctx, req)
	if use {
		return e.wsExec.Execute(ctx, req)
	}
	return e.httpExec.Execute(ctx, req)
}

// ExecuteStream routes the streaming request to the appropriate Codex executor.
func (e *CodexAutoExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	if e == nil || e.httpExec == nil || e.wsExec == nil {
		return nil, fmt.Errorf("codex auto executor: executor is nil")
	}
	use := codexUseWebsocketTransport(ctx, req)
	if use {
		return e.wsExec.ExecuteStream(ctx, req)
	}
	return e.httpExec.ExecuteStream(ctx, req)
}

// codexUseWebsocketTransport reports whether a request should be routed to the
// Codex WebSocket executor. The rules match the issue requirements:
//   - downstream request uses WebSocket (ctx carries DownstreamWebsocket), OR
//   - auth flag websockets is true (req.ProviderSpecificData["websockets"]).
func codexUseWebsocketTransport(ctx context.Context, req *Request) bool {
	if ctx != nil {
		if downstreamWebsocket(ctx) {
			return true
		}
	}
	if codexWebsocketsEnabled(req) {
		return true
	}
	return false
}

// codexWebsocketsEnabled reports whether the request's auth/connection enables
// websockets via ProviderSpecificData["websockets"].
func codexWebsocketsEnabled(req *Request) bool {
	if req == nil {
		return false
	}
	if req.ProviderSpecificData != nil {
		raw := strings.TrimSpace(req.ProviderSpecificData["websockets"])
		if raw != "" {
			parsed, err := strconv.ParseBool(raw)
			if err == nil {
				return parsed
			}
		}
	}
	return false
}

// WithDownstreamWebsocket marks ctx as originating from a downstream WebSocket
// connection. This is the AxonRouter equivalent of CLIProxyAPI's
// cliproxyexecutor.WithDownstreamWebsocket.
func WithDownstreamWebsocket(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, downstreamWebsocketContextKey{}, true)
}

// downstreamWebsocket reports whether ctx was marked as a downstream WebSocket
// request.
func downstreamWebsocket(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	raw := ctx.Value(downstreamWebsocketContextKey{})
	enabled, ok := raw.(bool)
	return ok && enabled
}

// downstreamWebsocketContextKey is a package-scoped context key.
type downstreamWebsocketContextKey struct{}
