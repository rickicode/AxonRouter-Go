// Package protocol defines Model Context Protocol JSON-RPC 2.0 types and constants.
// It supports both the 2024-11-05 and 2025-03-26 protocol versions.
package protocol

import (
	"encoding/json"
)

const (
	JSONRPCVersion = "2.0"

	// Protocol versions this server supports, newest first.
	ProtocolVersion20250326 = "2025-03-26"
	ProtocolVersion20241105 = "2024-11-05"

	// Standard MCP JSON-RPC methods.
	MethodInitialize                = "initialize"
	MethodInitialized               = "notifications/initialized"
	MethodPing                      = "ping"
	MethodToolsList                 = "tools/list"
	MethodToolsCall                 = "tools/call"
	MethodComplete                  = "completion/complete"
	MethodPromptsList               = "prompts/list"
	MethodResourcesList             = "resources/list"
	MethodResourcesRead             = "resources/read"
	MethodRootsList                 = "roots/list"
	MethodSamplingCreateMessage     = "sampling/createMessage"
	MethodNotificationsCancelled    = "notifications/cancelled"
	MethodNotificationsProgress     = "notifications/progress"
	MethodNotificationsRootsUpdated = "notifications/rootsUpdated"

	// ServerInfo identifies this implementation per MCP spec.
	ServerName    = "AxonRouter-MCP"
	ServerVersion = "0.1.0"
)

// RequestID is the flexible identifier used by MCP for JSON-RPC requests.
// It may be a string, number, or omitted for notifications.
type RequestID struct {
	value interface{}
}

// String creates a string RequestID.
func NewRequestIDString(s string) RequestID {
	return RequestID{value: s}
}

// Number creates a numeric RequestID.
func NewRequestIDNumber(n float64) RequestID {
	return RequestID{value: n}
}

// IsZero reports whether no id was provided.
func (r RequestID) IsZero() bool {
	return r.value == nil
}

// MarshalJSON implements json.Marshaler.
func (r RequestID) MarshalJSON() ([]byte, error) {
	if r.value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(r.value)
}

// UnmarshalJSON implements json.Unmarshaler and accepts string or number IDs.
func (r *RequestID) UnmarshalJSON(b []byte) error {
	b = trimJSONWhitespace(b)
	if len(b) == 0 {
		r.value = nil
		return nil
	}
	if b[0] == 'n' {
		r.value = nil
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		r.value = s
		return nil
	}
	var n float64
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	r.value = n
	return nil
}

// String returns the string form, used for comparisons and map keys.
func (r RequestID) String() string {
	if r.value == nil {
		return ""
	}
	if s, ok := r.value.(string); ok {
		return s
	}
	b, _ := json.Marshal(r.value)
	return string(b)
}

// Value returns the underlying value (nil, string, or float64).
func (r RequestID) Value() interface{} {
	return r.value
}

func trimJSONWhitespace(b []byte) []byte {
	i := 0
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	return b[i:]
}

// Request is a JSON-RPC 2.0 request used by MCP.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      RequestID       `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	Meta    json.RawMessage `json:"_meta,omitempty"`
}

// IsNotification returns true when the request has no id.
func (r *Request) IsNotification() bool {
	return r.ID.IsZero()
}

// Response is a JSON-RPC 2.0 response used by MCP.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      RequestID       `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ErrorObject    `json:"error,omitempty"`
}

// ErrorObject is the JSON-RPC error object.
type ErrorObject struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *ErrorObject) Error() string {
	return e.Message
}

// JSON-RPC standard error codes.
const (
	ErrParseError     = -32700
	ErrInvalidRequest = -32600
	ErrMethodNotFound = -32601
	ErrInvalidParams  = -32602
	ErrInternalError  = -32603
)

// MCP-specific error codes.
const (
	ErrToolNotFound       = -32601 // share method-not-found for unknown tools.
	ErrInvalidToolInput   = -32602
	ErrToolExecutionError = -32603
)

// InitializeParams is the parameter object for initialize.
type InitializeParams struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      Implementation     `json:"clientInfo"`
}

// Implementation describes a party to the MCP connection.
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ClientCapabilities lists optional client abilities.
type ClientCapabilities struct {
	Roots        *RootsCapability       `json:"roots,omitempty"`
	Sampling     *SamplingCapability    `json:"sampling,omitempty"`
	Experimental map[string]interface{} `json:"experimental,omitempty"`
}

// RootsCapability documents client root support.
type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// SamplingCapability documents client sampling support.
type SamplingCapability struct{}

// ServerCapabilities lists optional server abilities.
type ServerCapabilities struct {
	Logging      *LoggingCapability     `json:"logging,omitempty"`
	Prompts      *PromptsCapability     `json:"prompts,omitempty"`
	Resources    *ResourcesCapability   `json:"resources,omitempty"`
	Tools        *ToolsCapability       `json:"tools,omitempty"`
	Experimental map[string]interface{} `json:"experimental,omitempty"`
}

// LoggingCapability documents server logging support.
type LoggingCapability struct{}

// PromptsCapability documents prompt support.
type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCapability documents resource support.
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// ToolsCapability documents tool support.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// InitializeResult is the response to initialize.
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      Implementation     `json:"serverInfo"`
}

// EmptyResult is used for ping and initialized notifications that need a result.
type EmptyResult struct{}

// Tool describes an MCP tool exposed by the server.
type Tool struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	InputSchema json.RawMessage  `json:"inputSchema"`
	Annotations *ToolAnnotations `json:"annotations,omitempty"`
}

// ToolAnnotations is the 2025-03-26 optional hint block.
type ToolAnnotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    bool   `json:"readOnlyHint,omitempty"`
	DestructiveHint bool   `json:"destructiveHint,omitempty"`
	IdempotentHint  bool   `json:"idempotentHint,omitempty"`
	OpenWorldHint   bool   `json:"openWorldHint,omitempty"`
}

// ToolResult is the response payload for tools/call.
type ToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolContent is a union-ish result item.
type ToolContent struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	MimeType string          `json:"mimeType,omitempty"`
	URI      string          `json:"uri,omitempty"`
}

// ListToolsResult is the result for tools/list.
type ListToolsResult struct {
	Tools []Tool `json:"tools"`
}

// PingResult is the result for ping.
type PingResult struct{}
