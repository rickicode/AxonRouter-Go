// Package signature provides Claude thinking-signature helpers used by translators.
// This is a simplified port of CLIProxyAPI's signature package, focused on the
// validation/normalization paths required for Antigravity Claude traffic.
package signature

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const MaxClaudeThinkingSignatureLen = 32 * 1024 * 1024

// ClaudeSignatureValidationOptions controls how strictly a signature is checked.
type ClaudeSignatureValidationOptions struct {
	// PrefixOnly checks only for an optional cache prefix followed by the
	// Claude E/R signature prefix. Use for request cleanup / strip.
	PrefixOnly bool
	// AllowEmptySignatureWithEmptyText keeps empty placeholders when set.
	AllowEmptySignatureWithEmptyText bool
	// Strict enables full protobuf inspection. Not implemented in this port.
	Strict bool
}

// IsValidClaudeThinkingSignature reports whether rawSignature looks like a real
// Claude thinking signature under the requested options.
func IsValidClaudeThinkingSignature(rawSignature string, opts ...ClaudeSignatureValidationOptions) bool {
	opt := mergeOpts(opts)
	if opt.PrefixOnly {
		return HasClaudeThinkingSignaturePrefix(rawSignature)
	}
	_, err := NormalizeClaudeThinkingSignature(rawSignature, opt)
	return err == nil
}

// HasClaudeThinkingSignaturePrefix reports whether rawSignature has an optional
// cache prefix followed by a Claude E/R signature prefix.
func HasClaudeThinkingSignaturePrefix(rawSignature string) bool {
	sig := stripCachePrefix(rawSignature)
	return sig != "" && (sig[0] == 'E' || sig[0] == 'R')
}

// NormalizeClaudeThinkingSignature strips any cache prefix, validates the
// signature, and returns the double-layer R-form expected by Antigravity bypass
// mode. For an E-form signature it returns base64(E-form). For an R-form
// signature it returns the original R-form after validation.
func NormalizeClaudeThinkingSignature(rawSignature string, opts ...ClaudeSignatureValidationOptions) (string, error) {
	opt := mergeOpts(opts)
	sig := stripCachePrefix(rawSignature)
	if sig == "" {
		return "", fmt.Errorf("empty signature")
	}
	if len(sig) > MaxClaudeThinkingSignatureLen {
		return "", fmt.Errorf("signature exceeds maximum length")
	}

	switch sig[0] {
	case 'R':
		if err := validateDoubleLayer(sig, opt); err != nil {
			return "", err
		}
		return sig, nil
	case 'E':
		if err := validateSingleLayer(sig, opt); err != nil {
			return "", err
		}
		return base64.StdEncoding.EncodeToString([]byte(sig)), nil
	default:
		return "", fmt.Errorf("invalid signature prefix %q", string(sig[0]))
	}
}

// StripInvalidClaudeThinkingBlocks removes Claude "thinking" content blocks whose
// signatures are empty or not valid Claude thinking signatures.
func StripInvalidClaudeThinkingBlocks(payload []byte, opts ...ClaudeSignatureValidationOptions) []byte {
	opt := mergeOpts(opts)
	messages := gjson.GetBytes(payload, "messages")
	if !messages.IsArray() {
		return payload
	}

	messageResults := messages.Array()
	keptMessages := make([]string, 0, len(messageResults))
	modified := false
	for _, msg := range messageResults {
		content := msg.Get("content")
		if !content.IsArray() {
			keptMessages = append(keptMessages, msg.Raw)
			continue
		}

		parts := content.Array()
		keptParts := make([]string, 0, len(parts))
		stripped := false
		for _, part := range parts {
			if part.Get("type").String() == "thinking" && shouldStrip(part, opt) {
				stripped = true
				continue
			}
			keptParts = append(keptParts, part.Raw)
		}
		if stripped {
			modified = true
			updated, _ := sjson.SetRaw(msg.Raw, "content", "["+strings.Join(keptParts, ",")+"]")
			keptMessages = append(keptMessages, updated)
			continue
		}
		keptMessages = append(keptMessages, msg.Raw)
	}
	if !modified {
		return payload
	}
	out, _ := sjson.SetRawBytes(payload, "messages", []byte("["+strings.Join(keptMessages, ",")+"]"))
	return out
}

func mergeOpts(opts []ClaudeSignatureValidationOptions) ClaudeSignatureValidationOptions {
	if len(opts) == 0 {
		// Default validation checks that the optional cache prefix, E/R prefix,
		// and base64 layer(s) decode to a Claude 0x12 marker.
		return ClaudeSignatureValidationOptions{}
	}
	return opts[0]
}

func stripCachePrefix(raw string) string {
	sig := strings.TrimSpace(raw)
	if sig == "" {
		return ""
	}
	if idx := strings.IndexByte(sig, '#'); idx >= 0 {
		sig = strings.TrimSpace(sig[idx+1:])
	}
	return sig
}

func validateSingleLayer(sig string, opt ClaudeSignatureValidationOptions) error {
	if len(sig) <= 1 {
		return fmt.Errorf("single-layer signature too short")
	}
	decoded, err := base64.StdEncoding.DecodeString(sig[1:])
	if err != nil {
		return fmt.Errorf("single-layer signature base64 decode failed: %w", err)
	}
	if len(decoded) == 0 {
		return fmt.Errorf("single-layer signature empty after decode")
	}
	if decoded[0] != 0x12 {
		return fmt.Errorf("single-layer signature missing Claude marker 0x12")
	}
	if opt.Strict {
		// Full protobuf inspection is intentionally omitted in this port.
	}
	return nil
}

func validateDoubleLayer(sig string, opt ClaudeSignatureValidationOptions) error {
	if len(sig) <= 1 {
		return fmt.Errorf("double-layer signature too short")
	}
	decoded, err := base64.StdEncoding.DecodeString(sig[1:])
	if err != nil {
		return fmt.Errorf("double-layer signature base64 decode failed: %w", err)
	}
	if len(decoded) == 0 {
		return fmt.Errorf("double-layer signature empty after decode")
	}
	if decoded[0] != 'E' {
		return fmt.Errorf("double-layer signature inner does not start with E")
	}
	if err := validateSingleLayer(string(decoded), opt); err != nil {
		return fmt.Errorf("double-layer signature inner invalid: %w", err)
	}
	return nil
}

func shouldStrip(part gjson.Result, opt ClaudeSignatureValidationOptions) bool {
	if opt.AllowEmptySignatureWithEmptyText && isEmptyPlaceholder(part) {
		return false
	}
	return !IsValidClaudeThinkingSignature(part.Get("signature").String(), opt)
}

func isEmptyPlaceholder(part gjson.Result) bool {
	if strings.TrimSpace(part.Get("signature").String()) != "" {
		return false
	}
	text := part.Get("text").String()
	if text == "" {
		text = part.Get("thinking").String()
	}
	return strings.TrimSpace(text) == ""
}
