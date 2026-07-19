package v1

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/config"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

func newDeviceTracker() *usage.DeviceTracker {
	return usage.NewDeviceTracker(config.Config{
		DeviceTrackerTTLMs:     60000,
		DeviceTrackerMaxPerKey: 10,
		DeviceTrackerMaxTotal:  100,
	})
}

func TestChatCompletions_TracksDevice(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	dt := newDeviceTracker()
	h.deviceTracker = dt

	fe := &fakeExecutor{
		responses: []struct {
			resp *executor.Response
			err  error
		}{
			{resp: &executor.Response{StatusCode: http.StatusOK, Body: []byte(`{"id":"x"}`)}},
		},
	}
	executor.GetRegistry().Register("dtchat", executor.FormatOpenAI, fe)
	defer executor.GetRegistry().Unregister("dtchat")

	seedProviderAndConnection(t, h, "dtchat", `["llm"]`, "dtchat-conn", "http://unused")

	body := []byte(`{"model":"dtchat/model","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", "dt-chat-agent")
	c.Request.Header.Set("X-Forwarded-For", "203.0.113.42")
	c.Set("api_key_id", "key-dt-chat")

	h.ChatCompletions(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := dt.GetDeviceCount("key-dt-chat"); got != 1 {
		t.Fatalf("expected 1 device for api key, got %d", got)
	}
	details := dt.GetDeviceDetails("key-dt-chat")
	if len(details) != 1 {
		t.Fatalf("expected 1 device detail, got %d", len(details))
	}
	if details[0].UserAgent != "dt-chat-agent" {
		t.Errorf("user agent = %q, want dt-chat-agent", details[0].UserAgent)
	}
	if details[0].IP != "203.0.x.x" {
		t.Errorf("masked ip = %q, want 203.0.x.x", details[0].IP)
	}
}

func TestEmbeddings_TracksDevice(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	dt := newDeviceTracker()
	h.deviceTracker = dt

	fe := &fakeEmbeddingsExecutor{
		BaseExecutor: executor.NewBaseExecutor(),
		body:         []byte(`{"object":"list","data":[],"usage":{"prompt_tokens":5,"total_tokens":5}}`),
	}
	executor.GetRegistry().Register("dtembed", executor.FormatOpenAI, fe)
	defer executor.GetRegistry().Unregister("dtembed")

	seedProviderAndConnection(t, h, "dtembed", `["llm","embedding"]`, "dtembed-conn", "http://unused")

	body := []byte(`{"model":"dtembed/model","input":"hello"}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", "dt-embed-agent")
	c.Request.Header.Set("X-Forwarded-For", "198.51.100.7")
	c.Set("api_key_id", "key-dt-embed")

	h.Embeddings(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := dt.GetDeviceCount("key-dt-embed"); got != 1 {
		t.Fatalf("expected 1 device for api key, got %d", got)
	}
}

func TestChatCompletions_DoesNotTrackWithoutAPIKey(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	dt := newDeviceTracker()
	h.deviceTracker = dt

	fe := &fakeExecutor{
		responses: []struct {
			resp *executor.Response
			err  error
		}{
			{resp: &executor.Response{StatusCode: http.StatusOK, Body: []byte(`{"id":"x"}`)}},
		},
	}
	executor.GetRegistry().Register("dtnokey", executor.FormatOpenAI, fe)
	defer executor.GetRegistry().Unregister("dtnokey")

	seedProviderAndConnection(t, h, "dtnokey", `["llm"]`, "dtnokey-conn", "http://unused")

	body := []byte(`{"model":"dtnokey/model","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", "dt-nokey-agent")
	c.Request.Header.Set("X-Forwarded-For", "203.0.113.99")

	h.ChatCompletions(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	total := 0
	for _, key := range []string{"", "key-dt-nokey"} {
		total += dt.GetDeviceCount(key)
	}
	if total != 0 {
		t.Fatalf("expected no devices tracked without api key, got %d", total)
	}
}
