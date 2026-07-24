package v1

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

func TestWriteCostHeaders_EstimatedFromPricing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	counts := StreamTokenCounts{InputTokens: 10, OutputTokens: 5}
	writeCostHeaders(c, "openai/gpt-4o", 0, counts, false, false)

	assertEstimatedHeader(t, rec, true)
	assertHeader(t, rec, tokensInHeader, "10")
	assertHeader(t, rec, tokensOutHeader, "5")
	cost, err := strconv.ParseFloat(rec.Header().Get(costHeader), 64)
	if err != nil {
		t.Fatalf("cost header is not numeric: %v", err)
	}
	expected := usage.EstimateCost("openai/gpt-4o", 10, 5, 0, 0, 0)
	if cost != expected {
		t.Fatalf("cost header = %v, want %v", cost, expected)
	}
}

func TestWriteCostHeaders_ExactCostNotEstimated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	counts := StreamTokenCounts{InputTokens: 10, OutputTokens: 5}
	writeCostHeaders(c, "openai/gpt-4o", 0.42, counts, false, false)

	assertEstimatedHeader(t, rec, false)
	assertHeader(t, rec, costHeader, "0.42")
}

func TestWriteJSONResponse_AttachesCostHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	h := &Handler{}

	body := []byte(`{"id":"x","model":"openai/gpt-4o","usage":{"prompt_tokens":2,"completion_tokens":1}}`)
	counts := ExtractTokensFromBody(body)
	h.writeJSONResponse(c, http.StatusOK, body, responseCost{
		modelID:         "openai/gpt-4o",
		exactCost:       0,
		counts:          counts,
		tokensEstimated: false,
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	assertHeader(t, rec, tokensInHeader, "2")
	assertHeader(t, rec, tokensOutHeader, "1")
	assertEstimatedHeader(t, rec, true)
}

func TestWriteCostTrailers_AfterStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	c.Header("Trailer", costTrailerNames)
	c.Header("Content-Type", "text/event-stream")
	c.Status(http.StatusOK)
	c.Writer.Write([]byte("data: [DONE]\n\n"))

	counts := StreamTokenCounts{InputTokens: 8, OutputTokens: 4}
	writeCostTrailers(c, "openai/gpt-4o", 0, counts, true, false)

	rec.Flush()
	resp := rec.Result()
	if resp.Trailer == nil {
		t.Fatalf("expected trailer map to be set")
	}
	assertHeaderMap(t, resp.Trailer, tokensInHeader, "8")
	assertHeaderMap(t, resp.Trailer, tokensOutHeader, "4")
	assertHeaderMap(t, resp.Trailer, costEstimatedHeader, "true")
}

func assertHeader(t *testing.T, rec *httptest.ResponseRecorder, name, want string) {
	t.Helper()
	if got := rec.Header().Get(name); got != want {
		t.Fatalf("header %s = %q, want %q", name, got, want)
	}
}

func assertHeaderMap(t *testing.T, h http.Header, name, want string) {
	t.Helper()
	if got := h.Get(name); got != want {
		t.Fatalf("trailer %s = %q, want %q", name, got, want)
	}
}

func assertEstimatedHeader(t *testing.T, rec *httptest.ResponseRecorder, want bool) {
	t.Helper()
	wantStr := "false"
	if want {
		wantStr = "true"
	}
	assertHeader(t, rec, costEstimatedHeader, wantStr)
}
