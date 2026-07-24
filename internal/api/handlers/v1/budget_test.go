package v1

import (
	"net/http"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

func seedAPIKeyBudget(t *testing.T, h *Handler, apiKeyID string, dailyLimit, monthlyLimit, warningThreshold float64) {
	t.Helper()
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO api_keys (id, key_hash, key_value, name, rate_limit_per_min, is_active, created_at) VALUES (?, 'hash', 'secret', 'Budget Key', 0, 1, 0)`, apiKeyID); err != nil {
		t.Fatalf("seed api key: %v", err)
	}
	if _, err := h.db.Exec(`
		INSERT INTO api_key_budgets (api_key_id, daily_limit_usd, monthly_limit_usd, warning_threshold, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(api_key_id) DO UPDATE SET
			daily_limit_usd = excluded.daily_limit_usd,
			monthly_limit_usd = excluded.monthly_limit_usd,
			warning_threshold = excluded.warning_threshold,
			updated_at = excluded.updated_at
	`, apiKeyID, dailyLimit, monthlyLimit, warningThreshold, time.Now().Unix()); err != nil {
		t.Fatalf("seed api key budget: %v", err)
	}
}

func seedAPIKeySpend(t *testing.T, h *Handler, apiKeyID string, cost float64) {
	t.Helper()
	now := time.Now().UTC()
	idPrefix := spendRecordID(apiKeyID, now)
	if _, err := h.db.Exec(`
		INSERT INTO api_key_spend_history (id, api_key_id, cost_usd, period_type, period_start, created_at)
		VALUES (?, ?, ?, 'day', ?, ?), (?, ?, ?, 'month', ?, ?)
	`, idPrefix+"-d", apiKeyID, cost, budgetPeriodStart(now, "day").Unix(), now.Unix(),
		idPrefix+"-m", apiKeyID, cost, budgetPeriodStart(now, "month").Unix(), now.Unix()); err != nil {
		t.Fatalf("seed spend history: %v", err)
	}
}

func TestCheckAPIKeyBudget_DailyLimitExceeded(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	seedAPIKeyBudget(t, h, "key-daily", 1.0, 100.0, 0.8)
	seedAPIKeySpend(t, h, "key-daily", 1.0)

	body := []byte(`{"model":"openai/gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	c, rec := budgetContext(t, http.MethodPost, "/v1/chat/completions", body)
	c.Set("api_key_id", "key-daily")

	if err := h.checkAPIKeyBudget(c); err == nil {
		t.Fatal("expected checkAPIKeyBudget to return error")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCheckAPIKeyBudget_MonthlyLimitExceeded(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	seedAPIKeyBudget(t, h, "key-monthly", 100.0, 1.0, 0.8)
	seedAPIKeySpend(t, h, "key-monthly", 1.0)

	body := []byte(`{"model":"openai/gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	c, rec := budgetContext(t, http.MethodPost, "/v1/chat/completions", body)
	c.Set("api_key_id", "key-monthly")

	if err := h.checkAPIKeyBudget(c); err == nil {
		t.Fatal("expected checkAPIKeyBudget to return error")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCheckAPIKeyBudget_AllowedWhenUnderLimit(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	seedAPIKeyBudget(t, h, "key-ok", 10.0, 100.0, 0.8)
	seedAPIKeySpend(t, h, "key-ok", 1.0)

	body := []byte(`{"model":"openai/gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	c, _ := budgetContext(t, http.MethodPost, "/v1/chat/completions", body)
	c.Set("api_key_id", "key-ok")

	if err := h.checkAPIKeyBudget(c); err != nil {
		t.Fatalf("expected checkAPIKeyBudget to allow, got %v", err)
	}
	if c.IsAborted() {
		t.Fatal("expected context not to be aborted")
	}
}

func TestRecordAPIKeyCost_InsertsSpendHistory(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	seedAPIKeyBudget(t, h, "key-cost", 0, 0, 0.8)

	h.recordAPIKeyCost("key-cost", "gpt-4o", 1000, 500, 0, 0, 0, 0)

	var count int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM api_key_spend_history WHERE api_key_id = ?`, "key-cost").Scan(&count); err != nil {
		t.Fatalf("query spend history: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 spend history rows (day+month), got %d", count)
	}

	var total float64
	if err := h.db.QueryRow(`SELECT COALESCE(SUM(cost_usd), 0) FROM api_key_spend_history WHERE api_key_id = ?`, "key-cost").Scan(&total); err != nil {
		t.Fatalf("sum spend history: %v", err)
	}
	expected := 2 * usage.EstimateCost("gpt-4o", "chat", 0, 1000, 500, 0, 0, 0)
	if total <= 0 || total != expected {
		t.Fatalf("expected total cost %v, got %v", expected, total)
	}
}

func TestRecordAPIKeyCost_UsesResponseCost(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	seedAPIKeyBudget(t, h, "key-resp-cost", 0, 0, 0.8)

	h.recordAPIKeyCost("key-resp-cost", "gpt-4o", 1000, 500, 0, 0, 0, 0.123)

	var cost float64
	if err := h.db.QueryRow(`SELECT COALESCE(SUM(cost_usd), 0) FROM api_key_spend_history WHERE api_key_id = ?`, "key-resp-cost").Scan(&cost); err != nil {
		t.Fatalf("sum spend history: %v", err)
	}
	if cost != 2*0.123 {
		t.Fatalf("expected total cost %v, got %v", 2*0.123, cost)
	}
}

func TestChatCompletions_DailyBudgetExhausted(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	wq := db.NewWriteQueue(h.db)
	tracker := usage.NewTracker(h.db)
	tracker.SetWriteQueue(wq)
	h.tracker = tracker
	defer func() {
		tracker.Stop()
		wq.Stop()
	}()
	seedAPIKeyBudget(t, h, "key-chat-budget", 1.0, 100.0, 0.8)
	seedAPIKeySpend(t, h, "key-chat-budget", 1.0)

	seedProviderAndConnection(t, h, "openai", `["llm"]`, "openai-budget-conn", "http://unused")

	body := []byte(`{"model":"openai/gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	c, rec := budgetContext(t, http.MethodPost, "/v1/chat/completions", body)
	c.Set("api_key_id", "key-chat-budget")

	h.ChatCompletions(c)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestChatCompletions_CostRecordedOnSuccess(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	wq := db.NewWriteQueue(h.db)
	tracker := usage.NewTracker(h.db)
	tracker.SetWriteQueue(wq)
	h.tracker = tracker
	defer func() {
		tracker.Stop()
		wq.Stop()
	}()
	seedAPIKeyBudget(t, h, "key-chat-record", 100.0, 1000.0, 0.8)

	fe := &fakeExecutor{
		responses: []struct {
			resp *executor.Response
			err  error
		}{{
			resp: &executor.Response{
				StatusCode: http.StatusOK,
				Body:       []byte(`{"choices":[{"message":{"content":"hello"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`),
			},
		}},
	}
	executor.GetRegistry().Register("cost", executor.FormatOpenAI, fe)
	defer executor.GetRegistry().Unregister("cost")

	seedProviderAndConnection(t, h, "cost", `["llm"]`, "cost-conn", "http://unused")

	body := []byte(`{"model":"cost/gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	c, rec := budgetContext(t, http.MethodPost, "/v1/chat/completions", body)
	c.Set("api_key_id", "key-chat-record")

	h.ChatCompletions(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var count int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM api_key_spend_history WHERE api_key_id = ?`, "key-chat-record").Scan(&count); err != nil {
		t.Fatalf("query spend history: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 spend history rows, got %d", count)
	}
}

