package responses

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var dataTag = []byte("data:")

// codexStreamState holds state for Codex Responses→OpenAI streaming.
type codexStreamState struct {
	MessageID string
	CreatedAt int64
	Model string
	FunctionCallIndex int
	HasReceivedArgumentsDelta bool
	HasToolCallAnnounced bool
	LastImageHashByItemID map[string][32]byte
	DoneSent bool
}

func getCodexState(param *any) *codexStreamState {
	if *param == nil {
		*param = &codexStreamState{
			FunctionCallIndex:     -1,
			LastImageHashByItemID: make(map[string][32]byte),
		}
	}
	return (*param).(*codexStreamState)
}

// convertCodexResponseToOpenAIStream converts Codex Responses streaming events to OpenAI Chat Completions format.
func convertCodexResponseToOpenAIStream(_ context.Context, modelName string, originalRequestRawJSON, _, rawChunk []byte, param *any) [][]byte {
	state := getCodexState(param)

	raw := bytes.TrimSpace(rawChunk)
	if bytes.HasPrefix(raw, dataTag) {
		raw = bytes.TrimSpace(raw[5:])
	}
	if len(raw) == 0 || bytes.Equal(raw, []byte("[DONE]")) {
		return nil
	}

	root := gjson.ParseBytes(raw)
	eventType := root.Get("type").String()

	if eventType == "response.created" {
		state.MessageID = root.Get("response.id").String()
		state.CreatedAt = root.Get("response.created_at").Int()
		state.Model = root.Get("response.model").String()
		return nil
	}

	// Initialize the OpenAI SSE template.
	template := []byte(`{"id":"","object":"chat.completion.chunk","created":0,"model":"","choices":[{"index":0,"delta":{},"finish_reason":null,"native_finish_reason":null}]}`)

	cachedModel := state.Model
	if modelResult := root.Get("model"); modelResult.Exists() {
		template, _ = sjson.SetBytes(template, "model", modelResult.String())
	} else if cachedModel != "" {
		template, _ = sjson.SetBytes(template, "model", cachedModel)
	} else if modelName != "" {
		template, _ = sjson.SetBytes(template, "model", modelName)
	}
	template, _ = sjson.SetBytes(template, "created", state.CreatedAt)
	template, _ = sjson.SetBytes(template, "id", state.MessageID)

	// Extract usage if present on any event (e.g. response.completed carries it).
	if usageResult := root.Get("response.usage"); usageResult.Exists() {
		template = extractCodexUsage(template, usageResult)
	}

	switch eventType {
	case "response.output_text.delta":
		text := root.Get("delta").String()
		template, _ = sjson.SetBytes(template, "choices.0.delta.role", "assistant")
		template, _ = sjson.SetBytes(template, "choices.0.delta.content", text)
	case "response.reasoning_summary_text.delta":
		template, _ = sjson.SetBytes(template, "choices.0.delta.role", "assistant")
		template, _ = sjson.SetBytes(template, "choices.0.delta.reasoning_content", root.Get("delta").String())
	case "response.reasoning_summary_text.done":
		template, _ = sjson.SetBytes(template, "choices.0.delta.role", "assistant")
		template, _ = sjson.SetBytes(template, "choices.0.delta.reasoning_content", "\n\n")
	case "response.output_item.added":
		itemResult := root.Get("item")
		if !itemResult.Exists() || itemResult.Get("type").String() != "function_call" {
			return nil
		}
		state.FunctionCallIndex++
		state.HasReceivedArgumentsDelta = false
		state.HasToolCallAnnounced = true

		functionCallItemTemplate := []byte(`{"index":0,"id":"","type":"function","function":{"name":"","arguments":""}}`)
		functionCallItemTemplate, _ = sjson.SetBytes(functionCallItemTemplate, "index", state.FunctionCallIndex)
		functionCallItemTemplate, _ = sjson.SetBytes(functionCallItemTemplate, "id", itemResult.Get("call_id").String())
		name := itemResult.Get("name").String()
		rev := buildReverseMapFromOriginalOpenAI(originalRequestRawJSON)
		if orig, ok := rev[name]; ok {
			name = orig
		}
		functionCallItemTemplate, _ = sjson.SetBytes(functionCallItemTemplate, "function.name", name)
		functionCallItemTemplate, _ = sjson.SetBytes(functionCallItemTemplate, "function.arguments", "")

		template, _ = sjson.SetBytes(template, "choices.0.delta.role", "assistant")
		template, _ = sjson.SetRawBytes(template, "choices.0.delta.tool_calls", []byte(`[]`))
		template, _ = sjson.SetRawBytes(template, "choices.0.delta.tool_calls.-1", functionCallItemTemplate)
	case "response.function_call_arguments.delta":
		state.HasReceivedArgumentsDelta = true
		deltaValue := root.Get("delta").String()
		functionCallItemTemplate := []byte(`{"index":0,"function":{"arguments":""}}`)
		functionCallItemTemplate, _ = sjson.SetBytes(functionCallItemTemplate, "index", state.FunctionCallIndex)
		functionCallItemTemplate, _ = sjson.SetBytes(functionCallItemTemplate, "function.arguments", deltaValue)
		template, _ = sjson.SetRawBytes(template, "choices.0.delta.tool_calls", []byte(`[]`))
		template, _ = sjson.SetRawBytes(template, "choices.0.delta.tool_calls.-1", functionCallItemTemplate)
	case "response.function_call_arguments.done":
		if state.HasReceivedArgumentsDelta {
			return nil
		}
		fullArgs := root.Get("arguments").String()
		functionCallItemTemplate := []byte(`{"index":0,"function":{"arguments":""}}`)
		functionCallItemTemplate, _ = sjson.SetBytes(functionCallItemTemplate, "index", state.FunctionCallIndex)
		functionCallItemTemplate, _ = sjson.SetBytes(functionCallItemTemplate, "function.arguments", fullArgs)
		template, _ = sjson.SetRawBytes(template, "choices.0.delta.tool_calls", []byte(`[]`))
		template, _ = sjson.SetRawBytes(template, "choices.0.delta.tool_calls.-1", functionCallItemTemplate)
	case "response.output_item.done":
		itemResult := root.Get("item")
		if !itemResult.Exists() {
			return nil
		}
		itemType := itemResult.Get("type").String()
		switch itemType {
		case "image_generation_call":
			return handleCodexImageGenerationEvent(state, itemResult, template)
		case "function_call":
			if state.HasToolCallAnnounced {
				state.HasToolCallAnnounced = false
				return nil
			}
			state.FunctionCallIndex++
			functionCallItemTemplate := []byte(`{"index":0,"id":"","type":"function","function":{"name":"","arguments":""}}`)
			functionCallItemTemplate, _ = sjson.SetBytes(functionCallItemTemplate, "index", state.FunctionCallIndex)
			functionCallItemTemplate, _ = sjson.SetBytes(functionCallItemTemplate, "id", itemResult.Get("call_id").String())
			name := itemResult.Get("name").String()
			rev := buildReverseMapFromOriginalOpenAI(originalRequestRawJSON)
			if orig, ok := rev[name]; ok {
				name = orig
			}
			functionCallItemTemplate, _ = sjson.SetBytes(functionCallItemTemplate, "function.name", name)
			functionCallItemTemplate, _ = sjson.SetBytes(functionCallItemTemplate, "function.arguments", itemResult.Get("arguments").String())
			template, _ = sjson.SetBytes(template, "choices.0.delta.role", "assistant")
			template, _ = sjson.SetRawBytes(template, "choices.0.delta.tool_calls", []byte(`[]`))
			template, _ = sjson.SetRawBytes(template, "choices.0.delta.tool_calls.-1", functionCallItemTemplate)
		default:
			return nil
		}
	case "response.image_generation_call.partial_image":
		itemID := root.Get("item_id").String()
		b64 := root.Get("partial_image_b64").String()
		if b64 == "" {
			return nil
		}
		return emitCodexImageDelta(state, itemID, b64, root.Get("output_format").String(), template)
	case "response.completed", "response.done":
		if state.DoneSent {
			return nil
		}
		state.DoneSent = true
		finishReason := "stop"
		if state.FunctionCallIndex != -1 {
			finishReason = "tool_calls"
		}
		template, _ = sjson.SetBytes(template, "choices.0.finish_reason", finishReason)
		template, _ = sjson.SetBytes(template, "choices.0.native_finish_reason", finishReason)
		if usageResult := root.Get("response.usage"); usageResult.Exists() {
			template = extractCodexUsage(template, usageResult)
		}
		chunk := []byte(fmt.Sprintf("data: %s\n\n", string(template)))
		return [][]byte{chunk}
	default:
		return nil
	}

	chunk := []byte(fmt.Sprintf("data: %s\n\n", string(template)))
	return [][]byte{chunk}
}

