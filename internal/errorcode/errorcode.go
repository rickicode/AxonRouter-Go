// Package errorcode extracts HTTP-like status codes from internal error text
// so that log rows, badges, and filters can show the real upstream status.
package errorcode

import (
	"regexp"
	"strconv"
)

var streamErrorCodeRe = regexp.MustCompile(`^stream error (\d+):`)

// FromString returns a status code embedded in s, or 0 if none is found.
// It recognises the common "stream error N:" prefix produced by upstream
// response translators when a streaming request fails mid-stream.
func FromString(s string) int {
	if m := streamErrorCodeRe.FindStringSubmatch(s); len(m) == 2 {
		if code, err := strconv.Atoi(m[1]); err == nil {
			return code
		}
	}
	return 0
}

// FromError is a convenience wrapper around FromString.
func FromError(err error) int {
	if err == nil {
		return 0
	}
	return FromString(err.Error())
}
