package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hyperscaleav/omniglass/internal/platformmetric"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// unmatchedInstance is the one instance every request no route matched
// records under, so a scan of made-up paths costs one series, not one each.
const unmatchedInstance = "unmatched"

// standardMethods are the methods an instance names as themselves; any other
// reads OTHER, so a caller inventing verbs cannot grow the series either.
var standardMethods = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodPost: true, http.MethodPut: true,
	http.MethodPatch: true, http.MethodDelete: true, http.MethodOptions: true,
}

// recordRequests times every API request into rec (#1627): one count, one
// mean and one max per instance per window. The instance is the route
// template chi matched (`GET /api/v1/components/{id} 2xx`), read after the
// router has run, never the raw path, so the series are bounded by the route
// table however many ids are asked for.
func recordRequests(rec *platformmetric.Recorder, now func() time.Time) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := now()
			rctx := chi.RouteContext(r.Context())
			patterns := 0
			if rctx != nil {
				patterns = len(rctx.RoutePatterns)
			}
			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r)
			ms := platformmetric.Millis(now().Sub(start))
			instance := unmatchedInstance
			// A route matched below this middleware appends its pattern; a
			// request no route took (a 404 or 405 from the router itself)
			// leaves the list as it found it.
			if rctx != nil && len(rctx.RoutePatterns) > patterns {
				instance = requestInstance(r.Method, rctx.RoutePattern(), sw.status())
			}
			rec.Count(writerclass.HTTPRequestCount, instance, 1)
			rec.Mean(writerclass.HTTPRequestDuration, instance, ms)
			rec.Max(writerclass.HTTPRequestDurationMax, instance, ms)
		})
	}
}

// requestInstance is a matched request's instance: its method, bounded to
// the standard set, its route template and its status class.
func requestInstance(method, pattern string, status int) string {
	if !standardMethods[method] {
		method = "OTHER"
	}
	return fmt.Sprintf("%s %s %dxx", method, pattern, status/100)
}

// statusWriter keeps the status a handler answered. Unwrap and Flush keep
// http.ResponseController and a streaming handler working through it.
type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.code == 0 {
		w.code = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Flush flushes the underlying writer when it can.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap is the underlying writer, for http.ResponseController.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// status is the status the response went out with: 200 when the handler
// wrote nothing at all, as net/http sends.
func (w *statusWriter) status() int {
	if w.code == 0 {
		return http.StatusOK
	}
	return w.code
}
