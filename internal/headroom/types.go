package headroom

// PayloadKind identifies the detected kind of tool output.
type PayloadKind string

const (
	KindGitDiff     PayloadKind = "git_diff"
	KindGitLog      PayloadKind = "git_log"
	KindGitStatus   PayloadKind = "git_status"
	KindGrep        PayloadKind = "grep"
	KindFindTree    PayloadKind = "find_tree"
	KindBuildLog    PayloadKind = "build_log"
	KindSearch      PayloadKind = "search"
	KindToolResult  PayloadKind = "tool_result"
	KindGeneric     PayloadKind = "generic"
)

// PayloadHeader is the envelope sent to / from the Headroom service.
type PayloadHeader struct {
	Kind       PayloadKind `json:"kind"`
	SourceSize int         `json:"source_size"`
	Original   string      `json:"original"`
}

// CompressedResult is the response from the Headroom service.
type CompressedResult struct {
	Kind        PayloadKind `json:"kind"`
	Original    string      `json:"original"`
	Compressed  string      `json:"compressed"`
	OriginalSize int        `json:"original_size"`
	SavedBytes  int         `json:"saved_bytes"`
	Techniques  []string    `json:"techniques"`
}

// Metrics hold cumulative counters for the headroom service.
type Metrics struct {
	Total      int64 `json:"total"`
	BytesSaved int64 `json:"bytes_saved"`
	Errors     int64 `json:"errors"`
}

// Config configures the headroom client/server.
type Config struct {
	Enabled        bool   `json:"enabled"`
	Endpoint       string `json:"endpoint"`
	TimeoutMs      int    `json:"timeout_ms"`
	MaxPayloadBytes int   `json:"max_payload_bytes"`
}

// DefaultTimeoutMs is the default RPC timeout.
const DefaultTimeoutMs = 30000

// DefaultMaxPayloadBytes is the default max payload (512 KB).
const DefaultMaxPayloadBytes = 512 * 1024

// DefaultEndpoint is the loopback address used by the in-process server.
const DefaultEndpoint = "http://127.0.0.1:9123"
