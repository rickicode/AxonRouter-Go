package executor

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// iflowHeaders adds the iFlow request-signing headers. It is a no-op for every
// provider except "iflow". iFlow requires:
//
//   - session-id: "session-<uuid>"
//   - x-iflow-timestamp: unix milliseconds
//   - x-iflow-signature: hex HMAC-SHA256 of "<userAgent>:<sessionID>:<timestamp>"
//     keyed by the provider-scoped apiKey
//   - Authorization: "Bearer <apiKey>" (only when an apiKey is present)
//   - User-Agent: "iFlow-Cli"
//
// The apiKey is stored in provider_specific_data["api_key"] by the iFlow OAuth
// flow (see internal/auth/iflow). The signature is skipped when no apiKey is
// available, matching 9router's createIFlowSignature returning "".
func iflowHeaders(headers map[string]string, provider string, psd map[string]string) {
	if provider != "iflow" {
		return
	}

	apiKey := ""
	if psd != nil {
		apiKey = psd["api_key"]
	}

	sessionID := "session-" + randomUUID()
	timestamp := time.Now().UnixMilli()
	signature := createIFlowSignature("iFlow-Cli", sessionID, timestamp, apiKey)

	headers["session-id"] = sessionID
	headers["x-iflow-timestamp"] = strconv.FormatInt(timestamp, 10)
	headers["x-iflow-signature"] = signature
	headers["User-Agent"] = "iFlow-Cli"
	if apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	}
}

// iflowTransformRequest injects stream_options.include_usage into streaming
// iFlow requests so usage data is returned, matching 9router's
// IFlowExecutor.transformRequest. No-op for non-iflow providers and non-stream
// requests; fail-open on parse errors.
func iflowTransformRequest(body []byte, provider string, stream bool) []byte {
	if provider != "iflow" || !stream {
		return body
	}
	if len(body) == 0 {
		return body
	}
	if gjson.GetBytes(body, "stream_options").Exists() {
		return body
	}
	out, err := sjson.SetBytes(body, "stream_options", map[string]any{"include_usage": true})
	if err != nil {
		return body
	}
	return out
}

// createIFlowSignature computes the hex HMAC-SHA256 signature over
// "<userAgent>:<sessionID>:<timestamp>" keyed by apiKey. An empty apiKey
// produces an empty signature (9router returns "" for missing apiKey).
func createIFlowSignature(userAgent, sessionID string, timestamp int64, apiKey string) string {
	if apiKey == "" {
		return ""
	}
	payload := fmt.Sprintf("%s:%s:%d", userAgent, sessionID, timestamp)
	mac := hmac.New(sha256.New, []byte(apiKey))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// randomUUID returns a random RFC 4122 version 4 UUID string.
func randomUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
