package headroom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultPort is the TCP port used by the internal Headroom server.
const DefaultPort = "9123"

// DefaultAddr is the bind address used by the internal server.
const DefaultAddr = "127.0.0.1"

// Server is an in-process Headroom HTTP server.
type Server struct {
	mu       sync.Mutex
	http     *http.Server
	listener net.Listener
	endpoint string
	started  bool
	
	metrics Metrics
}

// NewServer creates a server listening on the default loopback address.
func NewServer() *Server {
	s := &Server{}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/compress", s.handleCompress)
	s.http = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

// Start binds the server to the default address. It picks an available port if
// 9123 is busy and stores the actual endpoint.
func (s *Server) Start() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return s.endpoint, nil
	}

	addr := net.JoinHostPort(DefaultAddr, DefaultPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// Fallback: let the OS choose a port.
		addr = net.JoinHostPort(DefaultAddr, "0")
		ln, err = net.Listen("tcp", addr)
		if err != nil {
			return "", fmt.Errorf("headroom listen: %w", err)
		}
	}
	s.listener = ln
	s.endpoint = fmt.Sprintf("http://%s", ln.Addr().String())
	s.started = true

	go func() {
		if err := s.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("headroom server error: %v", err)
		}
	}()
	return s.endpoint, nil
}

// Stop shuts down the server.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return nil
	}
	err := s.http.Shutdown(ctx)
	s.started = false
	s.listener = nil
	return err
}

// Endpoint returns the endpoint (empty if not started).
func (s *Server) Endpoint() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.endpoint
}

// Running reports whether the server is running.
func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleCompress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, int64(DefaultMaxPayloadBytes)))
	if err != nil {
		s.atomicIncrErrors()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req PayloadHeader
	if err := json.Unmarshal(body, &req); err != nil {
		s.atomicIncrErrors()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	kind := req.Kind
	if kind == "" {
		kind = DetectPayloadKind(req.Original)
	}
	compressed, saved, techniques := Compress(kind, req.Original)

	result := CompressedResult{
		Kind:         kind,
		Original:     req.Original,
		Compressed:   compressed,
		OriginalSize: len(req.Original),
		SavedBytes:   saved,
		Techniques:   techniques,
	}

	s.atomicAddMetrics(saved)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *Server) atomicAddMetrics(saved int) {
	atomic.AddInt64(&s.metrics.Total, 1)
	atomic.AddInt64(&s.metrics.BytesSaved, int64(saved))
}

func (s *Server) atomicIncrErrors() {
	atomic.AddInt64(&s.metrics.Errors, 1)
}

// Metrics returns a copy of current metrics.
func (s *Server) Metrics() Metrics {
	return Metrics{
		Total:      atomic.LoadInt64(&s.metrics.Total),
		BytesSaved: atomic.LoadInt64(&s.metrics.BytesSaved),
		Errors:     atomic.LoadInt64(&s.metrics.Errors),
	}
}

// WriteDefaultJSONHeaders sets the Content-Type header.
func WriteDefaultJSONHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
}

// kindFromString normalises a payload kind string.
func kindFromString(s string) PayloadKind {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "git_diff", "diff":
		return KindGitDiff
	case "git_log":
		return KindGitLog
	case "git_status", "status":
		return KindGitStatus
	case "grep":
		return KindGrep
	case "find_tree", "find", "tree":
		return KindFindTree
	case "build_log", "build":
		return KindBuildLog
	case "search":
		return KindSearch
	case "tool_result", "tool":
		return KindToolResult
	default:
		return KindGeneric
	}
}
