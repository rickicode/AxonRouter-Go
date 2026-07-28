package headroom

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
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
	router := gin.New()
	svc := &Service{
		server: &http.Server{
			Addr:              cfg.Endpoint,
			Handler:           router,
			ReadHeaderTimeout: 5 * time.Second,
		},
		detector:   detector,
		compressor: compressor,
		metrics:    metrics,
		maxBytes:   cfg.MaxPayloadBytes,
	}
	router.POST("/compress", svc.handleCompress)
	router.GET("/health", svc.handleHealth)
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

func (s *Service) handleHealth(c *gin.Context) {
	c.Header("Content-Type", "application/json")
	_ = json.NewEncoder(c.Writer).Encode(map[string]string{"status": "ok"})
}

func (s *Service) handleCompress(c *gin.Context) {
	var in Input
	if err := c.ShouldBindJSON(&in); err != nil {
		s.metrics.RecordError()
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	out, failed, err := s.compressInput(in)
	if err != nil {
		s.metrics.RecordError()
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if failed {
		s.metrics.RecordError()
	} else {
		s.metrics.RecordSuccess(out.OriginalBytes, out.CompressedBytes)
	}
	c.Header("Content-Type", "application/json")
	_ = json.NewEncoder(c.Writer).Encode(out)
}

func (s *Service) compressInput(in Input) (*Output, bool, error) {
	if len(in.Data) == 0 {
		return nil, false, ErrEmptyPayload
	}
	if len(in.Data) > s.maxBytes {
		return nil, false, fmt.Errorf("payload exceeds %d byte limit", s.maxBytes)
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
			return nil, false, err
		}
		// Fail-open: return the original payload on compression errors.
		final = in.Data
		kind = KindUnknown
		return &Output{
			Data:             final,
			Kind:             kind,
			OriginalBytes:    len(in.Data),
			CompressedBytes:  len(final),
			OriginalTokens:   TokenEstimate(in.Data),
			CompressedTokens: TokenEstimate(final),
		}, true, nil
	}

	return &Output{
		Data:             compressed,
		Kind:             kind,
		OriginalBytes:    len(in.Data),
		CompressedBytes:  len(compressed),
		OriginalTokens:   TokenEstimate(in.Data),
		CompressedTokens: TokenEstimate(compressed),
	}, false, nil
}
