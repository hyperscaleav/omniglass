package labfootprint

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// loadWindow takes the page clients' result and then waits for them again in its
// defer, so the wait loadPages returns must answer every time. It read a channel
// once, so the second wait blocked forever: every lab measure with page clients
// hung after its first window, and the lab-measure-series demo timed out (#2019).
func TestLoadPagesWaitAnswersEveryTime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	h := &harness{cfg: Config{Out: io.Discard}, env: map[string]string{"LAB_HOST_URL": srv.URL, "LAB_TOKEN": "t"}, http: srv.Client()}

	wait := h.loadPages(context.Background(), ScaleConfig{PageClients: 1, PageLoads: 1, Window: 50 * time.Millisecond}, "sys")
	first := wait()
	second := make(chan PageRun, 1)
	go func() { second <- wait() }()
	select {
	case got := <-second:
		if got.Clients != first.Clients || len(got.Loads) != len(first.Loads) {
			t.Fatalf("the second wait gave %+v, the first %+v", got, first)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the second wait blocked: loadWindow's deferred wait would hang the measure")
	}
}
