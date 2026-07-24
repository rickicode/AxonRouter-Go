package executor

import (
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestExtractAndRemoveBetas(t *testing.T) {
	cases := []struct {
		name          string
		input         string
		wantBetas     []string
		wantBodyField string
		wantBodyValue string
	}{
		{
			name:          "extracts array of betas",
			input:         `{"model":"claude-opus-4","betas":["beta-a","beta-b"],"messages":[]}`,
			wantBetas:     []string{"beta-a", "beta-b"},
			wantBodyField: "model",
			wantBodyValue: "claude-opus-4",
		},
		{
			name:          "extracts single string beta",
			input:         `{"betas":"single-beta","messages":[]}`,
			wantBetas:     []string{"single-beta"},
			wantBodyField: "messages",
		},
		{
			name:          "returns nil when betas missing",
			input:         `{"model":"claude-opus-4"}`,
			wantBetas:     nil,
			wantBodyField: "model",
			wantBodyValue: "claude-opus-4",
		},
		{
			name:          "ignores empty array",
			input:         `{"betas":[],"model":"claude-opus-4"}`,
			wantBetas:     nil,
			wantBodyField: "model",
			wantBodyValue: "claude-opus-4",
		},
		{
			name:          "trims whitespace and skips empty entries",
			input:         `{"betas":[" beta-a ","","beta-b"],"model":"x"}`,
			wantBetas:     []string{"beta-a", "beta-b"},
			wantBodyField: "model",
			wantBodyValue: "x",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			betas, body := extractAndRemoveBetas([]byte(tc.input))

			if len(betas) != len(tc.wantBetas) {
				t.Errorf("betas = %v, want %v", betas, tc.wantBetas)
			} else {
				for i := range betas {
					if betas[i] != tc.wantBetas[i] {
						t.Errorf("betas[%d] = %q, want %q", i, betas[i], tc.wantBetas[i])
					}
				}
			}

			if got := gjson.GetBytes(body, tc.wantBodyField).Exists(); tc.wantBodyField != "" && !got {
				t.Errorf("body field %q missing", tc.wantBodyField)
			}
			if tc.wantBodyValue != "" {
				if got := gjson.GetBytes(body, tc.wantBodyField).String(); got != tc.wantBodyValue {
					t.Errorf("body %q = %q, want %q", tc.wantBodyField, got, tc.wantBodyValue)
				}
			}
			if gjson.GetBytes(body, "betas").Exists() && len(betas) > 0 {
				t.Errorf("betas should be removed from body when non-empty")
			}
		})
	}
}

func TestClaudeBetaHeader(t *testing.T) {
	cases := []struct {
		name       string
		bodyBetas  []string
		reqHeaders map[string]string
		want       []string
	}{
		{
			name:       "base betas always included",
			bodyBetas:  nil,
			reqHeaders: nil,
			want:       baseClaudeBetas,
		},
		{
			name:       "merges body betas with base betas",
			bodyBetas:  []string{"client-beta-1", "client-beta-2"},
			reqHeaders: nil,
			want:       append(baseClaudeBetas, "client-beta-1", "client-beta-2"),
		},
		{
			name:       "merges header betas with base betas",
			bodyBetas:  nil,
			reqHeaders: map[string]string{"Anthropic-Beta": "header-beta-1,header-beta-2"},
			want:       append(baseClaudeBetas, "header-beta-1", "header-beta-2"),
		},
		{
			name:       "deduplicates across base, header and body",
			bodyBetas:  []string{"claude-code-20250219", "body-beta"},
			reqHeaders: map[string]string{"anthropic-beta": "oauth-2025-04-20,body-beta"},
			want:       append(baseClaudeBetas, "body-beta"),
		},
		{
			name:       "trims whitespace in header betas",
			bodyBetas:  nil,
			reqHeaders: map[string]string{"Anthropic-Beta": " beta-x , beta-y "},
			want:       append(baseClaudeBetas, "beta-x", "beta-y"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := claudeBetaHeader(tc.bodyBetas, tc.reqHeaders)
			if got == "" {
				t.Fatalf("claudeBetaHeader = empty, want non-empty")
			}
			gotParts := strings.Split(got, ",")
			if len(gotParts) != len(tc.want) {
				t.Fatalf("header = %q (%d parts), want %d parts", got, len(gotParts), len(tc.want))
			}
			for i := range gotParts {
				if gotParts[i] != tc.want[i] {
					t.Errorf("header[%d] = %q, want %q", i, gotParts[i], tc.want[i])
				}
			}
		})
	}
}

func TestPrepareClaudeBody_BetasExtraction(t *testing.T) {
	input := []byte(`{"model":"claude-opus-4","betas":["client-beta"],"messages":[],"temperature":0.5}`)

	out, betas := prepareClaudeBody(input)

	if got := gjson.GetBytes(out, "betas").Exists(); got {
		t.Errorf("betas should be removed from body")
	}
	if len(betas) != 1 || betas[0] != "client-beta" {
		t.Errorf("betas = %v, want [client-beta]", betas)
	}
}
