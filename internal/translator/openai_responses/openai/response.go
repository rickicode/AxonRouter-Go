package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

var dataTag = []byte("data:")

// openAIStreamState accumulates OpenAI Chat Completions streaming chunks and
// rebuilds OpenAI Responses API streaming events.
type openAIStreamState struct {
	ResponseID       string
	CreatedAt        int64
	Model            string
	OutputIndex      int
	TextAcc          strings.Builder
	ToolArgsAcc      map[int]*strings.Builder
	ToolNames        map[int]string
	ToolIDs          map[int]string
	ToolCallIDs      map[int]string
	ToolAnnounced    map[int]bool
	MessageItemAdded bool
	ContentPartAdded bool
	DoneSent         bool
}

func getOpenAIState(param *any) *openAIStreamState {
	if *param == nil {
		*param = &openAIStreamState{
			ToolArgsAcc:   make(map[int]*strings.Builder),
			ToolNames:     make(map[int]string),
			ToolIDs:       make(map[int]string),
			ToolCallIDs:   make(map[int]string),
			ToolAnnounced: make(map[int]bool),
		}
	}
	return (*param).(*openAIStreamState)
}

func (s *openAIStreamState) messageItemID() string {
	return "msg_" + s.ResponseID
}

func (s *openAIStreamState) appendItem(item map[string]interface{}) {
	if item == nil {
		return
	}
	item["output_index"] = s.OutputIndex
	s.OutputIndex++
}

func buildResponsesEvent(state *openAIStreamState, eventType string, body map[string]interface{}) []byte {
	event := map[string]interface{}{
		"type":            eventType,
		"sequence_number": 0,
	}
	response := map[string]interface{}{
		"id":         state.ResponseID,
		"object":     "response",
		"created_at": state.CreatedAt,
		"model":      state.Model,
	}
	if eventType == "response.completed" {
		for k, v := range body {
			response[k] = v
		}
		event["response"] = response
	} else {
		event["response"] = response
		for k, v := range body {
			event[k] = v
		}
	}
	b, _ := json.Marshal(event)
	return []byte(fmt.Sprintf("data: %s\n\n", string(b)))
}

func buildCreatedAndInProgress(state *openAIStreamState) [][]byte {
	created := map[string]interface{}{
		"type":            "response.created",
		"sequence_number": 0,
		"response": map[string]interface{}{
			"id":         state.ResponseID,
			"object":     "response",
			"created_at": state.CreatedAt,
			"model":      state.Model,
			"status":     "in_progress",
			"output":     []interface{}{},
		},
	}
	inprog := map[string]interface{}{
		"type":            "response.in_progress",
		"sequence_number": 0,
		"response": map[string]interface{}{
			"id":         state.ResponseID,
			"object":     "response",
			"created_at": state.CreatedAt,
			"model":      state.Model,
			"status":     "in_progress",
		},
	}
	cb, _ := json.Marshal(created)
	ib, _ := json.Marshal(inprog)
	return [][]byte{
		[]byte(fmt.Sprintf("data: %s\n\n", string(cb))),
		[]byte(fmt.Sprintf("data: %s\n\n", string(ib))),
	}
}

// flushText finalizes any accumulated text content and returns the events needed
// to close out the message item.
func (s *openAIStreamState) flushText() [][]byte {
	if s.TextAcc.Len() == 0 {
		return nil
	}
	text := s.TextAcc.String()
	s.TextAcc.Reset()

	itemID := s.messageItemID()
	outputIndex := s.OutputIndex
	s.appendItem(map[string]interface{}{
		"type":         "message",
		"role":         "assistant",
		"id":           itemID,
		"content":      []map[string]interface{}{{"type": "output_text", "text": text}},
		"output_index": outputIndex,
	})

	var events [][]byte
	events = append(events, buildResponsesEvent(s, "response.output_text.done", map[string]interface{}{
		"item_id":       itemID,
		"output_index":  outputIndex,
		"content_index": 0,
		"text":          text,
	}))
	events = append(events, buildResponsesEvent(s, "response.content_part.done", map[string]interface{}{
		"item_id":       itemID,
		"output_index":  outputIndex,
		"content_index": 0,
		"part":          map[string]interface{}{"type": "output_text", "text": text},
	}))
	events = append(events, buildResponsesEvent(s, "response.output_item.done", map[string]interface{}{
		"output_index": outputIndex,
		"item": map[string]interface{}{
			"type":         "message",
			"role":         "assistant",
			"id":           itemID,
			"content":      []map[string]interface{}{{"type": "output_text", "text": text}},
			"output_index": outputIndex,
		},
	}))
	s.MessageItemAdded = false
	s.ContentPartAdded = false
	return events
}

