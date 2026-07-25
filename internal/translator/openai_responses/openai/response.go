package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/translator/common"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var dataTag = []byte("data:")

type openaiToResponsesState struct {
	ResponseID         string
	CreatedAt          int64
	Model              string
	Seq                int
	OutputIndex        int
	MessageItemAdded   bool
	ContentPartAdded   bool
	MessageItemID      string
	MessageOutputIndex int
	TextAcc            strings.Builder
	DoneSent           bool
}

func getOpenAIToResponsesState(param *any) *openaiToResponsesState {
	if *param == nil {
		*param = &openaiToResponsesState{MessageOutputIndex: -1}
	}
	return (*param).(*openaiToResponsesState)
}

func (s *openaiToResponsesState) nextSeq() int {
	s.Seq++
	return s.Seq
}

func (s *openaiToResponsesState) allocateOutputIndex() int {
	idx := s.OutputIndex
	s.OutputIndex++
	return idx
}

func (s *openaiToResponsesState) messageOutputIndex() int {
	if s.MessageOutputIndex < 0 {
		s.MessageOutputIndex = s.allocateOutputIndex()
	}
	return s.MessageOutputIndex
}

func sseFrame(event string, payload []byte) []byte {
	return append(common.SSEEventData(event, payload), '\n', '\n')
}

func baseEvent(state *openaiToResponsesState, eventType string) []byte {
	event := []byte(`{"type":"","sequence_number":0,"response":{"id":"","object":"response","created_at":0,"status":"in_progress","output":[],"model":""}}`)
	event, _ = sjson.SetBytes(event, "type", eventType)
	event, _ = sjson.SetBytes(event, "sequence_number", state.nextSeq())
	event, _ = sjson.SetBytes(event, "response.id", state.ResponseID)
	event, _ = sjson.SetBytes(event, "response.created_at", state.CreatedAt)
	event, _ = sjson.SetBytes(event, "response.model", state.Model)
	return event
}

func buildMessageItem(state *openaiToResponsesState) []byte {
	item := []byte(`{"type":"response.output_item.added","sequence_number":0,"output_index":0,"item":{"id":"","type":"message","status":"in_progress","content":[],"role":"assistant"}}`)
	item, _ = sjson.SetBytes(item, "sequence_number", state.nextSeq())
	item, _ = sjson.SetBytes(item, "output_index", state.messageOutputIndex())
	item, _ = sjson.SetBytes(item, "item.id", state.MessageItemID)
	return item
}

func buildContentPartAdded(state *openaiToResponsesState) []byte {
	part := []byte(`{"type":"response.content_part.added","sequence_number":0,"item_id":"","output_index":0,"content_index":0,"part":{"type":"output_text","text":""}}`)
	part, _ = sjson.SetBytes(part, "sequence_number", state.nextSeq())
	part, _ = sjson.SetBytes(part, "item_id", state.MessageItemID)
	part, _ = sjson.SetBytes(part, "output_index", state.messageOutputIndex())
	return part
}

