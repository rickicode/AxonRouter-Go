package quota

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	githubCopilotTokenURL = "https://api.github.com/copilot_internal/v2/token"
	githubCopilotUserURL  = "https://api.github.com/copilot_internal/user"

	githubCopilotAPIVersion    = "2026-06-01"
	githubCopilotEditorVersion = "vscode/1.126.0"
	githubCopilotPluginVersion = "copilot-chat/0.54.0"
	githubCopilotUserAgent     = "GitHubCopilotChat/0.54.0"
	githubCopilotRefreshAgent  = "GithubCopilot/1.0"
	githubCopilotRefreshPlugin = "copilot/1.388.0"
)

// copilotTokenResponse mirrors the short-lived Copilot token returned by GitHub.
type copilotTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	Endpoints struct {
		API string `json:"api"`
	} `json:"endpoints"`
}

// getCopilotToken reads the cached Copilot token and its expiry from provider-specific data.
func getCopilotToken(psd map[string]any) (string, int64) {
	tok := getStringField(psd, "copilotToken")

	var expiresAt int64
	if s := getStringField(psd, "copilotTokenExpiresAt"); s != "" {
		expiresAt, _ = strconv.ParseInt(s, 10, 64)
	} else {
		expiresAt = int64(getNumberField(psd, "copilotTokenExpiresAt"))
	}
	return tok, expiresAt
}

// refreshCopilotTokenIfNeeded returns a valid Copilot token, refreshing it via
// GitHub's internal endpoint when the cached token is missing or near expiry.
// If a refresh happens, the DB provider_specific_data is updated so the
// dashboard token timer and the executor stay in sync.
func refreshCopilotTokenIfNeeded(db *sql.DB, connectionID, accessToken string, psd map[string]any) (map[string]any, string, error) {
	tok, exp := getCopilotToken(psd)
	now := time.Now().Unix()
	if tok != "" && exp > now+300 {
		return psd, tok, nil
	}

	newTok, newExp, endpoint, err := fetchGitHubCopilotToken(accessToken)
	if err != nil {
		return psd, "", fmt.Errorf("refresh copilot token: %w", err)
	}

	if psd == nil {
		psd = map[string]any{}
	}
	psd["copilotToken"] = newTok
	psd["copilotTokenExpiresAt"] = strconv.FormatInt(newExp, 10)
	if endpoint != "" {
		psd["copilotEndpointAPI"] = endpoint
	}

	if db != nil && connectionID != "" {
		b, err := json.Marshal(psd)
		if err == nil {
			if _, err := db.Exec(`
				UPDATE connections
				SET provider_specific_data = ?, updated_at = ?
				WHERE id = ?
			`, b, now, connectionID); err != nil {
				log.Printf("quota: failed to persist refreshed Copilot token for %s: %v", connectionID, err)
			}
		}
	}

	return psd, newTok, nil
}

// fetchGitHubCopilotToken exchanges a GitHub OAuth access token for a short-lived Copilot token.
// parseGitHubForbiddenMessage converts a GitHub 403 JSON body into a short,
// user-facing reason. It recognizes Copilot access-denied responses.
func parseGitHubForbiddenMessage(body []byte) string {
	var details struct {
		Message      string `json:"message"`
		ErrorDetails struct {
			Message string `json:"message"`
			Title   string `json:"title"`
		} `json:"error_details"`
	}
	if err := json.Unmarshal(body, &details); err != nil {
		return ""
	}
	if strings.Contains(details.Message, "Resource not accessible by integration") {
		return "this GitHub account does not have GitHub Copilot access"
	}
	if details.ErrorDetails.Message != "" {
		return strings.ToLower(details.ErrorDetails.Title + ": " + details.ErrorDetails.Message)
	}
	if strings.Contains(details.Message, "Terms of Service") {
		return "GitHub Copilot access is blocked for this account"
	}
	return ""
}

