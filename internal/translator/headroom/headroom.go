package thrm

import (
	"sync"

	"github.com/rickicode/AxonRouter-Go/internal/headroom"
)

// headroomClient is the lazily-initialized global translator headroom client.
var headroomClient struct {
	mu     sync.RWMutex
	client *headroom.Client
}

func getClient() *headroom.Client {
	headroomClient.mu.RLock()
	c := headroomClient.client
	headroomClient.mu.RUnlock()
	if c != nil {
		return c
	}
	headroomClient.mu.Lock()
	defer headroomClient.mu.Unlock()
	if headroomClient.client == nil {
		cfg := headroom.DefaultConfig()
		headroomClient.client = headroom.NewClient(cfg, nil, nil, nil)
	}
	return headroomClient.client
}

// Config exposes the current global client configuration.
func Config() headroom.Config {
	return getClient().Config()
}

// SetClient swaps the global headroom client used by translators.
// It is exported for tests.
func SetClient(c *headroom.Client) {
	headroomClient.mu.Lock()
	defer headroomClient.mu.Unlock()
	headroomClient.client = c
}

// CompressToolText compresses a tool/tool_result text payload when headroom
// is enabled and the payload is large enough. On failure or timeout the
// original bytes are returned unchanged.
func CompressToolText(data []byte) []byte {
	c := getClient()
	if !c.Config().Enabled {
		return data
	}
	if len(data) < headroom.MinToolContentBytes {
		return data
	}
	out, err := c.Compress(data)
	if err != nil {
		return data
	}
	return out.Data
}
