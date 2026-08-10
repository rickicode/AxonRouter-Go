package mcp

// Server represents a registered MCP stdio server.
type Server struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Command      string            `json:"command"`
	Args         []string          `json:"args"`
	Env          map[string]string `json:"env"`
	Enabled      bool              `json:"enabled"`
	RestartPolicy string           `json:"restart_policy"`
	MaxClients   int               `json:"max_clients"`
	MaxIdleSec   int               `json:"max_idle_sec"`
	CreatedAt    int64             `json:"created_at"`
	UpdatedAt    int64             `json:"updated_at"`
}

// defaultConfig fills zero-value runtime configuration with safe defaults.
func (s *Server) defaultConfig() {
	if s.RestartPolicy == "" {
		s.RestartPolicy = "on-failure"
	}
	if s.MaxClients <= 0 {
		s.MaxClients = 4
	}
	if s.MaxIdleSec <= 0 {
		s.MaxIdleSec = 60
	}
}

// Valid restart policies.
const (
	RestartAlways    = "always"
	RestartOnFailure = "on-failure"
	RestartNever     = "never"
)

// IsValidRestartPolicy reports whether p is an accepted restart policy.
func IsValidRestartPolicy(p string) bool {
	switch p {
	case RestartAlways, RestartOnFailure, RestartNever:
		return true
	default:
		return false
	}
}

// Message is a lightweight JSON-RPC envelope used by the stdio-SSE bridge.
type Message struct {
	JSONRPC string      `json:"jsonrpc,omitempty"`
	ID      interface{} `json:"id,omitempty"`
	Method  string      `json:"method,omitempty"`
	Params  interface{} `json:"params,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
}

// WriteRequest is the body expected on the message POST endpoint.
type WriteRequest struct {
	Message Message `json:"message"`
}

// ToolsResponse mirrors the common MCP tools/list response shape.
type ToolsResponse struct {
	Tools []Tool `json:"tools"`
}

// Tool represents a single MCP tool.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema interface{} `json:"inputSchema,omitempty"`
}
