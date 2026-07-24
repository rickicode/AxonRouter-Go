package executor

import (
	"fmt"
	"regexp"
	"strings"

	xxHash64 "github.com/pierrec/xxHash/xxHash64"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// claudeCCHSeed is the xxHash seed used by CLIProxyAPI for Anthropic CCH signing.
const claudeCCHSeed uint64 = 0x6E52736AC806831E

// claudeBillingHeaderCCHPattern matches the cch= value in the billing header.
var claudeBillingHeaderCCHPattern = regexp.MustCompile(`\bcch=([0-9a-f]{5});`)

// isClaudeOAuthToken returns true for Anthropic OAuth access tokens.
func isClaudeOAuthToken(apiKey string) bool {
	return strings.Contains(apiKey, "sk-ant-oat")
}

// signAnthropicMessagesBody signs a Claude messages request body by replacing the
// placeholder cch= value in the billing header with a deterministic xxHash of the
// unsigned body. This matches CLIProxyAPI's implementation for OAuth traffic.
func signAnthropicMessagesBody(body []byte) []byte {
	billingHeader := gjson.GetBytes(body, "system.0.text").String()
	if !strings.HasPrefix(billingHeader, "x-anthropic-billing-header:") {
		return body
	}
	if !claudeBillingHeaderCCHPattern.MatchString(billingHeader) {
		return body
	}

	unsignedBillingHeader := claudeBillingHeaderCCHPattern.ReplaceAllString(billingHeader, "cch=00000;")
	unsignedBody, err := sjson.SetBytes(body, "system.0.text", unsignedBillingHeader)
	if err != nil {
		return body
	}

	cch := fmt.Sprintf("%05x", xxHash64.Checksum(unsignedBody, claudeCCHSeed)&0xFFFFF)
	signedBillingHeader := claudeBillingHeaderCCHPattern.ReplaceAllString(unsignedBillingHeader, "cch="+cch+";")
	signedBody, err := sjson.SetBytes(unsignedBody, "system.0.text", signedBillingHeader)
	if err != nil {
		return unsignedBody
	}
	return signedBody
}
