package admin

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/proxypool"
)

type ProxyDeployHandler struct {
	db     *sql.DB
	health *proxypool.HealthChecker
}

func NewProxyDeployHandler(db *sql.DB, health *proxypool.HealthChecker) *ProxyDeployHandler {
	return &ProxyDeployHandler{db: db, health: health}
}

// DeployVercel deploys a relay edge function to Vercel.
// Reference: /workspaces/AxonRouter/src/app/api/proxy-pools/vercel-deploy/route.ts
func (h *ProxyDeployHandler) DeployVercel(c *gin.Context) {
	var req struct {
		VercelToken string `json:"vercelToken"`
		ProjectName string `json:"projectName"`
	}
	if c.ShouldBindJSON(&req) != nil || req.VercelToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Vercel API token is required"})
		return
	}
	projectName := req.ProjectName
	if projectName == "" {
		projectName = fmt.Sprintf("relay-%s", time.Now().Format("060102150405"))
	}

	relayAuth := proxypool.GenerateRelayAuth()
	source := buildRelayEdgeFunctionSource("vercel")

	// Create deployment with files (matching TS: api/relay.js + package.json + vercel.json)
	deployBody := map[string]any{
		"name": projectName,
		"files": []map[string]string{
			{"file": "api/relay.js", "data": source},
			{"file": "package.json", "data": fmt.Sprintf(`{"name":"%s","version":"1.0.0"}`, projectName)},
			{"file": "vercel.json", "data": `{"rewrites":[{"source":"/(.*)","destination":"/api/relay"}]}`},
		},
		"projectSettings": map[string]any{"framework": nil},
		"target":          "production",
	}
	body, _ := json.Marshal(deployBody)
	deployReq, _ := http.NewRequest("POST", "https://api.vercel.com/v13/deployments", bytes.NewReader(body))
	deployReq.Header.Set("Authorization", "Bearer "+req.VercelToken)
	deployReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(deployReq)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Vercel API error: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		errMsg := parseVercelError(respBody)
		c.JSON(resp.StatusCode, gin.H{"error": errMsg})
		return
	}

	var deployResp struct {
		ID        string `json:"id"`
		UID       string `json:"uid"`
		URL       string `json:"url"`
		ProjectID string `json:"projectId"`
	}
	json.Unmarshal(respBody, &deployResp)
	deploymentID := deployResp.ID
	if deploymentID == "" {
		deploymentID = deployResp.UID
	}

	// Disable deployment protection (TS: PATCH /v9/projects/{projectId})
	projectID := deployResp.ProjectID
	if projectID == "" {
		projectID = projectName
	}
	protBody, _ := json.Marshal(map[string]any{"ssoProtection": nil})
	protReq, _ := http.NewRequest("PATCH", fmt.Sprintf("https://api.vercel.com/v9/projects/%s", projectID), bytes.NewReader(protBody))
	protReq.Header.Set("Authorization", "Bearer "+req.VercelToken)
	protReq.Header.Set("Content-Type", "application/json")
	http.DefaultClient.Do(protReq) // best-effort

	// Poll until ready (TS: GET /v13/deployments/{id}, check readyState)
	readyURL, err := pollVercelDeployment(deploymentID, req.VercelToken, 120*time.Second)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Deployment poll failed: " + err.Error()})
		return
	}
	deployURL := fmt.Sprintf("https://%s", readyURL)

	// Test relay
	testRes := testRelayURL(deployURL, relayAuth)
	testedAt := time.Now().Format(time.RFC3339)

	// Save proxy pool
	poolID := saveDeployPool(h.db, projectName, "vercel", deployURL, relayAuth, testRes.OK, testedAt, testRes.Error)
	c.JSON(http.StatusCreated, gin.H{
		"proxyPoolId": poolID,
		"deployUrl":   deployURL,
		"relayAuth":   relayAuth,
		"relayTest":   testRes,
	})
}

