package executor

import "testing"

func TestCodeBuddyHeaders(t *testing.T) {
	tests := []struct {
		provider    string
		wantHeaders map[string]string
	}{
		{
			provider: "codebuddy",
			wantHeaders: map[string]string{
				"User-Agent":         "CLI/2.63.2 CodeBuddy/2.63.2",
				"X-Product":          "SaaS",
				"X-IDE-Type":         "CLI",
				"X-IDE-Name":         "CLI",
				"x-requested-with":   "XMLHttpRequest",
				"x-codebuddy-request": "1",
			},
		},
		{
			provider:    "openai",
			wantHeaders: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			headers := map[string]string{}
			codebuddyHeaders(headers, tt.provider)
			if len(headers) != len(tt.wantHeaders) {
				t.Fatalf("got %d headers, want %d", len(headers), len(tt.wantHeaders))
			}
			for k, v := range tt.wantHeaders {
				if got := headers[k]; got != v {
					t.Errorf("headers[%q] = %q, want %q", k, got, v)
				}
			}
		})
	}
}
