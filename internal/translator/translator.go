// Package translator provides the main entry point for the translator system.
// It re-exports types and registry for convenience, and imports all translator implementations.
package translator

// Re-export types for convenience
import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

// Re-export commonly used types and functions
type Format = types.Format
type TranslateFunc = types.TranslateFunc
type TranslateStreamFunc = types.TranslateStreamFunc
type TranslateNonStreamFunc = types.TranslateNonStreamFunc
type ResponseTransform = types.ResponseTransform

// Re-export format constants
const (
	FormatOpenAI         = types.FormatOpenAI
	FormatClaude         = types.FormatClaude
	FormatGemini         = types.FormatGemini
	FormatCodexResponses = types.FormatCodexResponses
	FormatAntigravity    = types.FormatAntigravity
	FormatKiro           = types.FormatKiro
)

// Re-export registry functions
var (
	Register          = registry.Register
	Request           = registry.Request
	NeedConvert       = registry.NeedConvert
	Response          = registry.Response
	ResponseNonStream = registry.ResponseNonStream
	Default           = registry.Default
)
