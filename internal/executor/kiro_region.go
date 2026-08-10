package executor

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	kiroDefaultRegion = "us-east-1"

	kiroDevScheme    = "https"
	kiroDevHost      = "runtime.us-east-1.kiro.dev"
	kiroGeneratePath = "/generateAssistantResponse"

	// AWS CodeWhisperer / Kiro runtime host templates. Rather than deriving the
	// host from a raw base_url via regex, we build it explicitly from the
	// validated region.
	awsQHostTemplate        = "q.%s.amazonaws.com"
	awsCodeWhispererDevHost = "codewhisperer.us-east-1.amazonaws.com"

	kiroDefaultProfileARNBuilderID = "arn:aws:codewhisperer:us-east-1:638616132270:profile/AAAACCCCXXXX"
	kiroDefaultProfileARNSocial    = "arn:aws:codewhisperer:us-east-1:699475941385:profile/EHGA3GRVQMUK"

	// Common legacy base_url values seeded in older versions or referenced by
	// clients. They are treated as "not an override" so the executor still builds
	// the proper regional endpoint list.
	legacyKiroDevEndpoint   = "https://runtime.us-east-1.kiro.dev/generateAssistantResponse"
	legacyQuSEndpoint       = "https://q.us-east-1.amazonaws.com/generateAssistantResponse"
	legacyCodeWhispererRoot = "https://codewhisperer.us-east-1.amazonaws.com"
)

var (
	// kiroCodeWhispererAuthMethods only work on the Amazon CodeWhisperer AWS
	// surface, so that endpoint is tried first for these methods.
	kiroCodeWhispererAuthMethods = map[string]struct{}{
		"api_key":      {},
		"external_idp": {},
		"idc":          {},
	}
)

// normalizeRegion returns a trimmed, lowercased region string or "".
func normalizeRegion(region string) string {
	return strings.ToLower(strings.TrimSpace(region))
}

// regionFromKiroProfileArn extracts the region from a CodeWhisperer profile ARN
// of the form arn:aws:codewhisperer:{region}:...
func regionFromKiroProfileArn(profileArn string) string {
	if profileArn == "" {
		return ""
	}
	prefix := "arn:aws:codewhisperer:"
	if !strings.HasPrefix(profileArn, prefix) {
		return ""
	}
	rest := profileArn[len(prefix):]
	idx := strings.Index(rest, ":")
	if idx <= 0 {
		return ""
	}
	return rest[:idx]
}

// resolveKiroRuntimeRegion returns the AWS region to use for Kiro
// runtime calls. Resolution priority:
//
//  1. The region embedded in profileArn, when it is a syntactically valid AWS region.
//     The profile ARN's region is authoritative because the Q Developer profile (and
//     therefore runtime) lives where AWS created it, not where the IdC/OIDC token was minted.
//  2. A stored region when it is a syntactically valid AWS region. This covers
//     enterprise/IdC accounts whose Q Developer runtime is outside the historic
//     us-east-1 / eu-central-1 profile regions.
//  3. us-east-1 as the final fallback.
func resolveKiroRuntimeRegion(psd map[string]string) string {
	fromArn := regionFromKiroProfileArn(psd["profileArn"])
	if fromArn != "" && isValidAWSRegion(fromArn) {
		return fromArn
	}
	stored := normalizeRegion(psd["region"])
	if stored != "" && isValidAWSRegion(stored) {
		return stored
	}
	return kiroDefaultRegion
}

// isValidAWSRegion validates the AWS region shape:
// two lowercase letters, hyphen, location, hyphen, digit(s).
// This guards against SSRF via region injection into upstream URLs.
func isValidAWSRegion(region string) bool {
	if region == "" {
		return false
	}
	parts := strings.Split(region, "-")
	if len(parts) != 3 {
		return false
	}
	if len(parts[0]) != 2 {
		return false
	}
	for _, r := range parts[0] {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	if parts[1] == "" {
		return false
	}
	for _, r := range parts[1] {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	if parts[2] == "" {
		return false
	}
	for _, r := range parts[2] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// kiroRuntimeHost returns the regional runtime host for Kiro.
// us-east-1 keeps the legacy codewhisperer host; all other regions use q.{region}.amazonaws.com.
func kiroRuntimeHost(region string) string {
	region = normalizeRegion(region)
	if region == "us-east-1" {
		return "https://" + awsCodeWhispererDevHost
	}
	return fmt.Sprintf("https://"+awsQHostTemplate, region)
}

// kiroDevEndpoint returns the fully-qualified Kiro dev/gateway endpoint.
func kiroDevEndpoint() string {
	return (&url.URL{Scheme: kiroDevScheme, Host: kiroDevHost, Path: kiroGeneratePath}).String()
}

// kiroQuSEndpoint returns the legacy q.us-east-1 AWS fallback endpoint.
func kiroQuSEndpoint() string {
	return fmt.Sprintf("https://"+awsQHostTemplate+kiroGeneratePath, kiroDefaultRegion)
}

// resolveDefaultKiroProfileArn returns the shared default profileArn for
// builder-id/social auth. Account-bound methods (api_key/idc/external_idp)
// must never use this shared ARN because it belongs to another account.
func resolveDefaultKiroProfileArn(authMethod string) string {
	authMethod = normalizeRegion(authMethod)
	if authMethod == "google" || authMethod == "github" {
		return kiroDefaultProfileARNSocial
	}
	return kiroDefaultProfileARNBuilderID
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// isDefaultKiroBaseURL reports whether url is one of the built-in Kiro endpoints.
// A default base_url is treated as empty so the executor falls back to the
// regional + dev + q.us-east-1 endpoint list instead of forcing a single URL.
func isDefaultKiroBaseURL(url string) bool {
	switch url {
	case "",
		legacyKiroDevEndpoint,
		legacyCodeWhispererRoot + kiroGeneratePath,
		legacyQuSEndpoint:
		return true
	}
	return false
}

// kiroEndpointURLs returns the ordered list of upstream URLs to try for a Kiro request.
//
// If baseURL is non-empty and not a legacy default, only that URL is returned
// (operator override).
//
// Otherwise the regional AWS endpoint and the Kiro IDE gateway endpoint are
// returned in an order that depends on the persisted auth method:
//   - api_key, external_idp, and idc tokens only work on the Amazon CodeWhisperer surface,
//     so the AWS endpoint is tried first.
//   - builder-id, social (github/google), and import tokens are Kiro OIDC/social tokens that
//     the kiro.dev gateway accepts, so that endpoint is tried first.
func kiroEndpointURLs(psd map[string]string, baseURL string) []string {
	if baseURL != "" && !isDefaultKiroBaseURL(baseURL) {
		return []string{baseURL}
	}

	region := resolveKiroRuntimeRegion(psd)
	awsEndpoint := kiroRuntimeHost(region) + kiroGeneratePath
	devEndpoint := kiroDevEndpoint()
	quSEndpoint := kiroQuSEndpoint()
	authMethod := normalizeRegion(psd["authMethod"])
	if _, isCodeWhisperer := kiroCodeWhispererAuthMethods[authMethod]; isCodeWhisperer {
		return dedupeStrings([]string{awsEndpoint, devEndpoint, quSEndpoint})
	}
	return dedupeStrings([]string{devEndpoint, awsEndpoint, quSEndpoint})
}
