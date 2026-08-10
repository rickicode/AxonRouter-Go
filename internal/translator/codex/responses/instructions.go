package responses

import _ "embed"

//go:embed codexInstructions.txt
var codexDefaultInstructions string

// DefaultInstructions returns the default system instruction for Codex requests.
func DefaultInstructions() string { return codexDefaultInstructions }
