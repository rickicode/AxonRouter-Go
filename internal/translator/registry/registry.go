// Package registry provides the global translator registry.
package registry

import (
	"context"
	"sync"

	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

// Registry manages translation functions across formats.
type Registry struct {
	mu        sync.RWMutex
	requests  map[types.Format]map[types.Format]types.TranslateFunc
	responses map[types.Format]map[types.Format]types.ResponseTransform
}

// defaultRegistry is the global translator registry.
var defaultRegistry = New()

// New constructs an empty translator registry.
func New() *Registry {
	return &Registry{
		requests:  make(map[types.Format]map[types.Format]types.TranslateFunc),
		responses: make(map[types.Format]map[types.Format]types.ResponseTransform),
	}
}

// Default returns the global registry.
func Default() *Registry {
	return defaultRegistry
}

// Register stores request/response transforms between two formats.
func (r *Registry) Register(from, to types.Format, request types.TranslateFunc, response types.ResponseTransform) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.requests[from]; !ok {
		r.requests[from] = make(map[types.Format]types.TranslateFunc)
	}
	if request != nil {
		r.requests[from][to] = request
	}

	if _, ok := r.responses[from]; !ok {
		r.responses[from] = make(map[types.Format]types.ResponseTransform)
	}
	r.responses[from][to] = response
}

// TranslateRequest converts a payload between formats.
func (r *Registry) TranslateRequest(from, to types.Format, model string, rawJSON []byte, stream bool) []byte {
	r.mu.RLock()
	var fn types.TranslateFunc
	if byTarget, ok := r.requests[from]; ok {
		fn = byTarget[to]
	}
	r.mu.RUnlock()

	if fn != nil {
		return fn(model, rawJSON, stream)
	}
	return rawJSON
}

// TranslateStream translates a streaming response chunk.
func (r *Registry) TranslateStream(ctx context.Context, from, to types.Format, model string, originalReq, translatedReq, rawChunk []byte, param *any) [][]byte {
	r.mu.RLock()
	var fn types.TranslateStreamFunc
	if byTarget, ok := r.responses[from]; ok {
		if rt, ok := byTarget[to]; ok {
			fn = rt.Stream
		}
	}
	r.mu.RUnlock()

	if fn != nil {
		return fn(ctx, model, originalReq, translatedReq, rawChunk, param)
	}
	return [][]byte{append(rawChunk, "\n\n"...)}
}

// TranslateNonStream translates a non-streaming response body.
func (r *Registry) TranslateNonStream(ctx context.Context, from, to types.Format, model string, originalReq, translatedReq, rawJSON []byte, param *any) []byte {
	r.mu.RLock()
	var fn types.TranslateNonStreamFunc
	if byTarget, ok := r.responses[from]; ok {
		if rt, ok := byTarget[to]; ok {
			fn = rt.NonStream
		}
	}
	r.mu.RUnlock()

	if fn != nil {
		return fn(ctx, model, originalReq, translatedReq, rawJSON, param)
	}
	return rawJSON
}

// HasRequestTransformer indicates whether a request translator exists.
func (r *Registry) HasRequestTransformer(from, to types.Format) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if byTarget, ok := r.requests[from]; ok {
		if fn, ok := byTarget[to]; ok && fn != nil {
			return true
		}
	}
	return false
}

// HasResponseTransformer indicates whether a response translator exists.
func (r *Registry) HasResponseTransformer(from, to types.Format) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if byTarget, ok := r.responses[from]; ok {
		if rt, ok := byTarget[to]; ok && (rt.Stream != nil || rt.NonStream != nil) {
			return true
		}
	}
	return false
}

// Package-level convenience functions

// Register registers a translator pair on the default registry.
func Register(from, to types.Format, request types.TranslateFunc, response types.ResponseTransform) {
	defaultRegistry.Register(from, to, request, response)
}

// Request translates a request using the default registry.
func Request(from, to, model string, rawJSON []byte, stream bool) []byte {
	return defaultRegistry.TranslateRequest(types.Format(from), types.Format(to), model, rawJSON, stream)
}

// NeedConvert checks if response translation exists on the default registry.
func NeedConvert(from, to string) bool {
	return defaultRegistry.HasResponseTransformer(types.Format(from), types.Format(to))
}

// Response translates a streaming response using the default registry.
func Response(ctx context.Context, from, to, model string, originalReq, translatedReq, rawChunk []byte, param *any) [][]byte {
	return defaultRegistry.TranslateStream(ctx, types.Format(from), types.Format(to), model, originalReq, translatedReq, rawChunk, param)
}

// ResponseNonStream translates a non-streaming response using the default registry.
func ResponseNonStream(ctx context.Context, from, to, model string, originalReq, translatedReq, rawJSON []byte, param *any) []byte {
	return defaultRegistry.TranslateNonStream(ctx, types.Format(from), types.Format(to), model, originalReq, translatedReq, rawJSON, param)
}
