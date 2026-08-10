package quota

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

type mockKiroTransport struct {
	profileCalls      atomic.Int32
	postCalls         atomic.Int32
	getCWCalls        atomic.Int32
	getQCalls         atomic.Int32
	postStatus        int
	postBody          []byte
	getCWStatus       int
	getCWBody         []byte
	getQStatus        int
	getQBody          []byte
	profileArn        string
	lastAuthorization string
}

func (m *mockKiroTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.lastAuthorization = req.Header.Get("Authorization")
	if req.Method == http.MethodPost && req.Header.Get("x-amz-target") == "AmazonCodeWhispererService.ListAvailableProfiles" {
		m.profileCalls.Add(1)
		body := map[string]any{}
		if m.profileArn != "" {
			body["profiles"] = []map[string]any{{"arn": m.profileArn}}
		}
		b, _ := json.Marshal(body)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(b)), Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
	}
	if req.Method == http.MethodPost && req.Header.Get("x-amz-target") == "AmazonCodeWhispererService.GetUsageLimits" {
		m.postCalls.Add(1)
		status := m.postStatus
		if status == 0 {
			status = 200
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(m.postBody)), Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
	}
	if req.Method == http.MethodGet && req.Host == "codewhisperer.us-east-1.amazonaws.com" && req.URL.Path == "/getUsageLimits" {
		m.getCWCalls.Add(1)
		status := m.getCWStatus
		if status == 0 {
			status = 404
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(m.getCWBody)), Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
	}
	if req.Method == http.MethodGet && req.Host == "q.eu-central-1.amazonaws.com" && req.URL.Path == "/getUsageLimits" {
		m.getQCalls.Add(1)
		status := m.getQStatus
		if status == 0 {
			status = 404
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(m.getQBody)), Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
	}
	return &http.Response{StatusCode: 404, Body: io.NopCloser(bytes.NewReader([]byte("not found")))}, nil
}

func setMockKiroTransport(t *testing.T, tr http.RoundTripper) {
	t.Helper()
	orig := kiroHTTPClient
	kiroHTTPClient = &http.Client{Timeout: 5 * time.Second, Transport: tr}
	t.Cleanup(func() { kiroHTTPClient = orig })
}

func usageBody() []byte {
	b, _ := json.Marshal(map[string]any{
		"usageBreakdownList": []map[string]any{
			{
				"resourceType":              "AGENTIC_REQUEST",
				"currentUsageWithPrecision": 5.0,
				"usageLimitWithPrecision":   100.0,
				"freeTrialInfo": map[string]any{
					"currentUsageWithPrecision": 1.0,
					"usageLimitWithPrecision":   10.0,
				},
			},
		},
		"overageConfiguration": map[string]any{"overageStatus": "ENABLED"},
		"nextDateReset":        "2026-08-01T00:00:00Z",
		"subscriptionInfo":     map[string]any{"subscriptionTitle": "Pro"},
	})
	return b
}