func extractCodexUsage(template []byte, usageResult gjson.Result) []byte {
	if outputTokensResult := usageResult.Get("output_tokens"); outputTokensResult.Exists() {
		template, _ = sjson.SetBytes(template, "usage.completion_tokens", outputTokensResult.Int())
	}
	if totalTokensResult := usageResult.Get("total_tokens"); totalTokensResult.Exists() {
		template, _ = sjson.SetBytes(template, "usage.total_tokens", totalTokensResult.Int())
	}
	if inputTokensResult := usageResult.Get("input_tokens"); inputTokensResult.Exists() {
		template, _ = sjson.SetBytes(template, "usage.prompt_tokens", inputTokensResult.Int())
	}
	if cachedTokensResult := usageResult.Get("input_tokens_details.cached_tokens"); cachedTokensResult.Exists() {
		template, _ = sjson.SetBytes(template, "usage.prompt_tokens_details.cached_tokens", cachedTokensResult.Int())
	}
	if reasoningTokensResult := usageResult.Get("output_tokens_details.reasoning_tokens"); reasoningTokensResult.Exists() {
		template, _ = sjson.SetBytes(template, "usage.completion_tokens_details.reasoning_tokens", reasoningTokensResult.Int())
	}
	return template
}

func handleCodexImageGenerationEvent(state *codexStreamState, itemResult gjson.Result, template []byte) [][]byte {
	itemID := itemResult.Get("id").String()
	b64 := itemResult.Get("result").String()
	if b64 == "" {
		return nil
	}
	return emitCodexImageDelta(state, itemID, b64, itemResult.Get("output_format").String(), template)
}