// ensureMessageItemAdded emits response.output_item.added once for the assistant
// message being streamed.
func (s *openAIStreamState) ensureMessageItemAdded(events *[][]byte) {
	if s.MessageItemAdded {
		return
	}
	s.MessageItemAdded = true
	item := map[string]interface{}{
		"type":    "message",
		"role":    "assistant",
		"id":      s.messageItemID(),
		"content": []map[string]interface{}{{"type": "output_text", "text": ""}},
	}
	*events = append(*events, buildResponsesEvent(s, "response.output_item.added", map[string]interface{}{"item": item}))
}

// ensureContentPartAdded emits response.content_part.added once for the
// output_text part being streamed.
func (s *openAIStreamState) ensureContentPartAdded(events *[][]byte) {
	if s.ContentPartAdded {
		return
	}
	s.ContentPartAdded = true
	*events = append(*events, buildResponsesEvent(s, "response.content_part.added", map[string]interface{}{
		"item_id":       s.messageItemID(),
		"output_index":  s.OutputIndex,
		"content_index": 0,
		"part":          map[string]interface{}{"type": "output_text", "text": ""},
	}))
}

// flushToolCall finalizes a tool call at the given index and returns the
// completed function_call item.
func (s *openAIStreamState) flushToolCall(idx int) map[string]interface{} {
	acc := s.ToolArgsAcc[idx]
	if acc == nil {
		return nil
	}
	args := acc.String()
	item := map[string]interface{}{
		"type":      "function_call",
		"id":        s.ToolIDs[idx],
		"call_id":   s.ToolCallIDs[idx],
		"name":      s.ToolNames[idx],
		"arguments": args,
	}
	delete(s.ToolArgsAcc, idx)
	delete(s.ToolNames, idx)
	delete(s.ToolIDs, idx)
	delete(s.ToolCallIDs, idx)
	delete(s.ToolAnnounced, idx)
	return item
}

