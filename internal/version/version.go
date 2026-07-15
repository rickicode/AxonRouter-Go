package version

import (
	_ "embed"
	"strings"
	"sync/atomic"
)

//go:embed VERSION
var version string

var testOverride atomic.Pointer[string]

// String returns the current AxonRouter-Go version from the embedded VERSION file.
// During tests the value may be overridden via SetTestVersion.
func String() string {
	if v := testOverride.Load(); v != nil {
		return *v
	}
	return strings.TrimSpace(version)
}

// SetTestVersion overrides String() with a fixed value for the duration of a test.
// It is exported only so that tests outside this package can control the reported
// version. Runtime code must not call this function.
func SetTestVersion(v string) {
	testOverride.Store(&v)
}

// ClearTestVersion removes the test override installed by SetTestVersion.
func ClearTestVersion() {
	testOverride.Store(nil)
}