func fetchGitHubCopilotToken(accessToken string) (string, int64, string, error) {
	req, err := http.NewRequest(http.MethodGet, githubCopilotTokenURL, nil)
	if err != nil {
		return "", 0, "", err
	}
	req.Header.Set("Authorization", "token "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", githubCopilotRefreshAgent)
	req.Header.Set("Editor-Version", githubCopilotEditorVersion)
	req.Header.Set("Editor-Plugin-Version", githubCopilotRefreshPlugin)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, "", err
	}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusForbidden {
			if msg := parseGitHubForbiddenMessage(body); msg != "" {
				return "", 0, "", fmt.Errorf("access denied: %s", msg)
			}
		}
		return "", 0, "", fmt.Errorf("copilot token endpoint returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var r copilotTokenResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return "", 0, "", fmt.Errorf("parse copilot token: %w", err)
	}
	if r.Token == "" {
		return "", 0, "", fmt.Errorf("copilot token endpoint returned empty token")
	}
	if r.ExpiresAt == 0 {
		r.ExpiresAt = time.Now().Add(time.Hour).Unix()
	}
	return r.Token, r.ExpiresAt, r.Endpoints.API, nil
}

// fetchCopilotQuota fetches Copilot usage from GitHub's internal /user endpoint.
// It expects a valid Copilot token (obtained via refreshCopilotTokenIfNeeded).
func fetchCopilotQuota(copilotToken string) ([]QuotaItem, string, error) {
	req, err := http.NewRequest(http.MethodGet, githubCopilotUserURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+copilotToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-GitHub-Api-Version", githubCopilotAPIVersion)
	req.Header.Set("User-Agent", githubCopilotUserAgent)
	req.Header.Set("Editor-Version", githubCopilotEditorVersion)
	req.Header.Set("Editor-Plugin-Version", githubCopilotPluginVersion)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("copilot user api: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("copilot user api %d: %s", resp.StatusCode, string(body))
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, "", fmt.Errorf("parse copilot user: %w", err)
	}

	return parseCopilotQuotas(data)
}

// parseCopilotQuotas converts GitHub's /copilot_internal/user response into QuotaItems.
// It handles both paid (quota_snapshots) and free/limited (monthly_quotas + limited_user_quotas) formats.
func parseCopilotQuotas(data map[string]any) ([]QuotaItem, string, error) {
	plan := getStringField(data, "copilot_plan", "access_type_sku", "plan")
	resetAt := getStringField(data, "quota_reset_date", "limited_user_reset_date", "reset_date")

	if snapshots := getMapField(data, "quota_snapshots"); snapshots != nil {
		quotas := make([]QuotaItem, 0, len(snapshots))
		for name, raw := range snapshots {
			snap, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			total := getNumberField(snap, "entitlement")
			remaining := getNumberField(snap, "remaining")
			unlimited := false
			if u, ok := snap["unlimited"].(bool); ok {
				unlimited = u
			}
			used := total - remaining
			remainingPct := 0.0
			if total > 0 {
				remainingPct = (remaining / total) * 100
			}
			quotas = append(quotas, QuotaItem{
				Name:         name,
				Used:         used,
				Total:        total,
				RemainingPct: remainingPct,
				ResetAt:      resetAt,
				Unlimited:    unlimited,
				Family:       "copilot",
			})
		}
		if len(quotas) > 0 {
			return quotas, plan, nil
		}
	}

	monthly := getMapField(data, "monthly_quotas")
	limited := getMapField(data, "limited_user_quotas")
	if monthly != nil {
		quotas := make([]QuotaItem, 0, len(monthly))
		for name, totalRaw := range monthly {
			total := 0.0
			switch n := totalRaw.(type) {
			case float64:
				total = n
			case int:
				total = float64(n)
			case int64:
				total = float64(n)
			}
			used := 0.0
			if limited != nil {
				used = getNumberField(limited, name)
			}
			remaining := total - used
			remainingPct := 0.0
			if total > 0 {
				remainingPct = (remaining / total) * 100
			}
			quotas = append(quotas, QuotaItem{
				Name:         name,
				Used:         used,
				Total:        total,
				RemainingPct: remainingPct,
				ResetAt:      resetAt,
				Family:       "copilot",
			})
		}
		if len(quotas) > 0 {
			return quotas, plan, nil
		}
	}

	return nil, plan, fmt.Errorf("GitHub Copilot connected. Unable to parse quota data.")
}
