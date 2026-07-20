package executor

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestKiroStreamWrapWithHoldback(t *testing.T) {
	contentFrame := buildEventFrame(
		map[string]string{
			":event-type":   "assistantResponseEvent",
			":message-type": "event",
		},
		map[string]any{
			"assistantResponseEvent": map[string]any{"content": "hello"},
		},
	)
	metricsFrame := buildEventFrame(
		map[string]string{":event-type": "meteringEvent"},
		map[string]any{
			"meteringEvent": map[string]any{
				"metricsEvent": map[string]any{
					"inputTokens":  5,
					"outputTokens": 3,
				},
			},
		},
	)
	stopFrame := buildEventFrame(
		map[string]string{":event-type": "messageStopEvent"},
		map[string]any{"messageStopEvent": map[string]any{}},
	)

	var body []byte
	body = append(body, contentFrame...)
	body = append(body, metricsFrame...)
	body = append(body, stopFrame...)

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     http.Header{"Content-Type": []string{"application/vnd.amazon.eventstream"}},
			Request:    req,
		}, nil
	})}

	base := &BaseExecutor{
		Client:            client,
		streamBase:        client,
		StreamIdleTimeout: 5 * time.Second,
	}
	exec := NewKiroExecutor(base)

	req := &Request{
		Provider: "kiro",
		Model:    "kiro",
		Body:     []byte(`{"conversationState":{}}`),
		ProviderSpecificData: map[string]string{
			"authMethod": "builder-id",
		},
	}

	res, err := exec.ExecuteStream(context.Background(), req)
	if err != nil {
		t.Fatalf("ExecuteStream returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Mimic chat.go: the handler swaps the returned channel for a holdback
	// wrapper and then reads from the wrapper. The executor goroutine must
	// continue to use its original channel reference; otherwise it can race
	// and close the holdback channel after WrapWithHoldback already closed it.
	holdback, _ := WrapWithHoldback(ctx, res.Chunks, 50, 64*1024)
	res.Chunks = holdback

	var count int
	for range res.Chunks {
		count++
	}
	if count == 0 {
		t.Fatal("expected at least one chunk")
	}
}