// DeployDeno deploys a relay edge function to Deno Deploy.
// Reference: /workspaces/AxonRouter/src/app/api/proxy-pools/deno-deploy/route.ts
func (h *ProxyDeployHandler) DeployDeno(c *gin.Context) {
	var req struct {
		DenoToken   string `json:"denoToken"`
		OrgDomain   string `json:"orgDomain"`
		ProjectName string `json:"projectName"`
	}
	if c.ShouldBindJSON(&req) != nil || req.DenoToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Deno Deploy token is required"})
		return
	}
	if req.OrgDomain == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Organization domain is required"})
		return
	}
	projectName := req.ProjectName
	if projectName == "" {
		projectName = fmt.Sprintf("axonrelay-%s", time.Now().Format("060102150405"))
	}

	relayAuth := proxypool.GenerateRelayAuth()
	source := buildRelayEdgeFunctionSource("deno")

	// Create project (TS: POST /v2/apps)
	projBody, _ := json.Marshal(map[string]string{"name": projectName, "type": "playground"})
	projReq, _ := http.NewRequest("POST", "https://api.deno.com/v2/apps", bytes.NewReader(projBody))
	projReq.Header.Set("Authorization", "Bearer "+req.DenoToken)
	projReq.Header.Set("Content-Type", "application/json")
	projResp, err := http.DefaultClient.Do(projReq)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Deno API error: " + err.Error()})
		return
	}
	defer projResp.Body.Close()
	projRespBody, _ := io.ReadAll(projResp.Body)
	if projResp.StatusCode >= 400 {
		c.JSON(projResp.StatusCode, gin.H{"error": parseDenoError(projRespBody)})
		return
	}
	var projResult struct {
		ID string `json:"id"`
	}
	json.Unmarshal(projRespBody, &projResult)

	// Deploy relay function (TS: POST /v2/apps/{projectId}/deploy)
	deployBody, _ := json.Marshal(map[string]any{
		"entrypointUrl": "main.ts",
		"manifest":      map[string]any{},
		"assets": []map[string]string{
			{"kind": "file", "path": "main.ts", "content": source, "encoding": "utf-8"},
		},
		"envVars": map[string]string{"RELAY_AUTH": relayAuth},
	})
	deployReq, _ := http.NewRequest("POST", fmt.Sprintf("https://api.deno.com/v2/apps/%s/deploy", projResult.ID), bytes.NewReader(deployBody))
	deployReq.Header.Set("Authorization", "Bearer "+req.DenoToken)
	deployReq.Header.Set("Content-Type", "application/json")
	deployResp, err := http.DefaultClient.Do(deployReq)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Deno deploy error: " + err.Error()})
		return
	}
	defer deployResp.Body.Close()
	deployRespBody, _ := io.ReadAll(deployResp.Body)
	if deployResp.StatusCode >= 400 {
		c.JSON(deployResp.StatusCode, gin.H{"error": parseDenoError(deployRespBody)})
		return
	}
	var dResult struct {
		ID string `json:"id"`
	}
	json.Unmarshal(deployRespBody, &dResult)

	// Poll revision (TS: GET /v2/revisions/{id})
	pollDenoRevision(dResult.ID, req.DenoToken, 60*time.Second)

	deployURL := fmt.Sprintf("https://%s.%s.deno.net", projectName, req.OrgDomain)
	testRes := testRelayURL(deployURL, relayAuth)
	testedAt := time.Now().Format(time.RFC3339)

	poolID := saveDeployPool(h.db, projectName, "deno", deployURL, relayAuth, testRes.OK, testedAt, testRes.Error)
	c.JSON(http.StatusCreated, gin.H{
		"proxyPoolId": poolID,
		"deployUrl":   deployURL,
		"relayAuth":   relayAuth,
		"relayTest":   testRes,
	})
}

