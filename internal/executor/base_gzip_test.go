package executor

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDoRequest_GzipWithContentEncoding(t *testing.T) {
	plain := `{"id":"x","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"hi"}}]}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "gzip" {
			t.Errorf("expected Accept-Encoding: gzip, got %q", r.Header.Get("Accept-Encoding"))
		}
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		gw.Write([]byte(plain))
		gw.Close()
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "application/json")
		w.Write(buf.Bytes())
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	resp, err := base.DoRequest(context.Background(), "POST", ts.URL, map[string]string{"Content-Type": "application/json"}, []byte(`{"model":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != plain {
		t.Fatalf("expected %s, got %s", plain, string(resp.Body))
	}
}

func TestDoRequest_GzipWithoutContentEncoding(t *testing.T) {
	plain := `{"ok":true}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		gw.Write([]byte(plain))
		gw.Close()
		w.Header().Set("Content-Type", "application/json")
		w.Write(buf.Bytes())
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	resp, err := base.DoRequest(context.Background(), "POST", ts.URL, map[string]string{"Content-Type": "application/json"}, []byte(`{"model":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != plain {
		t.Fatalf("expected %s, got %s", plain, string(resp.Body))
	}
}

func TestDoStreamRequest_GzipWithContentEncoding(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "gzip" {
			t.Errorf("expected Accept-Encoding: gzip, got %q", r.Header.Get("Accept-Encoding"))
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Cache-Control", "no-cache")
		gw := gzip.NewWriter(w)
		fmt.Fprintln(gw, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}")
		flusher.Flush()
		fmt.Fprintln(gw, "data: [DONE]")
		gw.Close()
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	result, err := base.DoStreamRequest(context.Background(), "POST", ts.URL, map[string]string{"Content-Type": "application/json"}, []byte(`{"model":"x","stream":true}`))
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error: %v", chunk.Err)
		}
		got = append(got, string(chunk.Payload))
	}
	if len(got) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d: %v", len(got), got)
	}
}
