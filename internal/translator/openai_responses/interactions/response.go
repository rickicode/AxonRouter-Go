package interactions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ConvertInteractionsResponseToOpenAIResponsesNonStream converts a complete
// Interactions response into the OpenAI Responses API non-streaming shape.
func ConvertInteractionsResponseToOpenAIResponsesNonStream(_ context.Context, modelName string, _, _ []byte, rawResponse []byte, _ *any) []byte {
	root := gjson.ParseBytes(rawResponse)
	out := []byte(`{"id":"","object":"response","status":"completed","model":"","output":[]}`)
	out, _ = sjson.SetBytes(out, "id", firstNonEmpty(root.Get("id").String(), root.Get("interaction.id").String()))
	out, _ = sjson.SetBytes(out, "model", responseModel(modelName, root))
	steps := root.Get("steps")
	if !steps.Exists() {
		steps = root.Get("interaction.steps")
	}
	steps.ForEach(func(_, step gjson.Result) bool {
		if item, ok := interactionsStepToResponsesOutput(step); ok {
			out, _ = sjson.SetRawBytes(out, "output.-1", item)
		}
		return true
	})
	out = setResponsesUsageFromInteractions(out, "usage", root.Get("usage"))
	return out
}

func interactionsStepToResponsesOutput(step gjson.Result) ([]byte, bool) {
	switch step.Get("type").String() {
	case "model_output":
		item := []byte(`{"type":"message","role":"assistant","content":[]}`)
		if id := firstNonEmpty(step.Get("id").String(), step.Get("step_id").String()); id != "" {
			item, _ = sjson.SetBytes(item, "id", id)
		}
		content := step.Get("content")
		if content.Type == gjson.String {
			part := []byte(`{"type":"output_text","text":""}`)
			part, _ = sjson.SetBytes(part, "text", content.String())
			item, _ = sjson.SetRawBytes(item, "content.-1", part)
		} else {
			content.ForEach(func(_, part gjson.Result) bool {
				if converted, ok := interactionsContentPartToResponses(part); ok {
					item, _ = sjson.SetRawBytes(item, "content.-1", converted)
				}
				return true
			})
		}
		return item, true
	case "thought":
		var texts []string
		content := step.Get("content")
		if content.Type == gjson.String {
			texts = append(texts, content.String())
		} else {
			content.ForEach(func(_, part gjson.Result) bool {
				if t := firstNonEmpty(part.Get("text").String(), part.Get("content.text").String()); t != "" {
					texts = append(texts, t)
				}
				return true
			})
		}
		item := responsesReasoningItem(strings.Join(texts, ""))
		if id := firstNonEmpty(step.Get("id").String(), step.Get("step_id").String()); id != "" {
			item, _ = sjson.SetBytes(item, "id", id)
		}
		return item, true
	case "function_call":
		callID := firstNonEmpty(step.Get("call_id").String(), step.Get("id").String())
		return responsesFunctionCallItem(callID, step.Get("name").String(), jsonStringValue(step.Get("arguments"), "{}")), true
	}
	return nil, false
}

func interactionsContentPartToResponses(part gjson.Result) ([]byte, bool) {
	partType := part.Get("type").String()
	if partType == "" && part.Get("text").Exists() {
		partType = "text"
	}
	switch partType {
	case "text":
		out := []byte(`{"type":"output_text","text":""}`)
		out, _ = sjson.SetBytes(out, "text", part.Get("text").String())
		return out, true
	case "image":
		out := []byte(`{"type":"output_image"}`)
		if url := part.Get("image_url").String(); url != "" {
			out, _ = sjson.SetBytes(out, "image_url", url)
		}
		return out, true
	}
	return nil, false
}

func responseModel(modelName string, root gjson.Result) string {
	return firstNonEmpty(modelName, root.Get("model").String(), root.Get("response.model").String(), root.Get("interaction.model").String())
}

func setResponsesUsageFromInteractions(out []byte, path string, usage gjson.Result) []byte {
	if !usage.Exists() {
		return out
	}
	if v := usage.Get("input_tokens"); v.Exists() {
		out, _ = sjson.SetBytes(out, path+".input_tokens", v.Int())
	}
	if v := usage.Get("output_tokens"); v.Exists() {
		out, _ = sjson.SetBytes(out, path+".output_tokens", v.Int())
	}
	if v := usage.Get("total_tokens"); v.Exists() {
		out, _ = sjson.SetBytes(out, path+".total_tokens", v.Int())
	}
	if v := usage.Get("cached_tokens"); v.Exists() {
		out, _ = sjson.SetBytes(out, path+".input_tokens_details.cached_tokens", v.Int())
	}
	if v := usage.Get("reasoning_tokens"); v.Exists() {
		out, _ = sjson.SetBytes(out, path+".output_tokens_details.reasoning_tokens", v.Int())
	}
	return out
}

