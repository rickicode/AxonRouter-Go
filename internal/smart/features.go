package smart

import (
	"regexp"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/models"
	"github.com/tidwall/gjson"
)

// Features captures a normalized request description used by the smart router.
type Features struct {
	TotalTokens      int64   `json:"total_tokens"`
	NumMessages      int     `json:"num_messages"`
	HasImage         bool    `json:"has_image"`
	HasAudio         bool    `json:"has_audio"`
	HasVideo         bool    `json:"has_video"`
	HasPDF           bool    `json:"has_pdf"`
	ToolCount        int     `json:"tool_count"`
	ToolCallCount    int     `json:"tool_call_count"`
	ReasoningEffort  string  `json:"reasoning_effort"`
	HasCodeHint      bool    `json:"has_code_hint"`
	LanguageHint     string  `json:"language_hint"`
	Score            float64 `json:"score"`
}

var codeFenceRE = regexp.MustCompile("(?i)```\\s*([a-z0-9_]+)?")

// ExtractFeatures parses request body bytes and returns a feature vector.
func ExtractFeatures(body []byte) Features {
	f := Features{}
	if len(body) == 0 {
		return f
	}
	root := gjson.ParseBytes(body)
	f.extractMessages(root)
	f.extractInput(root)
	f.extractContents(root)
	f.extractTools(root)
	f.extractReasoning(root)
	f.estimateTokens(root)
	f.Score = computeScore(f)
	return f
}

func (f *Features) extractMessages(root gjson.Result) {
	messages := root.Get("messages").Array()
	f.NumMessages = len(messages)
	for _, m := range messages {
		f.inspectContent(m.Get("content"))
		if m.Get("role").String() == "assistant" {
			f.ToolCallCount += len(m.Get("tool_calls").Array())
		}
	}
}

func (f *Features) extractInput(root gjson.Result) {
	input := root.Get("input").Array()
	if len(input) > 0 {
		f.NumMessages = len(input)
	}
	for _, item := range input {
		f.inspectContent(item.Get("content"))
	}
}

func (f *Features) extractContents(root gjson.Result) {
	contents := root.Get("contents").Array()
	if len(contents) > 0 {
		f.NumMessages = len(contents)
	}
	for _, c := range contents {
		for _, part := range c.Get("parts").Array() {
			f.inspectPart(part)
		}
	}
	if req := root.Get("request"); req.Exists() {
		for _, c := range req.Get("contents").Array() {
			for _, part := range c.Get("parts").Array() {
				f.inspectPart(part)
			}
		}
	}
}

func (f *Features) inspectContent(content gjson.Result) {
	switch content.Type {
	case gjson.String:
		text := content.String()
		f.detectCodeHints(text)
	case gjson.JSON:
		if content.IsArray() {
			for _, part := range content.Array() {
				f.inspectPart(part)
			}
		} else if content.IsObject() {
			f.inspectPart(content)
		}
	}
}

func (f *Features) inspectPart(part gjson.Result) {
	if !part.IsObject() {
		return
	}
	t := part.Get("type").String()
	switch t {
	case "image_url", "image", "input_image":
		f.HasImage = true
	case "input_audio", "audio":
		f.HasAudio = true
	case "video", "input_video":
		f.HasVideo = true
	case "file", "document", "input_file":
		if isPDF(part) {
			f.HasPDF = true
		}
	}
	if inline := part.Get("inlineData"); inline.Exists() {
		f.detectMime(inline.Get("mimeType").String())
	}
	if fileData := part.Get("fileData"); fileData.Exists() {
		f.detectMime(fileData.Get("mimeType").String())
	}
	if text := part.Get("text"); text.Exists() && text.Type == gjson.String {
		f.detectCodeHints(text.String())
	}
}

func (f *Features) detectMime(mime string) {
	mime = strings.ToLower(mime)
	switch {
	case strings.HasPrefix(mime, "image/"):
		f.HasImage = true
	case strings.HasPrefix(mime, "audio/"):
		f.HasAudio = true
	case strings.HasPrefix(mime, "video/"):
		f.HasVideo = true
	case strings.Contains(mime, "pdf"):
		f.HasPDF = true
	}
}

func isPDF(part gjson.Result) bool {
	mime := part.Get("mime_type").String()
	if mime == "" {
		mime = part.Get("mimeType").String()
	}
	fname := part.Get("file_name").String()
	if fname == "" {
		fname = part.Get("filename").String()
	}
	if strings.Contains(strings.ToLower(mime), "pdf") || strings.HasSuffix(strings.ToLower(fname), ".pdf") {
		return true
	}
	if file := part.Get("file"); file.IsObject() {
		return isPDF(file)
	}
	return false
}

