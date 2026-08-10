package quota

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchQoderQuota_NonPATAccessTokenDirect(t *testing.T) {
	clearQoderJobTokenCache()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer oauth-token" {
			t.Errorf("expected Authorization Bearer oauth-token, got %q", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"userType":        "individual",
			"plan":            "PLAN_TIER_PRO",
			"quota":           1000,
			"isQuotaExceeded": false,
			"nextResetAt":     "2030-01-01T00:00:00Z",
		})
	}))
	defer ts.Close()

	t.Setenv("QODER_JOB_TOKEN_EXCHANGE_URL", ts.URL+"/exchange")
	t.Setenv("QODER_USER_STATUS_URL", ts.URL+"/status")

	quotas, plan, err := fetchQoderQuota("oauth-token", nil)
	if err != nil {
		t.Fatalf("fetchQoderQuota failed: %v", err)
	}
	if plan != "Pro" {
		t.Errorf("expected plan Pro, got %q", plan)
	}
	if len(quotas) != 1 {
		t.Fatalf("expected 1 quota item, got %d", len(quotas))
	}
	qi := quotas[0]
	if qi.Name != "Requests" {
		t.Errorf("expected name Requests, got %q", qi.Name)
	}
	if qi.Total != 1000 {
		t.Errorf("expected total 1000, got %f", qi.Total)
	}
	if qi.Used != 0 {
		t.Errorf("expected used 0, got %f", qi.Used)
	}
	if qi.RemainingPct != 100 {
		t.Errorf("expected remaining pct 100, got %f", qi.RemainingPct)
	}
	if qi.Unlimited {
		t.Error("expected unlimited false")
	}
	if !strings.HasPrefix(qi.ResetAt, "2030-01-01") {
		t.Errorf("expected resetAt to start with 2030-01-01, got %q", qi.ResetAt)
	}
}

func TestFetchQoderQuota_PATExchangeSuccess(t *testing.T) {
	clearQoderJobTokenCache()

	var exchangeBody map[string]string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/exchange":
			if r.Method != http.MethodPost {
				t.Errorf("exchange expected POST, got %s", r.Method)
			}
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &exchangeBody); err != nil {
				t.Errorf("invalid exchange request body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"jobToken":  "jt-abc123",
					"expiresIn": 3600,
				},
			})
		case "/status":
			if auth := r.Header.Get("Authorization"); auth != "Bearer jt-abc123" {
				t.Errorf("status expected Authorization Bearer jt-abc123, got %q", auth)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"userType":        "individual",
				"plan":            "PLAN_TIER_PRO",
				"quota":           500,
				"isQuotaExceeded": false,
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	t.Setenv("QODER_JOB_TOKEN_EXCHANGE_URL", ts.URL+"/exchange")
	t.Setenv("QODER_USER_STATUS_URL", ts.URL+"/status")

	quotas, plan, err := fetchQoderQuota("pt-secret", nil)
	if err != nil {
		t.Fatalf("fetchQoderQuota failed: %v", err)
	}
	if exchangeBody["personal_token"] != "pt-secret" {
		t.Errorf("expected personal_token pt-secret, got %q", exchangeBody["personal_token"])
	}
	if plan != "Pro" {
		t.Errorf("expected plan Pro, got %q", plan)
	}
	if len(quotas) != 1 {
		t.Fatalf("expected 1 quota item, got %d", len(quotas))
	}
	if quotas[0].Name != "Requests" || quotas[0].Total != 500 {
		t.Errorf("unexpected quota item: %+v", quotas[0])
	}
}

func TestFetchQoderQuota_PATExchangeFailureGracefulFallback(t *testing.T) {
	clearQoderJobTokenCache()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/exchange":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("exchange unavailable"))
		case "/status":
			// The fetcher should fall back to the original PAT.
			if auth := r.Header.Get("Authorization"); auth != "Bearer pt-fallback" {
				t.Errorf("status expected Authorization Bearer pt-fallback, got %q", auth)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"userType":        "individual",
				"plan":            "PLAN_TIER_FREE",
				"quota":           100,
				"isQuotaExceeded": false,
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	t.Setenv("QODER_JOB_TOKEN_EXCHANGE_URL", ts.URL+"/exchange")
	t.Setenv("QODER_USER_STATUS_URL", ts.URL+"/status")

	quotas, plan, err := fetchQoderQuota("pt-fallback", nil)
	if err != nil {
		t.Fatalf("fetchQoderQuota failed: %v", err)
	}
	if plan != "Free" {
		t.Errorf("expected plan Free, got %q", plan)
	}
	if len(quotas) != 1 {
		t.Fatalf("expected 1 quota item, got %d", len(quotas))
	}
	if quotas[0].Name != "Requests" || quotas[0].Total != 100 {
		t.Errorf("unexpected quota item: %+v", quotas[0])
	}
}

func TestFetchQoderQuota_QuotaExceeded(t *testing.T) {
	clearQoderJobTokenCache()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"userType":        "individual",
			"plan":            "PLAN_TIER_PRO",
			"quota":           100,
			"isQuotaExceeded": true,
		})
	}))
	defer ts.Close()

	t.Setenv("QODER_JOB_TOKEN_EXCHANGE_URL", ts.URL+"/exchange")
	t.Setenv("QODER_USER_STATUS_URL", ts.URL+"/status")

	quotas, plan, err := fetchQoderQuota("oauth-exceeded", nil)
	if err != nil {
		t.Fatalf("fetchQoderQuota failed: %v", err)
	}
	if plan != "Pro" {
		t.Errorf("expected plan Pro, got %q", plan)
	}
	if len(quotas) != 1 {
		t.Fatalf("expected 1 quota item, got %d", len(quotas))
	}
	qi := quotas[0]
	if qi.Name != "Quota" {
		t.Errorf("expected name Quota, got %q", qi.Name)
	}
	if qi.Used != 100 || qi.Total != 100 {
		t.Errorf("expected used=total=100, got used=%f total=%f", qi.Used, qi.Total)
	}
	if qi.RemainingPct != 0 {
		t.Errorf("expected remaining pct 0, got %f", qi.RemainingPct)
	}
	if qi.Unlimited {
		t.Error("expected unlimited false for exceeded quota")
	}
}

func TestFetchQoderQuota_PooledPlan(t *testing.T) {
	clearQoderJobTokenCache()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"userType":        "teams",
			"plan":            "PLAN_TIER_TEAM",
			"quota":           0,
			"isQuotaExceeded": false,
		})
	}))
	defer ts.Close()

	t.Setenv("QODER_JOB_TOKEN_EXCHANGE_URL", ts.URL+"/exchange")
	t.Setenv("QODER_USER_STATUS_URL", ts.URL+"/status")

	quotas, plan, err := fetchQoderQuota("oauth-team", nil)
	if err != nil {
		t.Fatalf("fetchQoderQuota failed: %v", err)
	}
	if plan != "Team" {
		t.Errorf("expected plan Team, got %q", plan)
	}
	if len(quotas) != 1 {
		t.Fatalf("expected 1 quota item, got %d", len(quotas))
	}
	qi := quotas[0]
	if qi.Name != "Plan" {
		t.Errorf("expected name Plan, got %q", qi.Name)
	}
	if !qi.Unlimited {
		t.Error("expected unlimited true for pooled plan")
	}
	if qi.RemainingPct != 100 {
		t.Errorf("expected remaining pct 100, got %f", qi.RemainingPct)
	}
	if qi.Total != 0 || qi.Used != 0 {
		t.Errorf("expected used=total=0, got used=%f total=%f", qi.Used, qi.Total)
	}
}