// DeployCloudflare deploys a relay worker to Cloudflare Workers.
// Reference: /workspaces/AxonRouter/src/app/api/proxy-pools/cloudflare-deploy/route.ts
func (h *ProxyDeployHandler) DeployCloudflare(c *gin.Context) {
	var req struct {
		CFToken     string `json:"cfToken"`
		AccountID   string `json:"accountId"`
		ProjectName string `json:"projectName"`
	}
	if c.ShouldBindJSON(&req) != nil || req.CFToken == "" || req.AccountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cloudflare API token and accountId are required"})
		return
	}
	projectName := req.ProjectName
	if projectName == "" {
		projectName = fmt.Sprintf("axonrelay-%s", time.Now().Format("060102150405"))
	}

	relayAuth := proxypool.GenerateRelayAuth()
	source := buildRelayEdgeFunctionSource("cloudflare")

	// Inline the relay auth (TS: RELAY_WORKER_CODE.replace("env.RELAY_AUTH", `"${relayAuth}"`))
	source = strings.ReplaceAll(source, `env.RELAY_AUTH`, fmt.Sprintf(`"%s"`, relayAuth))

	// Deploy worker script (TS: PUT /accounts/{id}/workers/scripts/{name})
	deployURL := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/workers/scripts/%s", req.AccountID, projectName)
	deployReq, _ := http.NewRequest("PUT", deployURL, strings.NewReader(source))
	deployReq.Header.Set("Authorization", "Bearer "+req.CFToken)
	deployReq.Header.Set("Content-Type", "application/javascript")
	resp, err := http.DefaultClient.Do(deployReq)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Cloudflare API error: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		c.JSON(resp.StatusCode, gin.H{"error": parseCFError(respBody)})
		return
	}

	// Enable workers.dev subdomain (TS: POST /workers/scripts/{name}/subdomain)
	subURL := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/workers/scripts/%s/subdomain", req.AccountID, projectName)
	subBody, _ := json.Marshal(map[string]bool{"enabled": true})
	subReq, _ := http.NewRequest("POST", subURL, bytes.NewReader(subBody))
	subReq.Header.Set("Authorization", "Bearer "+req.CFToken)
	subReq.Header.Set("Content-Type", "application/json")
	http.DefaultClient.Do(subReq) // best-effort

	// Get workers.dev URL (TS: GET /workers/scripts/{name}/settings)
	settingsURL := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/workers/scripts/%s/settings", req.AccountID, projectName)
	settingsReq, _ := http.NewRequest("GET", settingsURL, nil)
	settingsReq.Header.Set("Authorization", "Bearer "+req.CFToken)
	settingsResp, err := http.DefaultClient.Do(settingsReq)
	var workersDevSubdomain string
	if err == nil {
		defer settingsResp.Body.Close()
		settingsBody, _ := io.ReadAll(settingsResp.Body)
		var settings struct {
			Result struct {
				WorkersDevSubdomain string `json:"workers_dev_subdomain"`
			} `json:"result"`
		}
		json.Unmarshal(settingsBody, &settings)
		workersDevSubdomain = settings.Result.WorkersDevSubdomain
	}
	if workersDevSubdomain == "" {
		workersDevSubdomain = fmt.Sprintf("%s.%s.workers.dev", projectName, req.AccountID[:8])
	}
	cfDeployURL := fmt.Sprintf("https://%s", workersDevSubdomain)

	testRes := testRelayURL(cfDeployURL, relayAuth)
	testedAt := time.Now().Format(time.RFC3339)

	poolID := saveDeployPool(h.db, projectName, "cloudflare", cfDeployURL, relayAuth, testRes.OK, testedAt, testRes.Error)
	c.JSON(http.StatusCreated, gin.H{
		"proxyPoolId": poolID,
		"deployUrl":   cfDeployURL,
		"relayAuth":   relayAuth,
		"relayTest":   testRes,
	})
}

// GenerateSource returns the edge function source code for a relay type.
func (h *ProxyDeployHandler) GenerateSource(c *gin.Context) {
	relayType := c.Query("type")
	if relayType == "" {
		relayType = "vercel"
	}
	source := buildRelayEdgeFunctionSource(relayType)
	c.JSON(http.StatusOK, gin.H{"type": relayType, "source": source})
}

