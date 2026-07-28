package headroom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// InternalService implements Service and exposes an HTTP /compress endpoint.
type InternalService struct {
	cfg        Config
	compressor Compressor
	detector   Detector
	metrics    *Metrics
	server     *http.Server
	listener   net.Listener
	started    bool
}

// NewInternalService creates an unstarted service with default config and a
// fresh compressor.
func NewInternalService(cfg Config) (*InternalService, error) {
	comp, err := NewDefaultCompressor()
	if err != nil {
		return nil, err
	}
	return &InternalService{
		cfg:        cfg,
		compressor: comp,
		detector:   &DefaultDetector{},
		metrics:    NewMetrics(),
	}, nil
}

// NewInternalServiceWithDeps creates a service with injected dependencies.
func NewInternalServiceWithDeps(cfg Config, c Compressor, d Detector, m *Metrics) *InternalService {
	if m == nil {
		m = NewMetrics()
	}
	return &InternalService{cfg: cfg, compressor: c, detector: d, metrics: m}
}

// Enabled reports whether headroom is enabled in config.
func (s *InternalService) Enabled() bool {
	return s != nil && s.cfg.Enabled
}

// Metrics returns the service metrics.
func (s *InternalService) Metrics() *Metrics {
	if s == nil || s.metrics == nil {
		return NewMetrics()
	}
	return s.metrics
}

// Compress performs in-process classification and compression.
func (s *InternalService) Compress(ctx context.Context, in Input) (Output, error) {
	if s == nil {
		return Output{}, errors.New("headroom: nil service")
	}
	s.metrics.RecordTotal()

	if len(in.Payload) == 0 {
		return Output{Kind: KindUnknown, Method: "none"}, nil
	}
	if len(in.Payload) > s.cfg.MaxPayloadBytes {
		s.metrics.RecordError()
		return Output{}, fmt.Errorf("headroom: payload %d bytes exceeds max %d", len(in.Payload), s.cfg.MaxPayloadBytes)
	}

	kind, _ := s.detector.Detect(in)
	if kind == "" || kind == KindUnknown {
		kind = KindUnknown
	}

	out, err := s.compress(ctx, kind, in.Payload)
	if err != nil {
		s.metrics.RecordError()
		return Output{}, err
	}
	return out, nil
}

func (s *InternalService) compress(ctx context.Context, kind Kind, payload []byte) (Output, error) {
	method := TransformMethod(kind)
	compressed, err := s.compressor.Compress(kind, payload)
	if err != nil {
		return Output{OriginalSize: len(payload), CompressedSize: 0, Kind: kind, Method: method, Error: err.Error()}, err
	}
	saved := len(payload) - len(compressed)
	s.metrics.RecordBytesSaved(saved)
	return Output{Payload: compressed, OriginalSize: len(payload), CompressedSize: len(compressed), Kind: kind, Method: method}, nil
}

// Start launches the internal HTTP server. If cfg.Endpoint is empty the default
// 127.0.0.1:9123 address is used.
func (s *InternalService) Start(ctx context.Context) error {
	if s == nil {
		return errors.New("headroom: nil service")
	}
	if s.started {
		return nil
	}
	addr := s.cfg.Endpoint
	if addr == "" {
		addr = "http://127.0.0.1:9123"
	}
	// Strip scheme so net.Listen can parse the host:port.
	addr = stripScheme(addr)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("headroom: listen %s: %w", addr, err)
	}
	s.listener = ln

	mux := http.NewServeMux()
	mux.HandleFunc("/compress", s.handleCompress)
	mux.HandleFunc("/health", s.handleHealth)

	s.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  s.cfg.Timeout,
		WriteTimeout: s.cfg.Timeout,
	}
	s.started = true

	go func() {
		<-ctx.Done()
		_ = s.Stop(context.Background())
	}()

	go func() {
		_ = s.server.Serve(ln)
	}()

	return nil
}

// Stop shuts down the HTTP server.
func (s *InternalService) Stop(ctx context.Context) error {
	if s == nil || !s.started || s.server == nil {
		return nil
	}
	err := s.server.Shutdown(ctx)
	s.started = false
	return err
}

// Addr returns the net address the server is listening on.
func (s *InternalService) Addr() string {
	if s == nil || s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

func (s *InternalService) handleCompress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(s.cfg.MaxPayloadBytes)+1))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(body) > s.cfg.MaxPayloadBytes {
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}

	var in Input
	if err := json.Unmarshal(body, &in); err != nil {
		// Allow raw payloads posted as plain bytes.
		in = Input{Payload: body}
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.Timeout)
	defer cancel()

	out, err := s.Compress(ctx, in)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

func (s *InternalService) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// stripScheme removes http:// or https:// from a URL.
func stripScheme(addr string) string {
	addr = stripPrefix(addr, "http://")
	addr = stripPrefix(addr, "https://")
	return addr
}

func stripPrefix(s, prefix string) string {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

// InternalHealthCheck waits up to timeout for the service to become ready.
func InternalHealthCheck(endpoint string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for time.Now().Before(deadline) {
		resp, err := client.Get(endpoint + "/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("headroom: service did not become healthy within %s", timeout)
}

// CompressViaHTTP sends an Input to a remote headroom service and returns Output.
func CompressViaHTTP(ctx context.Context, endpoint string, in Input) (Output, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return Output{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/compress", bytes.NewReader(body))
	if err != nil {
		return Output{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Output{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return Output{}, fmt.Errorf("headroom: %d %s", resp.StatusCode, string(msg))
	}
	var out Output
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Output{}, err
	}
	return out, nil
}
