package executor

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudflareSTT_PayloadExtraction(t *testing.T) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("model", "cf/openai/whisper")
	_ = writer.WriteField("language", "en")
	part, _ := writer.CreateFormFile("file", "audio.mp3")
	_, _ = part.Write([]byte("fake audio"))
	writer.Close()

	body, ct, err := buildCloudflareSTTPayload(buf.Bytes(), writer.FormDataContentType())
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	if !strings.Contains(ct, "multipart/") {
		t.Fatalf("content type = %q, want multipart", ct)
	}

	reader := multipart.NewReader(bytes.NewReader(body), extractBoundary(ct))
	hasAudio := false
	var total int
	for {
		p, err := reader.NextPart()
		if err != nil {
			break
		}
		data, _ := sttReadAll(p)
		p.Close()
		total++
		if p.FormName() == "audio" && string(data) == "fake audio" {
			hasAudio = true
		}
	}
	if !hasAudio {
		t.Fatalf("missing audio part")
	}
	if total != 2 {
		t.Fatalf("expected 2 parts, got %d", total)
	}
}

func TestCloudflareSTT_NormalizeResponse(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		want string
	}{
		{
			name: "openai shaped",
			body: []byte(`{"text":"hello"}`),
			want: `{"text":"hello"}`,
		},
		{
			name: "wrapped string result",
			body: []byte(`{"result":"hello world"}`),
			want: `{"text":"hello world"}`,
		},
		{
			name: "wrapped object result",
			body: []byte(`{"result":{"text":"hello object"}}`),
			want: `{"text":"hello object"}`,
		},
		{
			name: "deepgram result",
			body: []byte(`{"result":{"results":[{"channels":[{"alternatives":[{"transcript":"deepgram text"}]}]}]}}`),
			want: `{"text":"deepgram text"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(normalizeCloudflareSTTResponse(tt.body))
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCloudflareSTT_ExecuteReturnsText(t *testing.T) {
	var called string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":{"text":"hello from cf"}}`))
	}))
	defer ts.Close()

	orig := validateURL
	validateURL = func(string) error { return nil }
	defer func() { validateURL = orig }()

	exec := NewCloudflareSTTExecutor(NewBaseExecutor())

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, _ := writer.CreateFormFile("file", "audio.mp3")
	_, _ = part.Write([]byte("fake audio"))
	writer.Close()

	req := &Request{
		Model:                "openai/whisper",
		Body:                 buf.Bytes(),
		Provider:             "cf",
		BaseURL:              ts.URL + "/accounts/abc/ai/v1/chat/completions",
		ProviderSpecificData: map[string]string{"accountId": "abc"},
		Headers:              map[string]string{"Content-Type": writer.FormDataContentType()},
	}
	resp, err := exec.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.HasSuffix(called, "/ai/run/@cf/openai/whisper") {
		t.Fatalf("called path = %q", called)
	}
	want := `{"text":"hello from cf"}`
	if string(resp.Body) != want {
		t.Fatalf("body = %q, want %q", string(resp.Body), want)
	}
	if ct := resp.Headers.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
}

func sttReadAll(r interface{ Read([]byte) (int, error) }) ([]byte, error) {
	var b bytes.Buffer
	_, err := b.ReadFrom(r)
	return b.Bytes(), err
}

func extractBoundary(ct string) string {
	parts := strings.Split(ct, ";")
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if strings.HasPrefix(p, "boundary=") {
			return strings.Trim(strings.TrimPrefix(p, "boundary="), `"`)
		}
	}
	return ""
}