// interactionsToResponsesStreamState accumulates Interactions SSE chunks and
// rebuilds OpenAI Responses API streaming events.
type interactionsToResponsesStreamState struct {
	Seq           int
	Done          bool
	CurrentIndex  int
	ItemIDs       map[int]string
	ItemTypes     map[int]string
	TextAcc       map[int]*strings.Builder
	ReasoningAcc  map[int]*strings.Builder
	ToolArgs      map[int]*strings.Builder
	ToolNames     map[int]string
	ToolCallIDs   map[int]string
}

// ConvertInteractionsResponseToOpenAIResponses converts Interactions SSE chunks
// to OpenAI Responses API streaming events.
func ConvertInteractionsResponseToOpenAIResponses(_ context.Context, modelName string, _, _ []byte, rawChunk []byte, param *any) [][]byte {
	st := getInteractionsToResponsesState(param)

	raw := bytes.TrimSpace(rawChunk)
	if bytes.HasPrefix(raw, []byte("data:")) {
		raw = bytes.TrimSpace(raw[5:])
	}
	if len(raw) == 0 {
		return nil
	}
	if bytes.Equal(raw, []byte("[DONE]")) {
		if st.Done {
			return nil
		}
		st.Done = true
		return [][]byte{[]byte("data: [DONE]\n\n")}
	}

	root := gjson.ParseBytes(raw)
	switch root.Get("event_type").String() {
	case "interaction.created":
		interaction := root.Get("interaction")
		return [][]byte{responsesCreatedEvent(modelName, interaction, st)}
	case "step.start":
		return interactionsStepStartToResponses(root, st)
	case "step.delta":
		return interactionsStepDeltaToResponses(root, st)
	case "step.stop":
		return interactionsStepStopToResponses(root, st)
	case "interaction.completed", "finish":
		return [][]byte{responsesCompletedEvent(modelName, root, st)}
	}
	return nil
}

func getInteractionsToResponsesState(param *any) *interactionsToResponsesStreamState {
	if *param == nil {
		*param = &interactionsToResponsesStreamState{
			ItemIDs:      make(map[int]string),
			ItemTypes:    make(map[int]string),
			TextAcc:      make(map[int]*strings.Builder),
			ReasoningAcc: make(map[int]*strings.Builder),
			ToolArgs:     make(map[int]*strings.Builder),
			ToolNames:    make(map[int]string),
			ToolCallIDs:  make(map[int]string),
		}
	}
	return (*param).(*interactionsToResponsesStreamState)
}

func responsesCreatedEvent(modelName string, interaction gjson.Result, st *interactionsToResponsesStreamState) []byte {
	responseID := firstNonEmpty(interaction.Get("id").String(), fmt.Sprintf("resp_%d", time.Now().UnixMilli()))
	payload := map[string]interface{}{
		"type":            "response.created",
		"sequence_number": nextResponsesSeq(st),
		"response": map[string]interface{}{
			"id":         responseID,
			"object":     "response",
			"status":     "in_progress",
			"model":      firstNonEmpty(modelName, interaction.Get("model").String()),
			"created_at": time.Now().Unix(),
		},
	}
	b, _ := json.Marshal(payload)
	return []byte("data: " + string(b) + "\n\n")
}

func interactionsStepStartToResponses(root gjson.Result, st *interactionsToResponsesStreamState) [][]byte {
	index := int(root.Get("index").Int())
	step := root.Get("step")
	stepType := step.Get("type").String()
	itemID := firstNonEmpty(step.Get("id").String(), step.Get("call_id").String(), fmt.Sprintf("item_%d", index))
	st.ItemIDs[index] = itemID
	st.ItemTypes[index] = stepType
	st.CurrentIndex = index

	switch stepType {
	case "model_output":
		payload := responseOutputItemAdded(index, itemID, "message", map[string]interface{}{
			"role": "assistant", "content": []interface{}{},
		}, st)
		partPayload := responseContentPartAdded(index, itemID, map[string]interface{}{"type": "output_text", "text": ""}, st)
		return [][]byte{payload, partPayload}
	case "thought":
		return [][]byte{responseOutputItemAdded(index, itemID, "reasoning", map[string]interface{}{"summary": []interface{}{}}, st)}
	case "function_call":
		st.ToolNames[index] = step.Get("name").String()
		st.ToolCallIDs[index] = itemID
		if st.ToolArgs[index] == nil {
			st.ToolArgs[index] = &strings.Builder{}
		}
		if args := step.Get("arguments"); args.Exists() && strings.TrimSpace(args.Raw) != "{}" {
			st.ToolArgs[index].WriteString(jsonStringValue(args, "{}"))
		}
		payload := responseOutputItemAdded(index, itemID, "function_call", map[string]interface{}{
			"call_id":   itemID,
			"name":      step.Get("name").String(),
			"arguments": "",
		}, st)
		return [][]byte{payload}
	}
	return nil
}

