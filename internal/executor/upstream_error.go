package executor

import (
	"fmt"
	"net/http"

	"github.com/rickicode/AxonRouter-Go/internal/executor/translator"
)

// UpstreamError carries a translated upstream error response so the handler can
// return it to the client with the original HTTP status and provider body.
type UpstreamError struct {
	StatusCode int
	Body       []byte      // provider/translated error body (usually JSON)
	RawBody    []byte      // original raw body before translation (for logging)
	Headers    http.Header // upstream response headers (e.g. Retry-After)
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("upstream error %d: %s", e.StatusCode, string(e.Body))
}

// IsClientError reports whether the upstream error is a 4xx client error.
func (e *UpstreamError) IsClientError() bool {
	return e.StatusCode >= 400 && e.StatusCode < 500
}

// TranslateErrorBody runs the provider-specific translator (if any) and assigns
// the translated body to Body. When no translator is registered the body stays
// as the raw upstream response (default passthrough for OpenAI-format providers).
func (e *UpstreamError) TranslateErrorBody(providerPrefix string) {
	if e == nil {
		return
	}
	if translated := translator.Translate(providerPrefix, e.StatusCode, e.RawBody); translated != nil {
		e.Body = translated
	}
}
