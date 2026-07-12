package admin

import (
	"bytes"
	"net/http"

	"github.com/gin-gonic/gin"
)

// programmaticWriter captures the response body so non-standard admin responses
// (e.g. `{"ok": true}`) can be normalized to `{"data": {"ok": true}}` for the
// programmatic admin API without changing the dashboard endpoints.
type programmaticWriter struct {
	gin.ResponseWriter
	body   *bytes.Buffer
	status int
}

func newProgrammaticWriter(w gin.ResponseWriter) *programmaticWriter {
	return &programmaticWriter{
		ResponseWriter: w,
		body:           &bytes.Buffer{},
		status:         http.StatusOK,
	}
}

func (w *programmaticWriter) WriteHeader(status int) {
	w.status = status
}

func (w *programmaticWriter) Write(b []byte) (int, error) {
	return w.body.Write(b)
}

func (w *programmaticWriter) WriteString(s string) (int, error) {
	return w.body.WriteString(s)
}

func (w *programmaticWriter) Flush() {
	// Capture only; real flush happens after normalization.
}

// ProgrammaticResponseWrapper normalizes legacy `{"ok": true}` admin responses
// into the standard `{"data": ...}` envelope used by the programmatic API.
func ProgrammaticResponseWrapper() gin.HandlerFunc {
	return func(c *gin.Context) {
		pw := newProgrammaticWriter(c.Writer)
		c.Writer = pw
		defer func() {
			body := pw.body.Bytes()
			if pw.status == http.StatusOK && bytes.HasPrefix(body, []byte(`{"ok"`)) {
				out := make([]byte, 0, len(body)+10)
				out = append(out, []byte(`{"data":`)...)
				out = append(out, body...)
				out = append(out, '}')
				body = out
			}
			pw.ResponseWriter.WriteHeader(pw.status)
			pw.ResponseWriter.Write(body)
		}()
		c.Next()
	}
}
