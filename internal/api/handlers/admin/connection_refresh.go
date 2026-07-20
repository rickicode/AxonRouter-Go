package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
)

// isRefreshableTestError reports whether a failed connection test should be
// retried after refreshing the OAuth token.
func isRefreshableTestError(providerID string, err error, det connstate.ErrorDetection) bool {
	if det.Category == connstate.ErrorAuth {
		return true
	}

	var upErr *executor.UpstreamError
	if !errors.As(err, &upErr) {
		return false
	}

	switch upErr.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return true
	case http.StatusBadRequest:
		if providerID == "kiro" {
			body := strings.ToLower(string(upErr.Body))
			if body == "" {
				body = strings.ToLower(string(upErr.RawBody))
			}
			return strings.Contains(body, "improperly formed request") || strings.Contains(body, "request_body_invalid")
		}
	}

	return false
}
