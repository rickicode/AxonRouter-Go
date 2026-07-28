package common

import (
	"encoding/json"

	"github.com/rickicode/AxonRouter-Go/internal/headroom"
)

// CompressToolBlocks applies headroom compression to tool/tool_result text
// blocks inside a chat request body. It is fail-open: malformed bodies or
// compression errors return the original payload unchanged.
func CompressToolBlocks(body []byte, compressor *headroom.ToolCompressor, threshold int) []byte {
	if compressor == nil {
		return body
	}
	return compressor.CompressToolBlocks(body, threshold)
}

// CompressMessageContent compresses tool/tool_result text blocks inside the
// provided content array representation. The returned value can be marshaled
// back into the parent structure.
func CompressMessageContent(content any, compressor *headroom.ToolCompressor, threshold int) any {
	if compressor == nil || content == nil {
		return content
	}
	b, err := json.Marshal(map[string]any{"content": content})
	if err != nil {
		return content
	}
	compressed := compressor.CompressToolBlocks(b, threshold)
	if string(compressed) == string(b) {
		return content
	}
	var wrapper map[string]any
	if err := json.Unmarshal(compressed, &wrapper); err != nil {
		return content
	}
	out, _ := wrapper["content"]
	return out
}
