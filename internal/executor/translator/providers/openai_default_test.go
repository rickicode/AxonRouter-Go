package providers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/executor/translator"
)

func TestTranslateOpenAICompatible(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		raw        string
		wantType   string
		wantCode   string
		wantMsg    string
	}{
		{
			name:       "Bedrock validation_error context length",
			statusCode: 400,
			raw: `{"error":{"code":"validation_error","message":"ErrorEvent { error: APIError { type: \"BadRequestError\", code: Some(400), message: \"This model's maximum context length is 202752 tokens. However, you requested 32000 output tokens and your prompt contains at least 170753 input tokens, for a total of at least 202753 tokens. Please reduce the length of the input prompt or the number of requested output tokens. (parameter=input_tokens, value=170753)\", param: None } }","param":null,"type":"invalid_request_error"}}`,
			wantType: "invalid_request_error",
			wantCode: "context_length_exceeded",
			wantMsg:  "ErrorEvent { error: APIError { type: \"BadRequestError\", code: Some(400), message: \"This model's maximum context length is 202752 tokens. However, you requested 32000 output tokens and your prompt contains at least 170753 input tokens, for a total of at least 202753 tokens. Please reduce the length of the input prompt or the number of requested output tokens. (parameter=input_tokens, value=170753)\", param: None } }",
		},
		{
			name:       "OpenAI canonical context_length_exceeded preserved",
			statusCode: 400,
			raw:        `{"error":{"message":"This model's maximum context length is 8192 tokens","type":"invalid_request_error","param":"messages","code":"context_length_exceeded"}}`,
			wantType:   "invalid_request_error",
			wantCode:   "context_length_exceeded",
			wantMsg:    "This model's maximum context length is 8192 tokens",
		},
		{
			name:       "invalid_api_key preserved",
			statusCode: 401,
			raw:        `{"error":{"message":"Invalid API key","type":"authentication_error","code":"invalid_api_key"}}`,
			wantType:   "authentication_error",
			wantCode:   "invalid_api_key",
			wantMsg:    "Invalid API key",
		},
		{
			name:       "rate limit inferred from message with 400",
			statusCode: 400,
			raw:        `{"error":{"message":"rate limit exceeded for provider","type":"invalid_request_error","code":"validation_error"}}`,
			wantType:   "rate_limit_error",
			wantCode:   "rate_limit_exceeded",
			wantMsg:    "rate limit exceeded for provider",
		},
		{
			name:       "OpenCode Free input_tokens message",
			statusCode: 400,
			raw:        `{"error":{"message":"prompt contains too many input_tokens","type":"invalid_request_error","code":"bad_request"}}`,
			wantType:   "invalid_request_error",
			wantCode:   "context_length_exceeded",
			wantMsg:    "prompt contains too many input_tokens",
		},
		{
			name:       "server error with no body",
			statusCode: 500,
			raw:        `upstream timeout`,
			wantType:   "server_error",
			wantCode:   "internal_server_error",
			wantMsg:    "upstream timeout",
		},
		{
			name:       "model not found inferred",
			statusCode: 404,
			raw:        `{"error":{"message":"no such model: gpt-missing","type":"invalid_request_error","code":"not_found"}}`,
			wantType:   "invalid_request_error",
			wantCode:   "model_not_found",
			wantMsg:    "no such model: gpt-missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TranslateOpenAICompatible(tt.statusCode, []byte(tt.raw))
			var parsed translator.OpenAIError
			if err := json.Unmarshal(got, &parsed); err != nil {
				t.Fatalf("output is not valid JSON: %v\nbody: %s", err, string(got))
			}
			if parsed.Error.Type != tt.wantType {
				t.Errorf("type = %q, want %q", parsed.Error.Type, tt.wantType)
			}
			if parsed.Error.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", parsed.Error.Code, tt.wantCode)
			}
			if !strings.Contains(parsed.Error.Message, tt.wantMsg) {
				t.Errorf("message = %q, want containing %q", parsed.Error.Message, tt.wantMsg)
			}
		})
	}
}
