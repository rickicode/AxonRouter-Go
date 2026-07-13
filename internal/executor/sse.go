package executor

import "bytes"

// IsSSEDataLine checks if a line is an SSE data line (starts with "data:").
func IsSSEDataLine(line []byte) bool {
	return bytes.HasPrefix(line, []byte("data:"))
}

// ParseSSEDataLine extracts the payload from an SSE data line.
// Returns nil for non-data lines and for the [DONE] marker.
func ParseSSEDataLine(line []byte) []byte {
	if !bytes.HasPrefix(line, []byte("data:")) {
		return nil
	}
	content := bytes.TrimPrefix(line, []byte("data:"))
	content = bytes.TrimPrefix(content, []byte(" "))
	if string(content) == "[DONE]" {
		return nil
	}
	return content
}
