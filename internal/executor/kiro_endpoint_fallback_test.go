package executor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestKiroEndpointFallback_RetriesOn500(t *testing.T) {
	kiroDevHost := "runtime.us-east-1.kiro.dev"
	awsHost := "codewhisperer.us-east-1.amazonaws.com"

	var calls []string
	successFrame := buildEventFrame(
		map[string]string{":event-type": "messageStopEvent"},
		map[string]any{"messageStopEvent": map[string]any{}},
	)

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.URL.Host)
		if req.URL.Host == kiroDevHost {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader("kiro.dev is down")),
				Header:     http.Header{},
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(successFrame)),
			Header:     http.Header{"Content-Type": []string{"application/vnd.amazon.eventstream"}},
			Request:    req,
		}, nil
	})}

	base := &BaseExecutor{
		Client:            client,
		streamBase:        client,
		StreamIdleTimeout: 200 * time.Millisecond,
	}
	exec := NewKiroExecutor(base)

	req := &Request{
		Provider: "kiro",
		Model:    "kiro",
		Body:     []byte(`{"conversationState":{}}`),
		ProviderSpecificData: map[string]string{
			"authMethod": "builder-id",
			"region":     "us-east-1",
		},
	}

	res, err := exec.ExecuteStream(context.Background(), req)
	if err != nil {
		t.Fatalf("ExecuteStream returned error: %v", err)
	}
	for chunk := range res.Chunks {
		if chunk.Err != nil {
			t.Fatalf("unexpected chunk error: %v", chunk.Err)
		}
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 upstream calls, got %d: %v", len(calls), calls)
	}
	if calls[0] != kiroDevHost {
		t.Errorf("first call = %q, want %q", calls[0], kiroDevHost)
	}
	if calls[1] != awsHost {
		t.Errorf("second call = %q, want %q", calls[1], awsHost)
	}
}

func TestKiroEndpointFallback_StopsOn400(t *testing.T) {
	kiroDevHost := "runtime.us-east-1.kiro.dev"

	var calls []string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.URL.Host)
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       io.NopCloser(strings.NewReader(`{"message":"bad request"}`)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Request:    req,
		}, nil
	})}

	base := &BaseExecutor{Client: client, streamBase: client}
	exec := NewKiroExecutor(base)

	req := &Request{
		Provider: "kiro",
		Model:    "kiro",
		Body:     []byte(`{"conversationState":{}}`),
		ProviderSpecificData: map[string]string{
			"authMethod": "builder-id",
			"region":     "us-east-1",
		},
	}

	_, err := exec.ExecuteStream(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	var upErr *UpstreamError
	if !errors.As(err, &upErr) {
		t.Fatalf("expected *UpstreamError, got %T: %v", err, err)
	}
	if upErr.StatusCode != http.StatusBadRequest {
		t.Errorf("status code = %d, want %d", upErr.StatusCode, http.StatusBadRequest)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 upstream call, got %d: %v", len(calls), calls)
	}
	if calls[0] != kiroDevHost {
		t.Errorf("first call = %q, want %q", calls[0], kiroDevHost)
	}
}

func TestKiroEndpointFallback_AllFail(t *testing.T) {
	var calls []string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.URL.Host)
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Body:       io.NopCloser(strings.NewReader("unavailable")),
			Header:     http.Header{},
			Request:    req,
		}, nil
	})}

	base := &BaseExecutor{Client: client, streamBase: client}
	exec := NewKiroExecutor(base)

	req := &Request{
		Provider: "kiro",
		Model:    "kiro",
		Body:     []byte(`{"conversationState":{}}`),
		ProviderSpecificData: map[string]string{
			"authMethod": "builder-id",
			"region":     "us-east-1",
		},
	}

	_, err := exec.ExecuteStream(context.Background(), req)
	if err == nil {
		t.Fatal("expected error when all endpoints fail")
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 upstream calls, got %d: %v", len(calls), calls)
	}
	if !strings.Contains(err.Error(), "kiro request failed") {
		t.Errorf("error = %q, want it to contain 'kiro request failed'", err.Error())
	}
}