func (f *Features) detectCodeHints(text string) {
	if len(text) < 6 {
		return
	}
	matches := codeFenceRE.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return
	}
	f.HasCodeHint = true
	for _, m := range matches {
		if len(m) > 1 && m[1] != "" && f.LanguageHint == "" {
			f.LanguageHint = strings.ToLower(m[1])
			break
		}
	}
}

func (f *Features) extractTools(root gjson.Result) {
	tools := root.Get("tools").Array()
	f.ToolCount = len(tools)
	for _, tool := range tools {
		if fn := tool.Get("function"); fn.Exists() {
			if name := fn.Get("name").String(); name != "" {
				if strings.Contains(name, "bash") || strings.Contains(name, "shell") || strings.Contains(name, "command") {
					f.HasCodeHint = true
				}
			}
		}
	}
}

func (f *Features) extractReasoning(root gjson.Result) {
	for _, path := range []string{"reasoning_effort", "reasoning.effort", "thinking.type", "thinking.budget_tokens", "max_completion_tokens"} {
		if r := root.Get(path); r.Exists() {
			switch path {
			case "reasoning_effort", "reasoning.effort", "thinking.type":
				f.ReasoningEffort = strings.ToLower(r.String())
			case "thinking.budget_tokens", "max_completion_tokens":
				if r.Int() > 32000 {
					f.ReasoningEffort = "high"
				} else if r.Int() > 8000 {
					f.ReasoningEffort = "medium"
				} else if r.Int() > 0 {
					f.ReasoningEffort = "low"
				}
			}
		}
	}
}

func (f *Features) estimateTokens(root gjson.Result) {
	// Use explicit max_tokens / max_completion_tokens as an upper-bound hint.
	var total int64
	if maxTokens := root.Get("max_tokens"); maxTokens.Exists() {
		if v := maxTokens.Int(); v > 0 {
			total = v
		}
	}
	if mc := root.Get("max_completion_tokens"); mc.Exists() && mc.Int() > total {
		total = mc.Int()
	}

	// Sum rough per-message token counts (4 chars ~ 1 token fallback).
	messages := root.Get("messages").Array()
	if len(messages) == 0 {
		messages = root.Get("input").Array()
	}
	var inputChars int64
	for _, m := range messages {
		content := m.Get("content")
		inputChars += contentLength(content)
	}
	// Each prompt token is ~4 UTF-8 bytes; clamp to max_tokens when present.
	est := inputChars / 4
	if total > 0 && est > total {
		est = total
	}
	f.TotalTokens = est
}

func contentLength(content gjson.Result) int64 {
	switch content.Type {
	case gjson.String:
		return int64(len(content.String()))
	case gjson.JSON:
		if content.IsArray() {
			var n int64
			for _, part := range content.Array() {
				n += contentLength(part)
			}
			return n
		}
		if content.IsObject() {
			if text := content.Get("text"); text.Exists() {
				return int64(len(text.String()))
			}
		}
	}
	return 0
}

// computeScore returns a normalized complexity score (0-1+) used by the router.
// Short prompts always receive a small baseline so the score is never zero.
func computeScore(f Features) float64 {
	s := 0.02 + 0.02*float64(f.NumMessages)
	// workload complexity
	if f.NumMessages > 10 {
		s += 0.3
	} else if f.NumMessages > 3 {
		s += 0.15
	}
	if f.TotalTokens > 32000 {
		s += 0.35
	} else if f.TotalTokens > 8000 {
		s += 0.2
	} else if f.TotalTokens > 2000 {
		s += 0.1
	}
	// modalities and tools
	if f.HasImage {
		s += 0.15
	}
	if f.HasAudio {
		s += 0.15
	}
	if f.HasVideo {
		s += 0.25
	}
	if f.HasPDF {
		s += 0.1
	}
	if f.ToolCount > 0 {
		s += 0.1 + float64(f.ToolCallCount)*0.05
	}
	if f.HasCodeHint {
		s += 0.1
	}
	// reasoning
	switch f.ReasoningEffort {
	case "high":
		s += 0.3
	case "medium":
		s += 0.15
	case "low":
		s += 0.05
	}
	return s
}

// ExtractCapabilityFeatures returns a simple capability set derived from body content.
func ExtractCapabilityFeatures(body []byte) models.ModelCapabilities {
	var caps models.ModelCapabilities
	f := ExtractFeatures(body)
	if f.HasImage {
		caps.Vision = true
	}
	if f.HasAudio {
		caps.AudioInput = true
	}
	if f.HasVideo {
		caps.VideoInput = true
	}
	if f.HasPDF {
		caps.PDF = true
	}
	if f.ToolCount > 0 {
		caps.Tools = true
	}
	return caps
}