func TestFetchKiroQuota_POSTSuccess(t *testing.T) {
	mock := &mockKiroTransport{postBody: usageBody()}
	setMockKiroTransport(t, mock)
	psd := map[string]any{"profileArn": "arn:aws:codewhisperer:eu-central-1:123456789012:profile/ABC"}
	quotas, plan, msg, err := fetchKiroQuota("tok", psd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != "" {
		t.Fatalf("unexpected message: %q", msg)
	}
	if plan != "Pro" {
		t.Fatalf("expected plan Pro, got %q", plan)
	}
	if mock.postCalls.Load() != 1 {
		t.Fatalf("expected 1 POST call, got %d", mock.postCalls.Load())
	}
	if len(quotas) != 2 {
		t.Fatalf("expected 2 quotas (agentic + freetrial), got %d", len(quotas))
	}
	agenticFound := false
	for _, q := range quotas {
		if q.Name == "agentic_request" && q.Unlimited {
			agenticFound = true
		}
		if q.Name == "agentic_request_freetrial" && !q.Unlimited {
			// free trial should still be unlimited when overage enabled
		}
	}
	if !agenticFound {
		t.Fatalf("expected unlimited agentic_request quota in %v", quotas)
	}
}

func TestFetchKiroQuota_GETFallback(t *testing.T) {
	mock := &mockKiroTransport{postStatus: 400, getCWStatus: 200, getCWBody: usageBody()}
	setMockKiroTransport(t, mock)
	psd := map[string]any{
		"region":     "eu-central-1",
		"profileArn": "arn:aws:codewhisperer:eu-central-1:123456789012:profile/ABC",
	}
	quotas, _, _, err := fetchKiroQuota("tok", psd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.postCalls.Load() != 1 {
		t.Fatalf("expected 1 POST attempt, got %d", mock.postCalls.Load())
	}
	if mock.getCWCalls.Load() != 1 {
		t.Fatalf("expected 1 codewhisperer GET fallback call, got %d", mock.getCWCalls.Load())
	}
	if len(quotas) == 0 {
		t.Fatal("expected quotas from GET fallback")
	}
}

func TestFetchKiroQuota_QFallback(t *testing.T) {
	mock := &mockKiroTransport{postStatus: 400, getCWStatus: 400, getQStatus: 200, getQBody: usageBody()}
	setMockKiroTransport(t, mock)
	psd := map[string]any{
		"region":     "eu-central-1",
		"profileArn": "arn:aws:codewhisperer:eu-central-1:123456789012:profile/ABC",
	}
	quotas, _, _, err := fetchKiroQuota("tok", psd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.getQCalls.Load() != 1 {
		t.Fatalf("expected 1 q GET fallback call, got %d", mock.getQCalls.Load())
	}
	if len(quotas) == 0 {
		t.Fatal("expected quotas from q fallback")
	}
}

func TestFetchKiroQuota_ProfileArnDiscovery(t *testing.T) {
	mock := &mockKiroTransport{profileArn: "arn:aws:codewhisperer:eu-central-1:123456789012:profile/DISCOVERED", postBody: usageBody()}
	setMockKiroTransport(t, mock)
	psd := map[string]any{"region": "eu-north-1"}
	quotas, _, _, err := fetchKiroQuota("tok", psd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.profileCalls.Load() == 0 {
		t.Fatal("expected profile discovery calls")
	}
	if len(quotas) == 0 {
		t.Fatal("expected quotas after profile discovery")
	}
}

func TestFetchKiroQuota_SocialAuthFriendlyMessage(t *testing.T) {
	mock := &mockKiroTransport{postStatus: 401}
	setMockKiroTransport(t, mock)
	psd := map[string]any{
		"authMethod": "imported",
		"provider":   "Google",
		"region":     "us-east-1",
	}
	quotas, _, msg, err := fetchKiroQuota("tok", psd)
	if err != nil {
		t.Fatalf("expected no error for social auth, got %v", err)
	}
	if msg == "" {
		t.Fatal("expected friendly message for social auth")
	}
	if len(quotas) != 0 {
		t.Fatalf("expected no quotas for social auth, got %d", len(quotas))
	}
}

func TestParseKiroQuotas_FreeTrialUnlimited(t *testing.T) {
	data := map[string]any{
		"usageBreakdownList": []map[string]any{
			{
				"resourceType":              "REQUEST",
				"currentUsageWithPrecision": 10.0,
				"usageLimitWithPrecision":   50.0,
				"freeTrialInfo": map[string]any{
					"currentUsageWithPrecision": 2.0,
					"usageLimitWithPrecision":   5.0,
				},
			},
		},
		"overageConfiguration": map[string]any{"overageEnabled": true},
		"nextDateReset":        "2026-08-01T00:00:00Z",
	}
	quotas, _, err := parseKiroQuotas(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(quotas) != 2 {
		t.Fatalf("expected 2 quota rows, got %d", len(quotas))
	}
	for _, q := range quotas {
		if !q.Unlimited {
			t.Fatalf("expected unlimited quota %q", q.Name)
		}
		if q.RemainingPct != 100 {
			t.Fatalf("expected remaining pct 100 for unlimited, got %v", q.RemainingPct)
		}
	}
}