func emitCodexImageDelta(state *codexStreamState, itemID, b64, outputFormat string, template []byte) [][]byte {
	if itemID != "" {
		hash := sha256.Sum256([]byte(b64))
		if last, ok := state.LastImageHashByItemID[itemID]; ok && last == hash {
			return nil
		}
		state.LastImageHashByItemID[itemID] = hash
	}
	mimeType := mimeTypeFromCodexOutputFormat(outputFormat)
	imageURL := "data:" + mimeType + ";base64," + b64

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

	chunk := []byte(fmt.Sprintf("data: %s\n\n", string(template)))
	return [][]byte{chunk}
}

// convertCodexResponseToOpenAINonStream converts a non-streaming Codex response to a non-streaming OpenAI response.
func convertCodexResponseToOpenAINonStream(_ context.Context, _ string, originalRequestRawJSON, _, rawResponse []byte, _ *any) []byte {
	rootResult := gjson.ParseBytes(rawResponse)

	responseResult := rootResult.Get("response")
	if !responseResult.Exists() {
		// Some upstreams return the response object directly without the wrapper.
		responseResult = rootResult
	}

	template := []byte(`{"id":"","object":"chat.completion","created":0,"model":"","choices":[{"index":0,"message":{"role":"assistant","content":null,"reasoning_content":null,"tool_calls":null,"images":null},"finish_reason":null,"native_finish_reason":null}]}`)

	if modelResult := responseResult.Get("model"); modelResult.Exists() {
		template, _ = sjson.SetBytes(template, "model", modelResult.String())
	}
	if createdAtResult := responseResult.Get("created_at"); createdAtResult.Exists() {
		template, _ = sjson.SetBytes(template, "created", createdAtResult.Int())
	} else {
		template, _ = sjson.SetBytes(template, "created", time.Now().Unix())
	}
	if idResult := responseResult.Get("id"); idResult.Exists() {
		template, _ = sjson.SetBytes(template, "id", idResult.String())
	}

	if usageResult := responseResult.Get("usage"); usageResult.Exists() {
		template = extractCodexUsage(template, usageResult)
	}

	var toolCalls [][]byte
	var images [][]byte
	outputResult := responseResult.Get("output")
	if outputResult.IsArray() {
		outputArray := outputResult.Array()
		var contentText string
		var reasoningText string
		for _, outputItem := range outputArray {
			outputType := outputItem.Get("type").String()
			switch outputType {
			case "reasoning":
				if summaryResult := outputItem.Get("summary"); summaryResult.IsArray() {
					for _, summaryItem := range summaryResult.Array() {
						if summaryItem.Get("type").String() == "summary_text" {
							if text := summaryItem.Get("text").String(); text != "" {
								reasoningText += text
							}
							break
						}
					}
				}
			case "message":
				if contentResult := outputItem.Get("content"); contentResult.IsArray() {
					for _, contentItem := range contentResult.Array() {
						if contentItem.Get("type").String() == "output_text" {
							if text := contentItem.Get("text").String(); text != "" {
								contentText += text
							}
							break
						}
					}
				}
			case "function_call":
				functionCallTemplate := []byte(`{"id":"","type":"function","function":{"name":"","arguments":""}}`)
				if callIDResult := outputItem.Get("call_id"); callIDResult.Exists() {
					functionCallTemplate, _ = sjson.SetBytes(functionCallTemplate, "id", callIDResult.String())
				}
				if nameResult := outputItem.Get("name"); nameResult.Exists() {
					n := nameResult.String()
					rev := buildReverseMapFromOriginalOpenAI(originalRequestRawJSON)
					if orig, ok := rev[n]; ok {
						n = orig
					}
					functionCallTemplate, _ = sjson.SetBytes(functionCallTemplate, "function.name", n)
				}
				if argsResult := outputItem.Get("arguments"); argsResult.Exists() {
					functionCallTemplate, _ = sjson.SetBytes(functionCallTemplate, "function.arguments", argsResult.String())
				}
				toolCalls = append(toolCalls, functionCallTemplate)
			case "image_generation_call":
				b64 := outputItem.Get("result").String()
				if b64 == "" {
					break
				}
				outputFormat := outputItem.Get("output_format").String()
				mimeType := mimeTypeFromCodexOutputFormat(outputFormat)
				imageURL := "data:" + mimeType + ";base64," + b64
				imagePayload := []byte(`{"type":"image_url","image_url":{"url":""}}`)
				imagePayload, _ = sjson.SetBytes(imagePayload, "index", len(images))
				imagePayload, _ = sjson.SetBytes(imagePayload, "image_url.url", imageURL)
				images = append(images, imagePayload)
			}
		}

		if contentText != "" {
			template, _ = sjson.SetBytes(template, "choices.0.message.content", contentText)
		}
		if reasoningText != "" {
			template, _ = sjson.SetBytes(template, "choices.0.message.reasoning_content", reasoningText)
		}
		if len(toolCalls) > 0 {
			template, _ = sjson.SetRawBytes(template, "choices.0.message.tool_calls", []byte(`[]`))
			for _, tc := range toolCalls {
				template, _ = sjson.SetRawBytes(template, "choices.0.message.tool_calls.-1", tc)
			}
		}
		if len(images) > 0 {
			template, _ = sjson.SetRawBytes(template, "choices.0.message.images", []byte(`[]`))
			for _, img := range images {
				template, _ = sjson.SetRawBytes(template, "choices.0.message.images.-1", img)
			}
		}
	}

	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}
	if statusResult := responseResult.Get("status"); statusResult.Exists() {
		if statusResult.String() == "completed" || statusResult.String() == "incomplete" {
			template, _ = sjson.SetBytes(template, "choices.0.finish_reason", finishReason)
			template, _ = sjson.SetBytes(template, "choices.0.native_finish_reason", finishReason)
		}
	} else {
		template, _ = sjson.SetBytes(template, "choices.0.finish_reason", finishReason)
		template, _ = sjson.SetBytes(template, "choices.0.native_finish_reason", finishReason)
	}

	return template
}

func mimeTypeFromCodexOutputFormat(outputFormat string) string {
	if outputFormat == "" {
		return "image/png"
	}
	if strings.Contains(outputFormat, "/") {
		return outputFormat
	}
	switch strings.ToLower(outputFormat) {
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	case "gif":
		return "image/gif"
	default:
		return "image/png"
	}
}

// buildReverseMapFromOriginalOpenAI builds a map of shortened tool name -> original tool name.
func buildReverseMapFromOriginalOpenAI(original []byte) map[string]string {
	tools := gjson.GetBytes(original, "tools")
	rev := map[string]string{}
	if tools.IsArray() && len(tools.Array()) > 0 {
		var names []string
		arr := tools.Array()
		for i := range arr {
			t := arr[i]
			if t.Get("type").String() != "function" {
				continue
			}
			fn := t.Get("function")
			if !fn.Exists() {
				continue
			}
			if v := fn.Get("name"); v.Exists() {
				names = append(names, v.String())
			}
		}
		if len(names) > 0 {
			m := buildShortNameMap(names)
			for orig, short := range m {
				rev[short] = orig
			}
		}
	}
	return rev
}