// --- Helpers ---

func buildRelayEdgeFunctionSource(relayType string) string {
	switch relayType {
	case "deno":
		return `Deno.serve(async (req) => {
  const auth = req.headers.get("x-relay-auth");
  const expected = Deno.env.get("RELAY_AUTH");
  if (expected && auth !== expected) {
    return new Response(JSON.stringify({ error: "Unauthorized" }), { status: 401, headers: { "content-type": "application/json" } });
  }
  const target = req.headers.get("x-relay-target");
  const relayPath = req.headers.get("x-relay-path") || "/";
  if (!target) {
    return new Response(JSON.stringify({ error: "Missing x-relay-target header" }), { status: 400, headers: { "content-type": "application/json" } });
  }
  const targetUrl = new URL(target.replace(/\/$/, "") + relayPath);
  if (["127.0.0.1","localhost","::1","0.0.0.0"].includes(targetUrl.hostname) || targetUrl.hostname.startsWith("10.") || targetUrl.hostname.startsWith("192.168.") || targetUrl.hostname.endsWith(".local")) {
    return new Response(JSON.stringify({ error: "Blocked private target" }), { status: 403, headers: { "content-type": "application/json" } });
  }
  const headers = new Headers(req.headers);
  headers.delete("x-relay-target"); headers.delete("x-relay-path"); headers.delete("x-relay-auth"); headers.delete("host");
  const response = await fetch(targetUrl.href, { method: req.method, headers, body: req.method !== "GET" && req.method !== "HEAD" ? req.body : undefined, duplex: "half" });
  return new Response(response.body, { status: response.status, headers: response.headers });
});`

	case "cloudflare":
		return `export default {
  async fetch(request, env, ctx) {
    const auth = request.headers.get("x-relay-auth");
    const expected = env.RELAY_AUTH;
    if (expected && auth !== expected) {
      return new Response(JSON.stringify({ error: "Unauthorized" }), { status: 401, headers: { "content-type": "application/json" } });
    }
    const target = request.headers.get("x-relay-target");
    const relayPath = request.headers.get("x-relay-path") || "/";
    if (!target) {
      return new Response(JSON.stringify({ error: "Missing x-relay-target header" }), { status: 400, headers: { "content-type": "application/json" } });
    }
    const targetUrl = new URL(target.replace(/\/$/, "") + relayPath);
    if (["127.0.0.1","localhost","::1","0.0.0.0"].includes(targetUrl.hostname) || targetUrl.hostname.startsWith("10.") || targetUrl.hostname.startsWith("192.168.") || targetUrl.hostname.endsWith(".local")) {
      return new Response(JSON.stringify({ error: "Blocked private target" }), { status: 403, headers: { "content-type": "application/json" } });
    }
    const headers = new Headers(request.headers);
    headers.delete("x-relay-target"); headers.delete("x-relay-path"); headers.delete("x-relay-auth"); headers.delete("host");
    const response = await fetch(targetUrl.href, { method: request.method, headers, body: request.method !== "GET" && request.method !== "HEAD" ? request.body : undefined, duplex: "half" });
    return new Response(response.body, { status: response.status, headers: response.headers });
  }
};`

	default: // vercel
		return `export const config = { runtime: "edge" };

export default async function handler(req) {
  const auth = req.headers.get("x-relay-auth");
  const expected = process.env.RELAY_AUTH;
  if (expected && auth !== expected) {
    return new Response(JSON.stringify({ error: "Unauthorized" }), { status: 401, headers: { "content-type": "application/json" } });
  }
  const target = req.headers.get("x-relay-target");
  const relayPath = req.headers.get("x-relay-path") || "/";
  if (!target) {
    return new Response(JSON.stringify({ error: "Missing x-relay-target header" }), { status: 400, headers: { "content-type": "application/json" } });
  }
  const targetUrl = new URL(target.replace(/\/$/, "") + relayPath);
  if (["127.0.0.1","localhost","::1","0.0.0.0"].includes(targetUrl.hostname) || targetUrl.hostname.startsWith("10.") || targetUrl.hostname.startsWith("192.168.") || targetUrl.hostname.endsWith(".local")) {
    return new Response(JSON.stringify({ error: "Blocked private target" }), { status: 403, headers: { "content-type": "application/json" } });
  }
  const headers = new Headers(req.headers);
  headers.delete("x-relay-target"); headers.delete("x-relay-path"); headers.delete("x-relay-auth"); headers.delete("host");
  const response = await fetch(targetUrl.href, { method: req.method, headers, body: req.method !== "GET" && req.method !== "HEAD" ? req.body : undefined, duplex: "half" });
  return new Response(response.body, { status: response.status, headers: response.headers });
}`
	}
}

