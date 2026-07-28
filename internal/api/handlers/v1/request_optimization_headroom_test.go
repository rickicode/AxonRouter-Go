package v1

import (
	"context"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/compression"
	"github.com/rickicode/AxonRouter-Go/internal/headroom"
)

func TestCompressRequestBodyHeadroomIntegration(t *testing.T) {
	h := newTestHandler(t)
	h.compressionStrategy = compression.Strategy{Mode: compression.ModeOff}

	s := headroom.NewServer()
	endpoint, err := s.Start()
	if err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer s.Stop(context.Background())

	client, err := headroom.NewClient(headroom.Config{Enabled: true, Endpoint: endpoint, TimeoutMs: 5000})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	h.headroomClient = client

	body := []byte(`{"model":"claude","messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"ok  github.com/rickicode/AxonRouter-Go/internal/headroom  0.006s\n\n\n"}]}]}`)
	out := h.compressRequestBody(body)

	if string(out) == string(body) {
		t.Errorf("expected headroom compression to change tool_result content")
	}
	if len(out) > len(body) {
		t.Errorf("compressed body larger than input: %d > %d", len(out), len(body))
	}
}

func TestCompressRequestBodyHeadroomDisabled(t *testing.T) {
	h := newTestHandler(t)
	h.compressionStrategy = compression.Strategy{Mode: compression.ModeOff}

	client, err := headroom.NewClient(headroom.Config{Enabled: false})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	h.headroomClient = client

	body := []byte(`{"model":"claude","messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"ok  test\n\n\n"}]}]}`)
	out := h.compressRequestBody(body)

	if string(out) != string(body) {
		t.Errorf("disabled headroom should return original body; got %s", out)
	}
}
