package main

import (
	"context"
	"encoding/json"
	"fmt"

	_ "github.com/rickicode/AxonRouter-Go/internal/translator"
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
)

func main() {
	body := map[string]interface{}{
		"model": "gemini-2.5-pro",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "hello world"},
		},
		"temperature": 0.5,
		"max_tokens": 100,
		"reasoning_effort": "high",
	}
	raw, _ := json.Marshal(body)
	result := registry.Request("openai", "antigravity", "gemini-2.5-pro", raw, false)

	// Test Response translation
	agResponse := `{
		"response": {
			"responseId": "ag-response-id-123",
			"modelVersion": "gemini-2.5-pro-version",
			"createTime": "2026-06-29T21:00:00Z",
			"candidates": [
				{
					"index": 0,
					"finishReason": "STOP",
					"content": {
						"parts": [
							{"text": "thinking...", "thought": true},
							{"text": "hello back!"}
						]
					}
				}
			],
			"usageMetadata": {
				"promptTokenCount": 10,
				"candidatesTokenCount": 15,
				"totalTokenCount": 25,
				"thoughtsTokenCount": 5
			}
		}
	}`

	var param any
	openaiResponseRaw := registry.ResponseNonStream(context.Background(), "openai", "antigravity", "gemini-2.5-pro", raw, result, []byte(agResponse), &param)
	
	var res map[string]interface{}
	json.Unmarshal(openaiResponseRaw, &res)
	fmt.Printf("res.id = %v\n", res["id"])
	fmt.Printf("res.model = %v\n", res["model"])
	
	choices := res["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	message := choice["message"].(map[string]interface{})
	fmt.Printf("content = %v\n", message["content"])
	fmt.Printf("reasoning_content = %v\n", message["reasoning_content"])
	
	usage := res["usage"].(map[string]interface{})
	fmt.Printf("prompt_tokens = %v\n", usage["prompt_tokens"])
	fmt.Printf("completion_tokens = %v\n", usage["completion_tokens"])
}