func pollVercelDeployment(deploymentID, token string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest("GET", fmt.Sprintf("https://api.vercel.com/v13/deployments/%s", deploymentID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var result struct {
			ReadyState string `json:"readyState"`
			URL        string `json:"url"`
		}
		json.Unmarshal(body, &result)
		if result.ReadyState == "READY" {
			return result.URL, nil
		}
		if result.ReadyState == "ERROR" || result.ReadyState == "CANCELED" {
			return "", fmt.Errorf("deployment %s", result.ReadyState)
		}
		time.Sleep(3 * time.Second)
	}
	return "", fmt.Errorf("deployment timed out")
}

func pollDenoRevision(revisionID, token string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest("GET", fmt.Sprintf("https://api.deno.com/v2/revisions/%s", revisionID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var result struct {
			Status string `json:"status"`
		}
		json.Unmarshal(body, &result)
		if result.Status == "deployed" || result.Status == "failed" {
			return
		}
		time.Sleep(2 * time.Second)
	}
}

type deployTestResult struct {
	OK         bool   `json:"ok"`
	StatusCode int    `json:"status"`
	Error      string `json:"error,omitempty"`
	ElapsedMs  int64  `json:"elapsedMs"`
}

func testRelayURL(relayURL, relayAuth string) deployTestResult {
	start := time.Now()
	req, err := http.NewRequest("GET", relayURL, nil)
	if err != nil {
		return deployTestResult{Error: err.Error(), ElapsedMs: time.Since(start).Milliseconds()}
	}
	if relayAuth != "" {
		req.Header.Set("x-relay-auth", relayAuth)
	}
	req.Header.Set("x-relay-target", "https://api.openai.com")
	req.Header.Set("x-relay-path", "/v1/models")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return deployTestResult{Error: err.Error(), ElapsedMs: time.Since(start).Milliseconds()}
	}
	defer resp.Body.Close()
	return deployTestResult{OK: resp.StatusCode < 400, StatusCode: resp.StatusCode, ElapsedMs: time.Since(start).Milliseconds()}
}

func saveDeployPool(db *sql.DB, name, poolType, deployURL, relayAuth string, active bool, testedAt, lastErr string) string {
	now := time.Now().Unix()
	id := fmt.Sprintf("%x", []byte(proxypool.GenerateRelayAuth()[:16]))
	testStatus := "active"
	var errVal interface{}
	if !active {
		testStatus = "error"
		errVal = lastErr
	}
	var isActive int
	if active {
		isActive = 1
	}
	db.Exec(`INSERT INTO proxy_pools (id, name, type, proxy_url, relay_auth, is_active, test_status, last_tested_at, last_error, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, name, poolType, deployURL, relayAuth, isActive, testStatus, testedAt, errVal, now, now)
	return id
}

func parseVercelError(body []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	return "Failed to create Vercel deployment"
}

func parseDenoError(body []byte) string {
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &e) == nil && e.Message != "" {
		return e.Message
	}
	return "Deno Deploy error"
}

func parseCFError(body []byte) string {
	var e struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &e) == nil && len(e.Errors) > 0 && e.Errors[0].Message != "" {
		return e.Errors[0].Message
	}
	return "Cloudflare deploy error"
}
