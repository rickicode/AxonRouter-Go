package quota

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	codebuddyAPIEndpoint  = "https://www.codebuddy.ai"
	codebuddyDefaultQuotaTimeout = 15 * time.Second
)

// fetchCodeBuddyQuota fetches CodeBuddy account quota using the same
// billing/meter endpoints used by cockpit-tools:
//   - /v2/billing/meter/get-payment-type for the plan name
//   - /v2/billing/meter/get-user-resource for the credit/resource balance
func fetchCodeBuddyQuota(accessToken string, psd map[string]any) ([]QuotaItem, string, error) {
	if accessToken == "" {
		return nil, "", fmt.Errorf("codebuddy: no access token")
	}

	domain := stringAny(psd["domain"])
	if domain == "" {
		domain = "www.codebuddy.ai"
	}
	uid := stringAny(psd["uid"])
	enterpriseID := stringAny(psd["enterprise_id"])

	client := &http.Client{Timeout: codebuddyDefaultQuotaTimeout}

	// If the OAuth flow didn't capture a uid, fall back to the accounts endpoint.
	if uid == "" {
		account, err := fetchCodeBuddyAccount(client, accessToken, domain)
		if err == nil {
			uid = account.UID
			if enterpriseID == "" {
				enterpriseID = account.EnterpriseID
			}
		}
	}

	plan := ""
	paymentType, err := fetchCodeBuddyPaymentType(client, accessToken, uid, enterpriseID, domain)
	if err == nil && paymentType != "" {
		plan = paymentType
	}

	quotas, err := fetchCodeBuddyUserResource(client, accessToken, uid, enterpriseID, domain)
	if err != nil {
		return nil, plan, err
	}

	return quotas, plan, nil
}

type codebuddyAccountEntry struct {
	UID            string `json:"uid"`
	EnterpriseID   string `json:"enterpriseId"`
}

type codebuddyAccountsResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Accounts []codebuddyAccountEntry `json:"accounts"`
	} `json:"data"`
}

func fetchCodeBuddyAccount(client *http.Client, accessToken, domain string) (codebuddyAccountEntry, error) {
	var empty codebuddyAccountEntry
	url := codebuddyAPIEndpoint + "/v2/plugin/accounts"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return empty, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-Domain", domain)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return empty, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return empty, err
	}
	if resp.StatusCode != http.StatusOK {
		return empty, fmt.Errorf("accounts %d: %s", resp.StatusCode, string(body))
	}

	var res codebuddyAccountsResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return empty, err
	}
	if res.Code != 0 {
		return empty, fmt.Errorf("accounts error %d: %s", res.Code, res.Msg)
	}
	for _, a := range res.Data.Accounts {
		if a.UID != "" {
			return a, nil
		}
	}
	if len(res.Data.Accounts) > 0 {
		return res.Data.Accounts[0], nil
	}
	return empty, fmt.Errorf("no accounts")
}

type codebuddyPaymentTypeResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		PaymentType string `json:"paymentType"`
	} `json:"data"`
}

func fetchCodeBuddyPaymentType(client *http.Client, accessToken, uid, enterpriseID, domain string) (string, error) {
	url := codebuddyAPIEndpoint + "/v2/billing/meter/get-payment-type"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Domain", domain)
	if uid != "" {
		req.Header.Set("X-User-Id", uid)
	}
	if enterpriseID != "" {
		req.Header.Set("X-Enterprise-Id", enterpriseID)
		req.Header.Set("X-Tenant-Id", enterpriseID)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("payment type %d: %s", resp.StatusCode, string(body))
	}

	var res codebuddyPaymentTypeResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return "", err
	}
	if res.Code != 0 {
		return "", fmt.Errorf("payment type error %d: %s", res.Code, res.Msg)
	}
	return stringAny(res.Data.PaymentType), nil
}

type codebuddyUserResourceResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Response struct {
			Data struct {
				Accounts []codebuddyResourceAccount `json:"Accounts"`
			} `json:"Data"`
		} `json:"Response"`
	} `json:"data"`
}

type codebuddyResourceAccount struct {
	PackageName   string `json:"PackageName"`
	CapacitySize  float64 `json:"CapacitySize"`
	CapacityUsed  float64 `json:"CapacityUsed"`
	CapacityRemain float64 `json:"CapacityRemain"`
}

func fetchCodeBuddyUserResource(client *http.Client, accessToken, uid, enterpriseID, domain string) ([]QuotaItem, error) {
	url := codebuddyAPIEndpoint + "/v2/billing/meter/get-user-resource"
	payload := map[string]any{
		"PageNumber":               1,
		"PageSize":                 100,
		"ProductCode":              "",
		"Status":                   []int{},
		"PackageEndTimeRangeBegin": "",
		"PackageEndTimeRangeEnd":   "",
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Domain", domain)
	if uid != "" {
		req.Header.Set("X-User-Id", uid)
	}
	if enterpriseID != "" {
		req.Header.Set("X-Enterprise-Id", enterpriseID)
		req.Header.Set("X-Tenant-Id", enterpriseID)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("user resource %d: %s", resp.StatusCode, string(body))
	}

	var res codebuddyUserResourceResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}
	if res.Code != 0 {
		return nil, fmt.Errorf("user resource error %d: %s", res.Code, res.Msg)
	}

	accounts := res.Data.Response.Data.Accounts
	if len(accounts) == 0 {
		return nil, fmt.Errorf("no resource accounts")
	}

	items := make([]QuotaItem, 0, len(accounts))
	for _, acc := range accounts {
		name := acc.PackageName
		if name == "" {
			name = "Credits"
		}
		total := acc.CapacitySize
		used := acc.CapacityUsed
		remaining := acc.CapacityRemain
		if remaining == 0 && total > 0 {
			remaining = total - used
		}
		pct := 0.0
		if total > 0 {
			pct = (remaining / total) * 100
		}
		items = append(items, QuotaItem{
			Name:         name,
			Used:         used,
			Total:        total,
			RemainingPct: pct,
		})
	}
	return items, nil
}