func (s *openAIStreamState) finalizeToolCalls() [][]byte {
	var events [][]byte
	var keys []int
	for k := range s.ToolArgsAcc {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	for _, idx := range keys {
		item := s.flushToolCall(idx)
		if item == nil {
			continue
		}
		outputIndex := s.OutputIndex
		item["output_index"] = outputIndex
		s.OutputIndex++
		events = append(events, buildResponsesEvent(s, "response.output_item.done", map[string]interface{}{
			"output_index": outputIndex,
			"item":         item,
		}))
	}
	return events
}

func deriveResponseID(chatID string) string {
	if strings.HasPrefix(chatID, "chatcmpl-") {
		return "resp_" + strings.TrimPrefix(chatID, "chatcmpl-")
	}
	if chatID != "" {
		return chatID
	}
	return "resp_" + fmt.Sprintf("%d", time.Now().UnixNano())
}

func mapUsage(usage gjson.Result) map[string]interface{} {
	usageMap := map[string]interface{}{
		"input_tokens":  usage.Get("prompt_tokens").Int(),
		"output_tokens": usage.Get("completion_tokens").Int(),
		"total_tokens":  usage.Get("total_tokens").Int(),
	}
	if usage.Get("total_tokens").Type == gjson.Null || !usage.Get("total_tokens").Exists() {
		usageMap["total_tokens"] = usageMap["input_tokens"].(int64) + usageMap["output_tokens"].(int64)
	}
	if cached := usage.Get("prompt_tokens_details.cached_tokens"); cached.Exists() && cached.Int() > 0 {
		usageMap["input_tokens_details"] = map[string]interface{}{"cached_tokens": cached.Int()}
	}
	if reasoning := usage.Get("completion_tokens_details.reasoning_tokens"); reasoning.Exists() && reasoning.Int() > 0 {
		usageMap["output_tokens_details"] = map[string]interface{}{"reasoning_tokens": reasoning.Int()}
	}
	return usageMap
}

// ConvertOpenAIChatToOpenAIResponsesNonStream converts a complete OpenAI Chat
// Completions response to the OpenAI Responses API non-streaming shape.
func ConvertOpenAIChatToOpenAIResponsesNonStream(_ context.Context, _ string, _, _ []byte, rawResponse []byte, _ *any) []byte {
	root := gjson.ParseBytes(rawResponse)

	responseID := deriveResponseID(root.Get("id").String())
	createdAt := root.Get("created").Int()
	if createdAt == 0 {
		createdAt = time.Now().Unix()
	}
	model := root.Get("model").String()

	out := map[string]interface{}{
		"id":         responseID,
		"object":     "response",
		"created_at": createdAt,
		"model":      model,
		"status":     "completed",
		"output":     []map[string]interface{}{},
	}

	outputIndex := 0
	var output []map[string]interface{}

	choices := root.Get("choices")
	if choices.Exists() && choices.IsArray() && len(choices.Array()) > 0 {
		message := choices.Array()[0].Get("message")
		if message.Exists() {
			content := message.Get("content").String()
			toolCalls := message.Get("tool_calls")
			finishReason := choices.Array()[0].Get("finish_reason").String()

			if content != "" {
				item := map[string]interface{}{
					"type":    "message",
					"role":    "assistant",
					"id":      "msg_" + responseID,
					"content": []map[string]interface{}{{"type": "output_text", "text": content}},
				}
				if finishReason == "" {
					item["status"] = "completed"
				}
				item["output_index"] = outputIndex
				output = append(output, item)
				outputIndex++
			}

			if toolCalls.Exists() && toolCalls.IsArray() {
				toolCallsArray := toolCalls.Array()
				for i := range toolCallsArray {
					tc := toolCallsArray[i]
					idx := outputIndex
					outputIndex++
					id := tc.Get("id").String()
					if id == "" {
						id = fmt.Sprintf("call_%s_%d", responseID, idx)
					}
					item := map[string]interface{}{
						"type":         "function_call",
						"id":           id,
						"call_id":      id,
						"name":         tc.Get("function.name").String(),
						"arguments":    tc.Get("function.arguments").String(),
						"output_index": idx,
					}
					output = append(output, item)
				}
			}
		}
	}

	out["output"] = output

	if usage := root.Get("usage"); usage.Exists() {
		out["usage"] = mapUsage(usage)
	}

	result, _ := json.Marshal(out)
	return result
}

// ConvertOpenAIChatToOpenAIResponsesStream converts OpenAI Chat Completions SSE
// chunks to OpenAI Responses API streaming events.
func ConvertOpenAIChatToOpenAIResponsesStream(_ context.Context, _ string, _, _ []byte, rawChunk []byte, param *any) [][]byte {
	state := getOpenAIState(param)

	raw := bytes.TrimSpace(rawChunk)
	if bytes.HasPrefix(raw, dataTag) {
		raw = bytes.TrimSpace(raw[5:])
	}
	if len(raw) == 0 || bytes.Equal(raw, []byte("[DONE]")) {
		return nil
	}

	root := gjson.ParseBytes(raw)

	if state.ResponseID == "" {
		state.ResponseID = deriveResponseID(root.Get("id").String())
	}
	if state.CreatedAt == 0 {
		state.CreatedAt = time.Now().Unix()
	}
	if state.Model == "" {
		state.Model = root.Get("model").String()
	}

	var events [][]byte

	// Emit response.created and response.in_progress on the first real chunk.
	if !state.DoneSent && state.OutputIndex == 0 && state.TextAcc.Len() == 0 && len(state.ToolArgsAcc) == 0 {
		events = append(events, buildCreatedAndInProgress(state)...)
	}

	choice := root.Get("choices.0")
	if choice.Exists() {
		delta := choice.Get("delta")
		finishReason := choice.Get("finish_reason").String()

		// Content delta handling.
		if textDelta := delta.Get("content"); textDelta.Exists() && textDelta.Type == gjson.String {
			text := textDelta.String()
			state.ensureMessageItemAdded(&events)
			state.ensureContentPartAdded(&events)
			state.TextAcc.WriteString(text)
			events = append(events, buildResponsesEvent(state, "response.output_text.delta", map[string]interface{}{
				"item_id":       state.messageItemID(),
				"output_index":  state.OutputIndex,
				"content_index": 0,
				"delta":         text,
			}))
		}

		// Tool-call delta handling.
		if toolCallsDelta := delta.Get("tool_calls"); toolCallsDelta.Exists() && toolCallsDelta.IsArray() {
			toolCallsDeltaArray := toolCallsDelta.Array()
			for i := range toolCallsDeltaArray {
				tc := toolCallsDeltaArray[i]
				idx := int(tc.Get("index").Int())
				if _, ok := state.ToolArgsAcc[idx]; !ok {
					state.ToolArgsAcc[idx] = &strings.Builder{}
				}

				id := tc.Get("id").String()
				if id != "" {
					state.ToolIDs[idx] = id
					state.ToolCallIDs[idx] = id
				}
				if name := tc.Get("function.name").String(); name != "" {
					state.ToolNames[idx] = name
				}

				// If we now have enough metadata to announce the tool call, and it
				// has not been announced yet, emit output_item.added.
				if !state.ToolAnnounced[idx] && (state.ToolIDs[idx] != "" || state.ToolNames[idx] != "") {
					state.ToolAnnounced[idx] = true
					callID := state.ToolCallIDs[idx]
					if callID == "" {
						callID = fmt.Sprintf("call_%s_%d", state.ResponseID, idx)
						state.ToolCallIDs[idx] = callID
						state.ToolIDs[idx] = callID
					}
					item := map[string]interface{}{
						"type":         "function_call",
						"id":           callID,
						"call_id":      callID,
						"name":         state.ToolNames[idx],
						"arguments":    "",
						"output_index": state.OutputIndex,
					}
					events = append(events, buildResponsesEvent(state, "response.output_item.added", map[string]interface{}{"item": item}))
				}

				if args := tc.Get("function.arguments").String(); args != "" {
					state.ToolArgsAcc[idx].WriteString(args)
					events = append(events, buildResponsesEvent(state, "response.function_call_arguments.delta", map[string]interface{}{
						"item_id": callIDForDelta(state, idx),
						"delta":   args,
					}))
				}
			}
		}

		// Finalize on finish_reason.
		if finishReason == "stop" || finishReason == "tool_calls" {
			if textEvents := state.flushText(); len(textEvents) > 0 {
				events = append(events, textEvents...)
			}
			if toolEvents := state.finalizeToolCalls(); len(toolEvents) > 0 {
				events = append(events, toolEvents...)
			}

			completedBody := map[string]interface{}{
				"status": "completed",
				"output": []map[string]interface{}{},
			}
			if usage := root.Get("usage"); usage.Exists() {
				completedBody["usage"] = mapUsage(usage)
			}
			events = append(events, buildResponsesEvent(state, "response.completed", completedBody))
			state.DoneSent = true
		}
	}

	return events
}

func callIDForDelta(state *openAIStreamState, idx int) string {
	if id := state.ToolCallIDs[idx]; id != "" {
		return id
	}
	return fmt.Sprintf("call_%s_%d", state.ResponseID, idx)
}
