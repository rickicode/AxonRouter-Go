package executor

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	kiroDefaultRegion = "us-east-1"
	kiroDevEndpoint   = "https://runtime.us-east-1.kiro.dev/generateAssistantResponse"
	kiroQuSEndpoint   = "https://q.us-east-1.amazonaws.com/generateAssistantResponse"

	kiroDefaultProfileARNBuilderID = "arn:aws:codewhisperer:us-east-1:638616132270:profile/AAAACCCCXXXX"
	kiroDefaultProfileARNSocial    = "arn:aws:codewhisperer:us-east-1:699475941385:profile/EHGA3GRVQMUK"
)

var (
	// AWS region shape: two lowercase letters, hyphen, location, hyphen, digit(s).
	// Guards against SSRF via region injection into upstream URLs.
	awsRegionPattern     = regexp.MustCompile(`^[a-z]{2}-[a-z]+-\d{1,2}$`)
	kiroProfileARNRe     = regexp.MustCompile(`^arn:aws:codewhisperer:([a-z0-9-]+):`)
	kiroProfileRegions   = map[string]struct{}{
		"us-east-1":    {},
		"eu-central-1": {},
	}
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
	matches := kiroProfileARNRe.FindStringSubmatch(profileArn)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

// resolveKiroRuntimeRegion returns the AWS region to use for CodeWhisperer / Amazon Q
// runtime calls. Resolution priority:
//
//  1. The region embedded in profileArn, when it is a syntactically valid AWS region.
//     The profile ARN's region is authoritative because the Q Developer profile (and
//     therefore runtime) lives where AWS created it, not where the IdC/OIDC token was minted.
//  2. A stored region only when it is one of the known Q Developer profile regions
//     (us-east-1 or eu-central-1). Other stored regions are IdC/OIDC token regions and are
//     deliberately ignored for runtime routing.
//  3. us-east-1 as the final fallback.
func resolveKiroRuntimeRegion(psd map[string]string) string {
	fromArn := regionFromKiroProfileArn(psd["profileArn"])
	if fromArn != "" && awsRegionPattern.MatchString(fromArn) {
		return fromArn
	}

	stored := normalizeRegion(psd["region"])
	if stored != "" {
		if _, ok := kiroProfileRegions[stored]; ok {
			return stored
		}
	}

	return kiroDefaultRegion
}

// kiroRuntimeHost returns the regional runtime host for CodeWhisperer / Amazon Q.
// us-east-1 keeps the legacy codewhisperer host; all other regions use q.{region}.amazonaws.com.
func kiroRuntimeHost(region string) string {
	if region == "us-east-1" {
		return "https://codewhisperer.us-east-1.amazonaws.com"
	}
	return fmt.Sprintf("https://q.%s.amazonaws.com", region)
}

// kiroEndpointURLs returns the ordered list of upstream URLs to try for a Kiro request.
//
// If baseURL is non-empty, only that URL is returned (operator override).
// Otherwise the regional AWS endpoint and the Kiro IDE gateway endpoint are returned in an
// order that depends on the persisted auth method:
//   - api_key, external_idp, and idc tokens only work on the Amazon CodeWhisperer surface,
//     so the AWS endpoint is tried first.
//   - builder-id, social (github), and import tokens are Kiro OIDC/social tokens that the
//     kiro.dev gateway accepts, so that endpoint is tried first.
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
		kiroDevEndpoint,
		"https://codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse",
		kiroQuSEndpoint:
		return true
	}
	return false
}

func kiroEndpointURLs(psd map[string]string, baseURL string) []string {
	if baseURL != "" && !isDefaultKiroBaseURL(baseURL) {
		return []string{baseURL}
	}

	region := resolveKiroRuntimeRegion(psd)
	awsEndpoint := kiroRuntimeHost(region) + "/generateAssistantResponse"

	authMethod := normalizeRegion(psd["authMethod"])
	if _, isCodeWhisperer := kiroCodeWhispererAuthMethods[authMethod]; isCodeWhisperer {
		return dedupeStrings([]string{awsEndpoint, kiroDevEndpoint, kiroQuSEndpoint})
	}
	return dedupeStrings([]string{kiroDevEndpoint, awsEndpoint, kiroQuSEndpoint})
}
