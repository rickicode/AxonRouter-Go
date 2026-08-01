package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestWebSearchHandler(t *testing.T) {
	tests := []struct {
		name        string
		args        json.RawMessage
		wantError   bool
		description string
	}{
		{
			name:        "valid search",
			args:        []byte(`{"query": "golang", "max_results": 5}`),
			wantError:   false,
			description: "should handle valid search request",
		},
		{
			name:        "missing query",
			args:        []byte(`{"max_results": 5}`),
			wantError:   true,
			description: "should require query parameter",
		},
		{
			name:        "empty query",
			args:        []byte(`{"query": ""}`),
			wantError:   true,
			description: "should require non-empty query",
		},
		{
			name:        "invalid json",
			args:        []byte(`{"query": invalid}`),
			wantError:   true,
			description: "should handle invalid JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := WebSearchHandler(context.Background(), tt.args)
			
			if tt.wantError && err == nil {
				t.Errorf("%s: expected error but got nil", tt.description)
			}

			if !tt.wantError && err != nil {
				t.Errorf("%s: unexpected error: %v", tt.description, err)
			}

			if !tt.wantError && result == nil {
				t.Errorf("%s: expected non-nil result", tt.description)
			}

			if tt.wantError {
				if result == nil {
					t.Errorf("%s: expected error result", tt.description)
				} else if !result.IsError {
					t.Errorf("%s: result should have IsError=true for errors", tt.description)
				}
			}
		})
	}
}

func TestWebSearchToolDefinition(t *testing.T) {
	if WebSearchTool.Name != "axonrouter_web_search" {
		t.Errorf("Tool name should be 'axonrouter_web_search', got '%s'", WebSearchTool.Name)
	}

	expectedDescription := "Search the web using AxonRouter's search capabilities"
	if WebSearchTool.Description != expectedDescription {
		t.Errorf("Tool description mismatch: got '%s', expected '%s'", 
			WebSearchTool.Description, expectedDescription)
	}

	var schema map[string]interface{}
	if err := json.Unmarshal(WebSearchTool.InputSchema, &schema); err != nil {
		t.Fatalf("Failed to unmarshal input schema: %v", err)
	}

	if schema["type"] != "object" {
		t.Errorf("Schema type should be 'object', got '%v'", schema["type"])
	}

	properties := schema["properties"].(map[string]interface{})
	if _, ok := properties["query"]; !ok {
		t.Error("Schema should have 'query' property")
	}

	if _, ok := properties["max_results"]; !ok {
		t.Error("Schema should have 'max_results' property")
	}

	required := schema["required"].([]interface{})
	if len(required) != 1 || required[0] != "query" {
		t.Errorf("Schema required array should have ['query'], got %v", required)
	}
}
