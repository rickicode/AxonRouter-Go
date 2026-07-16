// Package active tracks currently in-flight proxied requests so the
// dashboard's live "ActiveOctopus" / in-flight panel can display them.
// It is a leaf package (stdlib only) so both the v1 proxy handlers and
// the admin logs API can import it without an import cycle.
package active

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// ctxKey is the context key type for the active-request ID. Using a distinct
// non-string type avoids collisions with other context values.
type ctxKey string

const idKey ctxKey = "activeReqID"

// WithID attaches the active-request ID to a context.
func WithID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, idKey, id)
}

// IDFrom retrieves the active-request ID from a context, if present.
func IDFrom(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(idKey).(string)
	return id, ok
}

// Request is a single in-flight proxied request.
type Request struct {
	ID                   string `json:"id"`
	StartedAt            int64  `json:"started_at"` // Unix milliseconds
	ProviderTypeID       string `json:"provider_type_id"`
	ConnectionID         string `json:"connection_id"`
	ConnectionName       string `json:"connection_name"`
	TargetProviderTypeID string `json:"target_provider_type_id"`
	ModelID              string `json:"model_id"`
	Modality             string `json:"modality"`
	Stream               bool   `json:"stream"`
}

type registry struct {
	mu    sync.RWMutex
	items map[string]*Request
}

var defaultRegistry = &registry{items: make(map[string]*Request)}

var idCounter uint64

// NewID returns a process-unique identifier for an in-flight request.
func NewID() string {
	n := atomic.AddUint64(&idCounter, 1)
	return time.Now().Format("20060102.150405.000000") + "-" + itoa(n)
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// Register adds a new in-flight request.
func Register(r *Request) {
	defaultRegistry.mu.Lock()
	defaultRegistry.items[r.ID] = r
	defaultRegistry.mu.Unlock()
}

// BindConn fills in the chosen account once connection selection completes.
func BindConn(id, connID, connName, providerTypeID string) {
	defaultRegistry.mu.Lock()
	if r, ok := defaultRegistry.items[id]; ok {
		r.ConnectionID = connID
		r.ConnectionName = connName
		r.TargetProviderTypeID = providerTypeID
	}
	defaultRegistry.mu.Unlock()
}

// Deregister removes a finished request.
func Deregister(id string) {
	defaultRegistry.mu.Lock()
	delete(defaultRegistry.items, id)
	defaultRegistry.mu.Unlock()
}

// List returns a snapshot of all in-flight requests.
func List() []Request {
	defaultRegistry.mu.RLock()
	defer defaultRegistry.mu.RUnlock()
	out := make([]Request, 0, len(defaultRegistry.items))
	for _, r := range defaultRegistry.items {
		out = append(out, *r)
	}
	return out
}
