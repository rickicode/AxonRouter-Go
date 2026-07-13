package version

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var version string

// String returns the current AxonRouter-Go version from the embedded VERSION file.
func String() string {
	return strings.TrimSpace(version)
}
