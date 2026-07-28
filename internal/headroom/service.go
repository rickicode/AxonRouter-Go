package headroom

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Service is an HTTP server that exposes the /compress endpoint.
type Service struct {
	server     *http.Server
	detector   Detector
	compressor Compressor
	metrics    *Metrics
	maxBytes   int
}

// NewService creates a headroom HTTP service.
func NewService(cfg Config, detector Detector, compressor Compressor, metrics *Metrics) *Service {
	if detector == nil {
		detector = NewDefaultDetector()
	}
	if compressor == nil {
		compressor = NewDefaultCompressor()
	}
	if metrics == nil {
		metrics = NewMetrics()
	}
	mux := http.NewServeMux()
	svc := &Service{
		server: &http.Server{
			Addr:              cfg.Endpoint,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		},
		detector:   detector,
		compressor: compressor,
		metrics:    metrics,
		maxBytes:   cfg.MaxPayloadBytes,
	}
	mux.HandleFunc("/compress", svc.handleCompress)
	mux.HandleFunc("/health", svc.handleHealth)
	return svc
}

// ListenAndServe starts the HTTP service. Blocks until the server is closed.
func (s *Service) ListenAndServe() error {
	return s.server.ListenAndServe()
}

// Close shuts down the service.
func (s *Service) Close() error {
	return s.server.Close()
}

func (s *Service) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Service) handleCompress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		s.metrics.RecordError()
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, int64(s.maxBytes))
	var in Input
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		s.metrics.RecordError()
		return
	}

	out, err := s.compressInput(in)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		s.metrics.RecordError()
		return
	}

	s.metrics.RecordSuccess(out.OriginalBytes, out.CompressedBytes)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Service) compressInput(in Input) (*Output, error) {
	if len(in.Data) == 0 {
		return nil, ErrEmptyPayload
	}
	if len(in.Data) > s.maxBytes {
		return nil, fmt.Errorf("payload exceeds %d byte limit", s.maxBytes)
	}

	kind := in.KindHint
	if kind == "" {
		kind = s.detector.Detect(in.Data)
	}
	if kind == "" {
		kind = KindUnknown
	}

	compressed, err := s.compressor.Compress(in.Data, kind)
	var final []byte
	if err != nil {
		if errors.Is(err, ErrKindUnknown) {
			return nil, err
		}
		// Fail-open: return the original payload on compression errors.
		final = in.Data
		kind = KindUnknown
	} else {
		final = compressed
	}

	return &Output{
		Data:             final,
		Kind:             kind,
		OriginalBytes:    len(in.Data),
		CompressedBytes:  len(final),
		OriginalTokens:   TokenEstimate(in.Data),
		CompressedTokens: TokenEstimate(final),
	}, nil
}
