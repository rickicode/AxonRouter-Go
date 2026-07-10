package executor

import "testing"

func TestOpenAIEndpointNormalizesBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		endpoint string
		want     string
	}{
		{
			name:     "empty base uses OpenAI default",
			endpoint: "chat/completions",
			want:     "https://api.openai.com/v1/chat/completions",
		},
		{
			name:     "root OpenAI compatible base appends chat completions",
			baseURL:  "https://opencode.ai/zen/v1",
			endpoint: "chat/completions",
			want:     "https://opencode.ai/zen/v1/chat/completions",
		},
		{
			name:     "trailing slash root base appends models",
			baseURL:  "https://opencode.ai/zen/v1/",
			endpoint: "models",
			want:     "https://opencode.ai/zen/v1/models",
		},
		{
			name:     "full chat completions endpoint stays unchanged",
			baseURL:  "https://example.com/v1/chat/completions",
			endpoint: "chat/completions",
			want:     "https://example.com/v1/chat/completions",
		},
		{
			name:     "full responses endpoint stays unchanged",
			baseURL:  "https://example.com/v1/responses",
			endpoint: "responses",
			want:     "https://example.com/v1/responses",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := openAIEndpoint(tt.baseURL, tt.endpoint, nil); got != tt.want {
				t.Fatalf("openAIEndpoint(%q, %q) = %q, want %q", tt.baseURL, tt.endpoint, got, tt.want)
			}
		})
	}
}
