package openai

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// convertCliResponseToOpenAIChatParams holds parameters for response conversion.
type convertCliResponseToOpenAIChatParams struct {
	UnixTimestamp        int64
	FunctionIndex        int
	SawToolCall          bool   // Tracks if any tool call was seen in the entire stream
	UpstreamFinishReason string // Caches the upstream finish reason for final chunk
	SanitizedNameMap     map[string]string
}

// functionCallIDCounter provides a process-wide unique counter for function call identifiers.
var functionCallIDCounter uint64

// SanitizedToolNameMap builds a map of sanitized -> original tool names.
func SanitizedToolNameMap(rawJSON []byte) map[string]string {
	if len(rawJSON) == 0 || !gjson.ValidBytes(rawJSON) {
		return nil
	}
	tools := gjson.GetBytes(rawJSON, "tools")
	if !tools.Exists() || !tools.IsArray() {
		return nil
	}
	out := make(map[string]string)
	tools.ForEach(func(_, tool gjson.Result) bool {
		name := strings.TrimSpace(tool.Get("name").String())
		if name == "" {
			return true
		}
		// Use local SanitizeFunctionName
		sanitized := SanitizeFunctionName(name)
		if sanitized == name {
			return true
		}
		if _, exists := out[sanitized]; !exists {
			out[sanitized] = name
		}
		return true
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

// RestoreSanitizedToolName returns the original tool name.
func RestoreSanitizedToolName(toolNameMap map[string]string, sanitizedName string) string {
	if sanitizedName == "" || toolNameMap == nil {
		return sanitizedName
	}
	if original, ok := toolNameMap[sanitizedName]; ok {
		return original
	}
	return sanitizedName
}

// convertAntigravityResponseToOpenAIStream translates a single chunk of a streaming response.
func convertAntigravityResponseToOpenAIStream(_ context.Context, _ string, originalRequestRawJSON, _, rawJSON []byte, param *any) [][]byte {
	if *param == nil {
		*param = &convertCliResponseToOpenAIChatParams{
			UnixTimestamp:    0,
			FunctionIndex:    0,
			SanitizedNameMap: SanitizedToolNameMap(originalRequestRawJSON),
		}
	}
	p := (*param).(*convertCliResponseToOpenAIChatParams)
	if p.SanitizedNameMap == nil {
		p.SanitizedNameMap = SanitizedToolNameMap(originalRequestRawJSON)
	}

	if bytes.Equal(rawJSON, []byte("[DONE]")) {
		return [][]byte{}
	}

	template := []byte(`{"id":"","object":"chat.completion.chunk","created":12345,"model":"model","choices":[{"index":0,"delta":{"role":null,"content":null,"reasoning_content":null,"tool_calls":null},"finish_reason":null,"native_finish_reason":null}]}`)

	if modelVersionResult := gjson.GetBytes(rawJSON, "response.modelVersion"); modelVersionResult.Exists() {
		template, _ = sjson.SetBytes(template, "model", modelVersionResult.String())
	}

	if createTimeResult := gjson.GetBytes(rawJSON, "response.createTime"); createTimeResult.Exists() {
		t, err := time.Parse(time.RFC3339Nano, createTimeResult.String())
		if err == nil {
			p.UnixTimestamp = t.Unix()
		}
		template, _ = sjson.SetBytes(template, "created", p.UnixTimestamp)
	} else {
		template, _ = sjson.SetBytes(template, "created", p.UnixTimestamp)
	}

	if responseIDResult := gjson.GetBytes(rawJSON, "response.responseId"); responseIDResult.Exists() {
		template, _ = sjson.SetBytes(template, "id", responseIDResult.String())
	}

	if finishReasonResult := gjson.GetBytes(rawJSON, "response.candidates.0.finishReason"); finishReasonResult.Exists() {
		p.UpstreamFinishReason = strings.ToUpper(finishReasonResult.String())
	}

	if usageResult := gjson.GetBytes(rawJSON, "response.usageMetadata"); usageResult.Exists() {
		cachedTokenCount := usageResult.Get("cachedContentTokenCount").Int()
		if candidatesTokenCountResult := usageResult.Get("candidatesTokenCount"); candidatesTokenCountResult.Exists() {
			template, _ = sjson.SetBytes(template, "usage.completion_tokens", candidatesTokenCountResult.Int())
		}
		if totalTokenCountResult := usageResult.Get("totalTokenCount"); totalTokenCountResult.Exists() {
			template, _ = sjson.SetBytes(template, "usage.total_tokens", totalTokenCountResult.Int())
		}
		promptTokenCount := usageResult.Get("promptTokenCount").Int()
		thoughtsTokenCount := usageResult.Get("thoughtsTokenCount").Int()
		template, _ = sjson.SetBytes(template, "usage.prompt_tokens", promptTokenCount)
		if thoughtsTokenCount > 0 {
			template, _ = sjson.SetBytes(template, "usage.completion_tokens_details.reasoning_tokens", thoughtsTokenCount)
		}
		if cachedTokenCount > 0 {
			template, _ = sjson.SetBytes(template, "usage.prompt_tokens_details.cached_tokens", cachedTokenCount)
		}
	}

	partsResult := gjson.GetBytes(rawJSON, "response.candidates.0.content.parts")
	if partsResult.IsArray() {
		partResults := partsResult.Array()
		for i := 0; i < len(partResults); i++ {
			partResult := partResults[i]
			partTextResult := partResult.Get("text")
			functionCallResult := partResult.Get("functionCall")
			inlineDataResult := partResult.Get("inlineData")
			if !inlineDataResult.Exists() {
				inlineDataResult = partResult.Get("inline_data")
			}
			thoughtSignatureResult := partResult.Get("thoughtSignature")
			if !thoughtSignatureResult.Exists() {
				thoughtSignatureResult = partResult.Get("thought_signature")
			}

			hasThoughtSignature := thoughtSignatureResult.Exists() && thoughtSignatureResult.String() != ""
			hasContentPayload := partTextResult.Exists() || functionCallResult.Exists() || inlineDataResult.Exists()

			if hasThoughtSignature && !hasContentPayload {
				continue
			}

			if partTextResult.Exists() {
				textContent := partTextResult.String()
				if partResult.Get("thought").Bool() {
					template, _ = sjson.SetBytes(template, "choices.0.delta.reasoning_content", textContent)
				} else {
					template, _ = sjson.SetBytes(template, "choices.0.delta.content", textContent)
				}
				template, _ = sjson.SetBytes(template, "choices.0.delta.role", "assistant")
			} else if functionCallResult.Exists() {
				p.SawToolCall = true
				toolCallsResult := gjson.GetBytes(template, "choices.0.delta.tool_calls")
				functionCallIndex := p.FunctionIndex
				p.FunctionIndex++
				if toolCallsResult.Exists() && toolCallsResult.IsArray() {
					functionCallIndex = len(toolCallsResult.Array())
				} else {
					template, _ = sjson.SetRawBytes(template, "choices.0.delta.tool_calls", []byte(`[]`))
				}

				functionCallTemplate := []byte(`{"id": "","index": 0,"type": "function","function": {"name": "","arguments": ""}}`)
				fcName := RestoreSanitizedToolName(p.SanitizedNameMap, functionCallResult.Get("name").String())
				functionCallTemplate, _ = sjson.SetBytes(functionCallTemplate, "id", fmt.Sprintf("%s-%d-%d", fcName, time.Now().UnixNano(), atomic.AddUint64(&functionCallIDCounter, 1)))
				functionCallTemplate, _ = sjson.SetBytes(functionCallTemplate, "index", functionCallIndex)
				functionCallTemplate, _ = sjson.SetBytes(functionCallTemplate, "function.name", fcName)
				if fcArgsResult := functionCallResult.Get("args"); fcArgsResult.Exists() {
					functionCallTemplate, _ = sjson.SetBytes(functionCallTemplate, "function.arguments", fcArgsResult.Raw)
				}
				template, _ = sjson.SetBytes(template, "choices.0.delta.role", "assistant")
				template, _ = sjson.SetRawBytes(template, "choices.0.delta.tool_calls.-1", functionCallTemplate)
			} else if inlineDataResult.Exists() {
				data := inlineDataResult.Get("data").String()
				if data == "" {
					continue
				}
				mimeType := inlineDataResult.Get("mimeType").String()
				if mimeType == "" {
					mimeType = inlineDataResult.Get("mime_type").String()
				}
				if mimeType == "" {
					mimeType = "image/png"
				}
				imageURL := fmt.Sprintf("data:%s;base64,%s", mimeType, data)
				imagesResult := gjson.GetBytes(template, "choices.0.delta.images")
				if !imagesResult.Exists() || !imagesResult.IsArray() {
					template, _ = sjson.SetRawBytes(template, "choices.0.delta.images", []byte(`[]`))
				}
				imageIndex := len(gjson.GetBytes(template, "choices.0.delta.images").Array())
				imagePayload := []byte(`{"type":"image_url","image_url":{"url":""}}`)
				imagePayload, _ = sjson.SetBytes(imagePayload, "index", imageIndex)
				imagePayload, _ = sjson.SetBytes(imagePayload, "image_url.url", imageURL)
				template, _ = sjson.SetBytes(template, "choices.0.delta.role", "assistant")
				template, _ = sjson.SetRawBytes(template, "choices.0.delta.images.-1", imagePayload)
			}
		}
	}

	upstreamFinishReason := p.UpstreamFinishReason
	sawToolCall := p.SawToolCall
	usageExists := gjson.GetBytes(rawJSON, "response.usageMetadata").Exists()
	isFinalChunk := upstreamFinishReason != "" && usageExists

	if isFinalChunk {
		var finishReason string
		if sawToolCall {
			finishReason = "tool_calls"
		} else if upstreamFinishReason == "MAX_TOKENS" {
			finishReason = "max_tokens"
		} else {
			finishReason = "stop"
		}
		template, _ = sjson.SetBytes(template, "choices.0.finish_reason", finishReason)
		template, _ = sjson.SetBytes(template, "choices.0.native_finish_reason", strings.ToLower(upstreamFinishReason))
	}

	return [][]byte{template}
}

// convertAntigravityResponseToOpenAINonStream translates a non-streaming response.
func convertAntigravityResponseToOpenAINonStream(_ context.Context, _ string, originalRequestRawJSON, _, rawJSON []byte, _ *any) []byte {
	responseResult := gjson.GetBytes(rawJSON, "response")
	if !responseResult.Exists() {
		return []byte{}
	}
	rawJSON = []byte(responseResult.Raw)

	sanitizedNameMap := SanitizedToolNameMap(originalRequestRawJSON)
	var unixTimestamp int64
	template := []byte(`{"id":"","object":"chat.completion","created":123456,"model":"model","choices":[]}`)

	if modelVersionResult := gjson.GetBytes(rawJSON, "modelVersion"); modelVersionResult.Exists() {
		template, _ = sjson.SetBytes(template, "model", modelVersionResult.String())
	}

	if createTimeResult := gjson.GetBytes(rawJSON, "createTime"); createTimeResult.Exists() {
		t, err := time.Parse(time.RFC3339Nano, createTimeResult.String())
		if err == nil {
			unixTimestamp = t.Unix()
		}
		template, _ = sjson.SetBytes(template, "created", unixTimestamp)
	} else {
		template, _ = sjson.SetBytes(template, "created", unixTimestamp)
	}

	if responseIDResult := gjson.GetBytes(rawJSON, "responseId"); responseIDResult.Exists() {
		template, _ = sjson.SetBytes(template, "id", responseIDResult.String())
	}

	if usageResult := gjson.GetBytes(rawJSON, "usageMetadata"); usageResult.Exists() {
		if candidatesTokenCountResult := usageResult.Get("candidatesTokenCount"); candidatesTokenCountResult.Exists() {
			template, _ = sjson.SetBytes(template, "usage.completion_tokens", candidatesTokenCountResult.Int())
		}
		if totalTokenCountResult := usageResult.Get("totalTokenCount"); totalTokenCountResult.Exists() {
			template, _ = sjson.SetBytes(template, "usage.total_tokens", totalTokenCountResult.Int())
		}
		promptTokenCount := usageResult.Get("promptTokenCount").Int()
		thoughtsTokenCount := usageResult.Get("thoughtsTokenCount").Int()
		cachedTokenCount := usageResult.Get("cachedContentTokenCount").Int()
		template, _ = sjson.SetBytes(template, "usage.prompt_tokens", promptTokenCount)
		if thoughtsTokenCount > 0 {
			template, _ = sjson.SetBytes(template, "usage.completion_tokens_details.reasoning_tokens", thoughtsTokenCount)
		}
		if cachedTokenCount > 0 {
			template, _ = sjson.SetBytes(template, "usage.prompt_tokens_details.cached_tokens", cachedTokenCount)
		}
	}

	candidates := gjson.GetBytes(rawJSON, "candidates")
	if candidates.IsArray() {
		candidates.ForEach(func(_, candidate gjson.Result) bool {
			choiceTemplate := []byte(`{"index":0,"message":{"role":"assistant","content":null,"reasoning_content":null,"tool_calls":null},"finish_reason":null,"native_finish_reason":null}`)
			choiceTemplate, _ = sjson.SetBytes(choiceTemplate, "index", candidate.Get("index").Int())

			if finishReasonResult := candidate.Get("finishReason"); finishReasonResult.Exists() {
				choiceTemplate, _ = sjson.SetBytes(choiceTemplate, "finish_reason", strings.ToLower(finishReasonResult.String()))
				choiceTemplate, _ = sjson.SetBytes(choiceTemplate, "native_finish_reason", strings.ToLower(finishReasonResult.String()))
			}

			partsResult := candidate.Get("content.parts")
			hasFunctionCall := false
			if partsResult.IsArray() {
				partsResults := partsResult.Array()
				for i := 0; i < len(partsResults); i++ {
					partResult := partsResults[i]
					partTextResult := partResult.Get("text")
					functionCallResult := partResult.Get("functionCall")
					inlineDataResult := partResult.Get("inlineData")
					if !inlineDataResult.Exists() {
						inlineDataResult = partResult.Get("inline_data")
					}

					if partTextResult.Exists() {
						if partResult.Get("thought").Bool() {
							oldVal := gjson.GetBytes(choiceTemplate, "message.reasoning_content").String()
							choiceTemplate, _ = sjson.SetBytes(choiceTemplate, "message.reasoning_content", oldVal+partTextResult.String())
						} else {
							oldVal := gjson.GetBytes(choiceTemplate, "message.content").String()
							choiceTemplate, _ = sjson.SetBytes(choiceTemplate, "message.content", oldVal+partTextResult.String())
						}
						choiceTemplate, _ = sjson.SetBytes(choiceTemplate, "message.role", "assistant")
					} else if functionCallResult.Exists() {
						hasFunctionCall = true
						toolCallsResult := gjson.GetBytes(choiceTemplate, "message.tool_calls")
						if !toolCallsResult.Exists() || !toolCallsResult.IsArray() {
							choiceTemplate, _ = sjson.SetRawBytes(choiceTemplate, "message.tool_calls", []byte(`[]`))
						}
						functionCallItemTemplate := []byte(`{"id":"","type":"function","function":{"name":"","arguments":""}}`)
						fcName := RestoreSanitizedToolName(sanitizedNameMap, functionCallResult.Get("name").String())
						functionCallItemTemplate, _ = sjson.SetBytes(functionCallItemTemplate, "id", fmt.Sprintf("%s-%d-%d", fcName, time.Now().UnixNano(), atomic.AddUint64(&functionCallIDCounter, 1)))
						functionCallItemTemplate, _ = sjson.SetBytes(functionCallItemTemplate, "function.name", fcName)
						if fcArgsResult := functionCallResult.Get("args"); fcArgsResult.Exists() {
							functionCallItemTemplate, _ = sjson.SetBytes(functionCallItemTemplate, "function.arguments", fcArgsResult.Raw)
						}
						choiceTemplate, _ = sjson.SetBytes(choiceTemplate, "message.role", "assistant")
						choiceTemplate, _ = sjson.SetRawBytes(choiceTemplate, "message.tool_calls.-1", functionCallItemTemplate)
					} else if inlineDataResult.Exists() {
						data := inlineDataResult.Get("data").String()
						if data != "" {
							mimeType := inlineDataResult.Get("mimeType").String()
							if mimeType == "" {
								mimeType = inlineDataResult.Get("mime_type").String()
							}
							if mimeType == "" {
								mimeType = "image/png"
							}
							imageURL := fmt.Sprintf("data:%s;base64,%s", mimeType, data)
							imagesResult := gjson.GetBytes(choiceTemplate, "message.images")
							if !imagesResult.Exists() || !imagesResult.IsArray() {
								choiceTemplate, _ = sjson.SetRawBytes(choiceTemplate, "message.images", []byte(`[]`))
							}
							imageIndex := len(gjson.GetBytes(choiceTemplate, "message.images").Array())
							imagePayload := []byte(`{"type":"image_url","image_url":{"url":""}}`)
							imagePayload, _ = sjson.SetBytes(imagePayload, "index", imageIndex)
							imagePayload, _ = sjson.SetBytes(imagePayload, "image_url.url", imageURL)
							choiceTemplate, _ = sjson.SetBytes(choiceTemplate, "message.role", "assistant")
							choiceTemplate, _ = sjson.SetRawBytes(choiceTemplate, "message.images.-1", imagePayload)
						}
					}
				}
			}

			if hasFunctionCall {
				choiceTemplate, _ = sjson.SetBytes(choiceTemplate, "finish_reason", "tool_calls")
				choiceTemplate, _ = sjson.SetBytes(choiceTemplate, "native_finish_reason", "tool_calls")
			}

			template, _ = sjson.SetRawBytes(template, "choices.-1", choiceTemplate)
			return true
		})
	}

	return template
}