func interactionsStepDeltaToResponses(root gjson.Result, st *interactionsToResponsesStreamState) [][]byte {
	index := int(root.Get("index").Int())
	st.CurrentIndex = index
	delta := root.Get("delta")
	switch delta.Get("type").String() {
	case "thought_summary":
		text := firstNonEmpty(delta.Get("content.text").String(), delta.Get("text").String())
		if text == "" {
			return nil
		}
		if st.ReasoningAcc[index] == nil {
			st.ReasoningAcc[index] = &strings.Builder{}
		}
		st.ReasoningAcc[index].WriteString(text)
		return [][]byte{responseReasoningSummaryTextDelta(index, text, st)}
	case "arguments_delta":
		args := delta.Get("arguments").String()
		if st.ToolArgs[index] == nil {
			st.ToolArgs[index] = &strings.Builder{}
		}
		st.ToolArgs[index].WriteString(args)
		return [][]byte{responseFunctionCallArgumentsDelta(index, st.ItemIDs[index], args, st)}
	default:
		text := delta.Get("text").String()
		if text == "" {
			return nil
		}
		if st.TextAcc[index] == nil {
			st.TextAcc[index] = &strings.Builder{}
		}
		st.TextAcc[index].WriteString(text)
		return [][]byte{responseOutputTextDelta(index, st.ItemIDs[index], text, st)}
	}
}

func interactionsStepStopToResponses(root gjson.Result, st *interactionsToResponsesStreamState) [][]byte {
	index := int(root.Get("index").Int())
	itemID := st.ItemIDs[index]
	switch st.ItemTypes[index] {
	case "model_output":
		text := ""
		if acc := st.TextAcc[index]; acc != nil {
			text = acc.String()
		}
		var events [][]byte
		events = append(events, responseOutputTextDone(index, itemID, text, st))
		events = append(events, responseContentPartDone(index, itemID, map[string]interface{}{"type": "output_text", "text": text}, st))
		events = append(events, responseOutputItemDone(index, itemID, "message", map[string]interface{}{
			"role": "assistant",
			"content": []interface{}{
				map[string]interface{}{"type": "output_text", "text": text},
			},
		}, st))
		return events
	case "function_call":
		args := ""
		if acc := st.ToolArgs[index]; acc != nil {
			args = acc.String()
		}
		return [][]byte{responseOutputItemDone(index, itemID, "function_call", map[string]interface{}{
			"call_id":   itemID,
			"name":      st.ToolNames[index],
			"arguments": args,
		}, st)}
	case "thought":
		return [][]byte{responseOutputItemDone(index, itemID, "reasoning", map[string]interface{}{
			"summary": []interface{}{},
		}, st)}
	}
	return nil
}

func responsesCompletedEvent(modelName string, root gjson.Result, st *interactionsToResponsesStreamState) []byte {
	interaction := root.Get("interaction")
	responseID := firstNonEmpty(interaction.Get("id").String(), root.Get("id").String(), fmt.Sprintf("resp_%d", time.Now().UnixMilli()))
	payload := map[string]interface{}{
		"type":            "response.completed",
		"sequence_number": nextResponsesSeq(st),
		"response": map[string]interface{}{
			"id":         responseID,
			"object":     "response",
			"status":     "completed",
			"model":      responseModel(modelName, interaction),
			"created_at": time.Now().Unix(),
			"output":     buildCompletedOutput(st),
		},
	}
	b, _ := json.Marshal(payload)
	return []byte("data: " + string(b) + "\n\n")
}

func buildCompletedOutput(st *interactionsToResponsesStreamState) []map[string]interface{} {
	maxIndex := -1
	for index := range st.ItemTypes {
		if index > maxIndex {
			maxIndex = index
		}
	}
	var output []map[string]interface{}
	for index := 0; index <= maxIndex; index++ {
		itemType, ok := st.ItemTypes[index]
		if !ok {
			continue
		}
		itemID := st.ItemIDs[index]
		switch itemType {
		case "model_output":
			text := ""
			if acc := st.TextAcc[index]; acc != nil {
				text = acc.String()
			}
			output = append(output, map[string]interface{}{
				"id":   itemID,
				"type": "message",
				"role": "assistant",
				"content": []map[string]interface{}{
					{"type": "output_text", "text": text},
				},
			})
		case "thought":
			text := ""
			if acc := st.ReasoningAcc[index]; acc != nil {
				text = acc.String()
			}
			output = append(output, map[string]interface{}{
				"id":   itemID,
				"type": "reasoning",
				"summary": []map[string]interface{}{
					{"type": "summary_text", "text": text},
				},
			})
		case "function_call":
			args := ""
			if acc := st.ToolArgs[index]; acc != nil {
				args = acc.String()
			}
			output = append(output, map[string]interface{}{
				"id":        itemID,
				"type":      "function_call",
				"call_id":   itemID,
				"name":      st.ToolNames[index],
				"arguments": args,
			})
		}
	}
	return output
}