func convertOpenAIStreamToResponses(_ context.Context, _ string, _, _ []byte, rawChunk []byte, param *any) [][]byte {
	raw := bytes.TrimSpace(rawChunk)
	if bytes.HasPrefix(raw, dataTag) {
		raw = bytes.TrimSpace(raw[5:])
	}
	if len(raw) == 0 || bytes.Equal(raw, []byte("[DONE]")) {
		return nil
	}
	if !gjson.ValidBytes(raw) {
		return nil
	}

	state := getOpenAIToResponsesState(param)
	root := gjson.ParseBytes(raw)

	if state.ResponseID == "" {
		state.ResponseID = root.Get("id").String()
		if state.ResponseID == "" {
			state.ResponseID = fmt.Sprintf("resp_%d", time.Now().UnixNano())
		}
	}
	if state.CreatedAt == 0 {
		state.CreatedAt = root.Get("created").Int()
		if state.CreatedAt == 0 {
			state.CreatedAt = time.Now().Unix()
		}
	}
	if state.Model == "" {
		state.Model = root.Get("model").String()
	}

	var events [][]byte

	if !state.MessageItemAdded {
		events = append(events, sseFrame("response.created", baseEvent(state, "response.created")))
		events = append(events, sseFrame("response.in_progress", baseEvent(state, "response.in_progress")))
	}

	choice := root.Get("choices.0")
	delta := choice.Get("delta")
	content := delta.Get("content")
	finishReason := choice.Get("finish_reason").String()

	if content.Exists() && content.Type == gjson.String && content.String() != "" {
		if !state.MessageItemAdded {
			state.MessageItemAdded = true
			state.MessageItemID = fmt.Sprintf("msg_%s", state.ResponseID)
			events = append(events, sseFrame("response.output_item.added", buildMessageItem(state)))
		}
		if !state.ContentPartAdded {
			state.ContentPartAdded = true
			events = append(events, sseFrame("response.content_part.added", buildContentPartAdded(state)))
		}
		text := content.String()
		state.TextAcc.WriteString(text)

		deltaEvent := []byte(`{"type":"response.output_text.delta","sequence_number":0,"item_id":"","output_index":0,"content_index":0,"delta":""}`)
		deltaEvent, _ = sjson.SetBytes(deltaEvent, "sequence_number", state.nextSeq())
		deltaEvent, _ = sjson.SetBytes(deltaEvent, "item_id", state.MessageItemID)
		deltaEvent, _ = sjson.SetBytes(deltaEvent, "output_index", state.messageOutputIndex())
		deltaEvent, _ = sjson.SetBytes(deltaEvent, "delta", text)
		events = append(events, sseFrame("response.output_text.delta", deltaEvent))
	}

	if finishReason != "" && finishReason != "null" && !state.DoneSent {
		state.DoneSent = true
		outputIndex := state.messageOutputIndex()

		if state.MessageItemAdded {
			fullText := state.TextAcc.String()

			done := []byte(`{"type":"response.output_text.done","sequence_number":0,"item_id":"","output_index":0,"content_index":0,"text":""}`)
			done, _ = sjson.SetBytes(done, "sequence_number", state.nextSeq())
			done, _ = sjson.SetBytes(done, "item_id", state.MessageItemID)
			done, _ = sjson.SetBytes(done, "output_index", outputIndex)
			done, _ = sjson.SetBytes(done, "text", fullText)
			events = append(events, sseFrame("response.output_text.done", done))

			partDone := []byte(`{"type":"response.content_part.done","sequence_number":0,"item_id":"","output_index":0,"content_index":0,"part":{"type":"output_text","text":""}}`)
			partDone, _ = sjson.SetBytes(partDone, "sequence_number", state.nextSeq())
			partDone, _ = sjson.SetBytes(partDone, "item_id", state.MessageItemID)
			partDone, _ = sjson.SetBytes(partDone, "output_index", outputIndex)
			partDone, _ = sjson.SetBytes(partDone, "part.text", fullText)
			events = append(events, sseFrame("response.content_part.done", partDone))

			itemDone := []byte(`{"type":"response.output_item.done","sequence_number":0,"output_index":0,"item":{"id":"","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":""}]}}`)
			itemDone, _ = sjson.SetBytes(itemDone, "sequence_number", state.nextSeq())
			itemDone, _ = sjson.SetBytes(itemDone, "output_index", outputIndex)
			itemDone, _ = sjson.SetBytes(itemDone, "item.id", state.MessageItemID)
			itemDone, _ = sjson.SetBytes(itemDone, "item.content.0.text", fullText)
			events = append(events, sseFrame("response.output_item.done", itemDone))
		}

		completed := baseEvent(state, "response.completed")
		completed, _ = sjson.SetBytes(completed, "response.status", "completed")
		if state.MessageItemAdded {
			output := []map[string]interface{}{
				{
					"id":           state.MessageItemID,
					"type":         "message",
					"role":         "assistant",
					"content":      []map[string]interface{}{{"type": "output_text", "text": state.TextAcc.String()}},
					"output_index": outputIndex,
				},
			}
			rawOut, _ := json.Marshal(output)
			completed, _ = sjson.SetRawBytes(completed, "response.output", rawOut)
		}
		if usage := root.Get("usage"); usage.Exists() {
			completed, _ = sjson.SetBytes(completed, "response.usage.input_tokens", usage.Get("prompt_tokens").Int())
			completed, _ = sjson.SetBytes(completed, "response.usage.output_tokens", usage.Get("completion_tokens").Int())
			completed, _ = sjson.SetBytes(completed, "response.usage.total_tokens", usage.Get("total_tokens").Int())
		}
		events = append(events, sseFrame("response.completed", completed))
	}

	return events
}
