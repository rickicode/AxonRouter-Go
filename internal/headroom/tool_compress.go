package headroom

import (
	"encoding/json"
	"sync"
)

// ToolCompressor applies headroom compression to tool/tool_result content
// blocks in chat-style request bodies. It is safe for concurrent use.
type ToolCompressor struct {
	client *Client
	cfg    Config
	mu     sync.RWMutex
}

// NewToolCompressor creates a compressor wired to the supplied config.
func NewToolCompressor(cfg Config) *ToolCompressor {
	return &ToolCompressor{
		client: NewClient(cfg, nil, nil, NewMetrics()),
		cfg:    cfg,
	}
}

// UpdateConfig swaps the active config and client at runtime.
func (tc *ToolCompressor) UpdateConfig(cfg Config) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.cfg = cfg
	tc.client = NewClient(cfg, nil, nil, NewMetrics())
}

// Config returns the current config.
func (tc *ToolCompressor) Config() Config {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	return tc.cfg
}

// DefaultToolThreshold is the default size below which tool content is left
// uncompressed.
const DefaultToolThreshold = 256

func compressContentBlocks(client *Client, parts []any, threshold int) bool {
	changed := false
	for i := range parts {
		part, ok := parts[i].(map[string]any)
		if !ok {
			continue
		}
		typ, _ := part["type"].(string)
		if typ != "tool" && typ != "tool_result" && typ != "tool_use" {
			continue
		}
		var text string
		var key string
		if s, ok := part["text"].(string); ok {
			text = s
			key = "text"
		} else if s, ok := part["content"].(string); ok {
			text = s
			key = "content"
		}
		if len(text) < threshold {
			continue
		}
		out, err := client.Compress([]byte(text))
		if err != nil {
			continue
		}
		if out.Kind == KindUnknown {
			continue
		}
		part[key] = string(out.Data)
		changed = true
		// Also handle nested content arrays inside tool_result blocks.
		if inner, ok := part["content"].([]any); ok && key != "content" {
			if compressContentBlocks(client, inner, threshold) {
				changed = true
			}
		}
	}
	return changed
}

// CompressToolBlocks walks a request body's messages/content, compresses
// tool/tool_result text blocks via headroom, and returns the modified body.
// Compression is skipped when disabled, the payload is under threshold, or
// the detected kind is unknown. On failure/timeout the original text is kept.
func (tc *ToolCompressor) CompressToolBlocks(body []byte, threshold int) []byte {
	cfg := tc.Config()
	if !cfg.Enabled {
		return body
	}
	if threshold <= 0 {
		threshold = DefaultToolThreshold
	}

	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return body
	}
	messages, ok := root["messages"].([]any)
	if !ok {
		return body
	}
	changed := false
	for _, rawMsg := range messages {
		msg, ok := rawMsg.(map[string]any)
		if !ok {
			continue
		}
		content, ok := msg["content"]
		if !ok {
			continue
		}
		switch c := content.(type) {
		case string:
			continue
		case []any:
			if compressContentBlocks(tc.client, c, threshold) {
				changed = true
			}
		}
	}
	if !changed {
		return body
	}
	out, err := json.Marshal(root)
	if err != nil {
		return body
	}
	return out
}

// SetGlobalToolCompressor installs a package-level tool compressor. This is
// used by translator packages that do not receive a compressor through their
// function signatures. It is safe to call multiple times to reconfigre.
var (
	globalToolCompressor     *ToolCompressor
	globalToolCompressorOnce sync.Once
)

// InitGlobalToolCompressor lazily creates the package-level compressor.
func InitGlobalToolCompressor(cfg Config) {
	globalToolCompressorOnce.Do(func() {
		globalToolCompressor = NewToolCompressor(cfg)
	})
}

// SetGlobalToolCompressor updates the package-level compressor after init.
func SetGlobalToolCompressor(tc *ToolCompressor) {
	globalToolCompressor = tc
}

// GlobalToolCompressor returns the current package-level compressor.
func GlobalToolCompressor() *ToolCompressor {
	return globalToolCompressor
}
