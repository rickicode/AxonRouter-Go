package executor

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/logging"
)

func silenceLogs() {
	h := slog.New(slog.NewTextHandler(io.Discard, nil))
	logging.SetLogger(h)
	slog.SetDefault(h)
}

var benchBodyNonStream = mustJSON(map[string]any{
	"model": "cf/moonshotai/kimi-k2.6",
	"messages": []map[string]any{
		{"role": "user", "content": []map[string]any{
			{"type": "text", "text": "halo"},
		}},
	},
	"max_tokens": 10000,
	"stream": false,
})

var benchBodyStream = mustJSON(map[string]any{
	"model": "cf/moonshotai/kimi-k2.6",
	"messages": []map[string]any{
		{"role": "user", "content": []map[string]any{
			{"type": "text", "text": "halo"}},
		},
	},
	"max_tokens": 10000,
	"stream": true,
})

func BenchmarkCloudflareExecutor_Full_NonStream(b *testing.B) {
	silenceLogs()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"Hi"}}]}`))
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	cf := NewCloudflareExecutor(NewOpenAIExecutor(base))

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, err := cf.Execute(context.Background(), &Request{
			Model:                "moonshotai/kimi-k2.6",
			Body:                 benchBodyNonStream,
			Stream:               false,
			APIKey:               "test-token",
			BaseURL:              ts.URL + "/accounts/{accountId}/ai/v1/chat/completions",
			Provider:             "cf",
			ProviderSpecificData: map[string]string{"accountId": "abc123"},
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCloudflareExecutor_Full_Stream(b *testing.B) {
	silenceLogs()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		_, _ = fmt.Fprintln(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}")
		flusher.Flush()
		_, _ = fmt.Fprintln(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}")
		flusher.Flush()
		_, _ = fmt.Fprintln(w, "data: [DONE]")
		flusher.Flush()
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	cf := NewCloudflareExecutor(NewOpenAIExecutor(base))

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		res, err := cf.ExecuteStream(context.Background(), &Request{
			Model:                "moonshotai/kimi-k2.6",
			Body:                 benchBodyStream,
			Stream:               true,
			APIKey:               "test-token",
			BaseURL:              ts.URL + "/accounts/{accountId}/ai/v1/chat/completions",
			Provider:             "cf",
			ProviderSpecificData: map[string]string{"accountId": "abc123"},
		})
		if err != nil {
			b.Fatal(err)
		}
		for chunk := range res.Chunks {
			if chunk.Err != nil {
				b.Fatal(chunk.Err)
			}
		}
	}
}

func BenchmarkSanitizeCFRequest_StringContent(b *testing.B) {
	silenceLogs()
	body := mustJSON(map[string]any{
		"model": "cf/moonshotai/kimi-k2.6",
		"messages": []map[string]any{
			{"role": "user", "content": "halo"},
			{"role": "assistant", "content": "hai"},
		},
		"max_tokens": 10000,
	})
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = sanitizeCFRequest(body)
	}
}

func BenchmarkSanitizeCFRequest_ArrayContent(b *testing.B) {
	silenceLogs()
	body := mustJSON(map[string]any{
		"model": "cf/moonshotai/kimi-k2.6",
		"messages": []map[string]any{
			{"role": "user", "content": []map[string]any{
				{"type": "text", "text": "hello"},
				{"type": "thinking", "thinking": "..."},
			}},
			{"role": "assistant", "content": []map[string]any{
				{"type": "tool_result", "tool_use_id": "t1", "content": "result"},
			}},
		},
		"max_tokens": 10000,
	})
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = sanitizeCFRequest(body)
	}
}
