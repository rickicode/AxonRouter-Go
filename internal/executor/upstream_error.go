package executor

import "fmt"

// UpstreamError carries a translated upstream error response so the handler can
// return it to the client with the original HTTP status and provider body.
type UpstreamError struct {
	StatusCode int
	Body       []byte // provider/translated error body (usually JSON)
	RawBody    []byte // original raw body before translation (for logging)
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("upstream error %d: %s", e.StatusCode, string(e.Body))
}

// IsClientError reports whether the upstream error is a 4xx client error.
func (e *UpstreamError) IsClientError() bool {
	return e.StatusCode >= 400 && e.StatusCode < 500
}
