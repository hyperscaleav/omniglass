package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hyperscaleav/omniglass/internal/platformmetric"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// timedRouter is the API's shape in small: a sub-router under /api/v1 with
// the request recorder mounted on it, a route with an id in it, and routes
// answering each status class.
func timedRouter(rec *platformmetric.Recorder, now func() time.Time) http.Handler {
	r := chi.NewRouter()
	r.Route("/api/v1", func(sub chi.Router) {
		sub.Use(recordRequests(rec, now))
		sub.Get("/components/{id}", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
		sub.Get("/missing/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
		sub.Post("/broken", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
		sub.Get("/quiet", func(http.ResponseWriter, *http.Request) {})
	})
	return r
}

// seriesOf indexes a flush by key, then instance.
func seriesOf(rows []storage.PlatformMetric) map[string]map[string]float64 {
	out := map[string]map[string]float64{}
	for _, r := range rows {
		if out[r.Key] == nil {
			out[r.Key] = map[string]float64{}
		}
		out[r.Key][r.Instance] = r.Value
	}
	return out
}

// TestRecordRequestsKeysOnTheRouteTemplate: two requests for different ids
// on one route share one instance, the route template's, never the raw path;
// the status is its class; and a path no route matches is one `unmatched`
// instance however many paths miss.
func TestRecordRequestsKeysOnTheRouteTemplate(t *testing.T) {
	t.Parallel()
	rec := platformmetric.NewRecorder(time.Now)
	h := timedRouter(rec, time.Now)
	for _, req := range []struct{ method, path string }{
		{"GET", "/api/v1/components/7f3c"},
		{"GET", "/api/v1/components/9a1b"},
		{"GET", "/api/v1/missing/1"},
		{"POST", "/api/v1/broken"},
		{"GET", "/api/v1/quiet"},
		{"GET", "/api/v1/no/such/route"},
		{"GET", "/api/v1/another-miss"},
		{"PUT", "/api/v1/broken"},
	} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(req.method, req.path, nil))
	}
	got := seriesOf(rec.Flush("server-1"))
	wantCount := map[string]float64{
		"GET /api/v1/components/{id} 2xx": 2,
		"GET /api/v1/missing/{id} 4xx":    1,
		"POST /api/v1/broken 5xx":         1,
		"GET /api/v1/quiet 2xx":           1,
		"unmatched":                       3,
	}
	counts := got[writerclass.HTTPRequestCount]
	if len(counts) != len(wantCount) {
		t.Fatalf("http-request-count instances = %v, want %v", counts, wantCount)
	}
	for inst, n := range wantCount {
		if counts[inst] != n {
			t.Errorf("http-request-count[%q] = %v, want %v", inst, counts[inst], n)
		}
	}
	for _, key := range []string{writerclass.HTTPRequestDuration, writerclass.HTTPRequestDurationMax} {
		if len(got[key]) != len(wantCount) {
			t.Errorf("%s instances = %v, want one per count instance", key, got[key])
		}
	}
}

// TestRecordRequestsTimesEachRequest: the window's mean and max are the
// requests' own durations in milliseconds, read off the injected clock.
func TestRecordRequestsTimesEachRequest(t *testing.T) {
	t.Parallel()
	steps := []time.Duration{10 * time.Millisecond, 30 * time.Millisecond}
	at := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	calls := 0
	now := func() time.Time {
		// Each request reads the clock twice: at entry and at exit.
		if calls%2 == 1 {
			at = at.Add(steps[calls/2])
		}
		calls++
		return at
	}
	rec := platformmetric.NewRecorder(time.Now)
	h := timedRouter(rec, now)
	for range steps {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/components/x", nil))
	}
	got := seriesOf(rec.Flush("server-1"))
	inst := "GET /api/v1/components/{id} 2xx"
	if v := got[writerclass.HTTPRequestDuration][inst]; v != 20 {
		t.Errorf("mean = %v ms, want 20", v)
	}
	if v := got[writerclass.HTTPRequestDurationMax][inst]; v != 30 {
		t.Errorf("max = %v ms, want 30", v)
	}
}

// TestRequestInstanceBoundsTheMethod: a method outside HTTP's standard set
// reads OTHER, so a caller inventing verbs cannot grow the series.
func TestRequestInstanceBoundsTheMethod(t *testing.T) {
	t.Parallel()
	if got := requestInstance("BREW", "/api/v1/pot", 418); got != "OTHER /api/v1/pot 4xx" {
		t.Errorf("requestInstance = %q", got)
	}
	if got := requestInstance("PATCH", "/api/v1/x/{id}", 204); got != "PATCH /api/v1/x/{id} 2xx" {
		t.Errorf("requestInstance = %q", got)
	}
}

// TestStatusWriterKeepsTheResponseController: the wrapped writer still
// reaches the connection's deadlines and flushes, which the webhook receiver
// sets through http.ResponseController.
func TestStatusWriterKeepsTheResponseController(t *testing.T) {
	t.Parallel()
	rw := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rw}
	if err := http.NewResponseController(sw).Flush(); err != nil {
		t.Fatalf("flush through the status writer: %v", err)
	}
	if !rw.Flushed {
		t.Error("the flush did not reach the underlying writer")
	}
	if sw.status() != http.StatusOK {
		t.Errorf("an unwritten response's status = %d, want 200", sw.status())
	}
}