func responseOutputItemAdded(index int, itemID, itemType string, item map[string]interface{}, st *interactionsToResponsesStreamState) []byte {
	payload := map[string]interface{}{
		"type":            "response.output_item.added",
		"sequence_number": nextResponsesSeq(st),
		"output_index":    index,
		"item": map[string]interface{}{
			"id":   itemID,
			"type": itemType,
		},
	}
	for k, v := range item {
		payload["item"].(map[string]interface{})[k] = v
	}
	b, _ := json.Marshal(payload)
	return []byte("data: " + string(b) + "\n\n")
}

func responseContentPartAdded(index int, itemID string, part map[string]interface{}, st *interactionsToResponsesStreamState) []byte {
	payload := map[string]interface{}{
		"type":            "response.content_part.added",
		"sequence_number": nextResponsesSeq(st),
		"output_index":    index,
		"content_index":   0,
		"item_id":         itemID,
		"part":            part,
	}
	b, _ := json.Marshal(payload)
	return []byte("data: " + string(b) + "\n\n")
}

func responseOutputTextDelta(index int, itemID, text string, st *interactionsToResponsesStreamState) []byte {
	payload := map[string]interface{}{
		"type":            "response.output_text.delta",
		"sequence_number": nextResponsesSeq(st),
		"output_index":    index,
		"content_index":   0,
		"item_id":         itemID,
		"delta":           text,
	}
	b, _ := json.Marshal(payload)
	return []byte("data: " + string(b) + "\n\n")
}

func responseOutputTextDone(index int, itemID, text string, st *interactionsToResponsesStreamState) []byte {
	payload := map[string]interface{}{
		"type":            "response.output_text.done",
		"sequence_number": nextResponsesSeq(st),
		"output_index":    index,
		"content_index":   0,
		"item_id":         itemID,
		"text":            text,
	}
	b, _ := json.Marshal(payload)
	return []byte("data: " + string(b) + "\n\n")
}

func responseContentPartDone(index int, itemID string, part map[string]interface{}, st *interactionsToResponsesStreamState) []byte {
	payload := map[string]interface{}{
		"type":            "response.content_part.done",
		"sequence_number": nextResponsesSeq(st),
		"output_index":    index,
		"content_index":   0,
		"item_id":         itemID,
		"part":            part,
	}
	b, _ := json.Marshal(payload)
	return []byte("data: " + string(b) + "\n\n")
}

func responseOutputItemDone(index int, itemID, itemType string, item map[string]interface{}, st *interactionsToResponsesStreamState) []byte {
	payload := map[string]interface{}{
		"type":            "response.output_item.done",
		"sequence_number": nextResponsesSeq(st),
		"output_index":    index,
		"item": map[string]interface{}{
			"id":   itemID,
			"type": itemType,
		},
	}
	for k, v := range item {
		payload["item"].(map[string]interface{})[k] = v
	}
	b, _ := json.Marshal(payload)
	return []byte("data: " + string(b) + "\n\n")
}

func responseReasoningSummaryTextDelta(index int, text string, st *interactionsToResponsesStreamState) []byte {
	payload := map[string]interface{}{
		"type":            "response.reasoning_summary_text.delta",
		"sequence_number": nextResponsesSeq(st),
		"output_index":    index,
		"summary_index":   0,
		"delta":           text,
	}
	b, _ := json.Marshal(payload)
	return []byte("data: " + string(b) + "\n\n")
}

func responseFunctionCallArgumentsDelta(index int, itemID, args string, st *interactionsToResponsesStreamState) []byte {
	payload := map[string]interface{}{
		"type":            "response.function_call_arguments.delta",
		"sequence_number": nextResponsesSeq(st),
		"output_index":    index,
		"item_id":         itemID,
		"delta":           args,
	}
	b, _ := json.Marshal(payload)
	return []byte("data: " + string(b) + "\n\n")
}

func nextResponsesSeq(st *interactionsToResponsesStreamState) int {
	st.Seq++
	return st.Seq
}



