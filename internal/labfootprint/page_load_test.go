package labfootprint

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The system page figures are pure over the loads: a failed load counts as
// an error and its time stays out of the quantiles, and the error ratio is
// the share of loads that failed, so 0 reads as healthy.
func TestSummarizePage(t *testing.T) {
	got := SummarizePage(PageRun{Clients: 4, Loads: []PageLoad{
		{Took: 10 * time.Millisecond, Bytes: 1000},
		{Took: 30 * time.Millisecond, Bytes: 3000},
		{Took: 5 * time.Second, Bytes: 10, Err: errors.New("GET /api/v1/systems/x/roles: 503 Service Unavailable")},
		{Took: 20 * time.Millisecond, Bytes: 2000},
	}})
	if got == nil {
		t.Fatal("four loads summarized to nothing")
	}
	want := SystemPageFigures{
		Ms:         Quantiles{P50: 20, P95: 30, P99: 30, N: 3},
		Bytes:      2000,
		Clients:    4,
		Errors:     1,
		ErrorRatio: 0.25,
	}
	if *got != want {
		t.Errorf("summary = %+v, want %+v", *got, want)
	}
	if SummarizePage(PageRun{}) != nil {
		t.Error("no clients summarized to figures; want none, so the report leaves them out")
	}
	all := SummarizePage(PageRun{Clients: 1, Loads: []PageLoad{{Err: errors.New("x")}}})
	if all.Ms.N != 0 || all.ErrorRatio != 1 || all.Bytes != 0 {
		t.Errorf("every load failed: %+v, want no quantiles, ratio 1, no bytes", *all)
	}
}

// A report with no page clients leaves the system page fields out rather
// than reporting zero, and its table reads not measured.
func TestSystemPageAbsentWhenSkipped(t *testing.T) {
	rep := SummarizeScale(ScaleRun{})
	b, _ := json.Marshal(rep)
	if strings.Contains(string(b), "system_page") {
		t.Errorf("a run with no page clients reports system page fields: %s", b)
	}
	if !strings.Contains(rep.Table(), "system page p50              not measured") {
		t.Errorf("table has no not-measured system page row:\n%s", rep.Table())
	}
	measured := fullReport()
	for _, row := range []string{"system page p50              4.0 ms", "system page p95              4.0 ms   (1 loads, 2 clients, 1 failed, 100 B a load)"} {
		if !strings.Contains(measured.Table(), row) {
			t.Errorf("table lacks %q:\n%s", row, measured.Table())
		}
	}
}

// pageServer answers the system page's reads as a server would, recording
// each path asked, and answers the reads in fail with a 503.
func pageServer(t *testing.T, fail string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.RequestURI())
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if fail != "" && strings.HasPrefix(r.URL.Path, fail) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path == "/api/v1/systems/sys-1/metrics" {
			_, _ = w.Write([]byte(`{"metrics":[{"metric_type_name":"cpu-load","is_sampled":true,"from_contract":false,"instance":"core 0"},` +
				`{"metric_type_name":"uptime","is_sampled":false,"from_contract":true,"instance":""},` +
				`{"metric_type_name":"noise","is_sampled":false,"from_contract":false,"instance":""}]}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Sorted(slices.Values(asked))
	}
}

// One load makes the page's reads, the metrics read's series among them for
// the metrics the page charts (from the contract, or sampled with its
// instance), and sums the bytes it read.
func TestLoadSystemPage(t *testing.T) {
	srv, asked := pageServer(t, "")
	h := &harness{http: srv.Client(), env: map[string]string{"LAB_TOKEN": "tok"}}
	got := h.loadSystemPage(context.Background(), srv.URL, "sys-1")
	if got.Err != nil {
		t.Fatal(got.Err)
	}
	var want []string
	for _, r := range SystemPageReads {
		if strings.Contains(r, "{metric}") {
			continue
		}
		want = append(want, "/api/v1"+strings.ReplaceAll(r, "{system}", "sys-1"))
	}
	want = append(want,
		"/api/v1/systems/sys-1/metrics/cpu-load/samples?hours=24&resolution=300&instance=core+0",
		"/api/v1/systems/sys-1/metrics/uptime/samples?hours=24&resolution=300")
	slices.Sort(want)
	if g := asked(); !slices.Equal(g, want) {
		t.Errorf("a load read\n  %s\nwant\n  %s", strings.Join(g, "\n  "), strings.Join(want, "\n  "))
	}
	if got.Took <= 0 || got.Bytes < int64(len(want)*2) {
		t.Errorf("load took %s and read %d bytes", got.Took, got.Bytes)
	}
}

// One non-2xx read fails the whole load, naming the read.
func TestLoadSystemPageFailsOnANon2xx(t *testing.T) {
	srv, _ := pageServer(t, "/api/v1/systems/sys-1/roles")
	h := &harness{http: srv.Client(), env: map[string]string{"LAB_TOKEN": "tok"}}
	got := h.loadSystemPage(context.Background(), srv.URL, "sys-1")
	if got.Err == nil || !strings.Contains(got.Err.Error(), "/systems/sys-1/roles") || !strings.Contains(got.Err.Error(), "503") {
		t.Errorf("a load with a 503 read: err = %v, want the read and its status", got.Err)
	}
}

// The reads the console's system page makes on a cold open are a generated
// fact the page's own test checks (SystemZoom.test.tsx); the committed render
// equals a fresh one.
func TestSystemPageReadsMatchCommitted(t *testing.T) {
	committed, err := os.ReadFile("../../web/src/lib/system_page_reads.gen.ts")
	if err != nil {
		t.Fatalf("read committed system_page_reads.gen.ts (run make gen and commit it): %v", err)
	}
	if string(committed) != string(SystemPageReadsTS()) {
		t.Error("web/src/lib/system_page_reads.gen.ts drifted from labfootprint.SystemPageReads: run make gen and commit the result")
	}
}

// A request refused with a 401 mints a fresh token and retries once: a long
// sampling run outlives nothing it signed in with. Concurrent refusals of
// the same token mint once.
func TestGetReMintsTheTokenOnce(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer new" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	var mints atomic.Int32
	h := &harness{http: srv.Client(), tokens: newTokenSource("old", func(context.Context) (string, error) {
		mints.Add(1)
		return "new", nil
	})}
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var v struct{ OK bool }
			errs[i] = h.get(context.Background(), srv.URL+"/x", &v)
			if errs[i] == nil && !v.OK {
				errs[i] = errors.New("decoded nothing")
			}
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("read %d after a re-mint: %v", i, err)
		}
	}
	if n := mints.Load(); n != 1 {
		t.Errorf("minted %d tokens for one expiry, want 1", n)
	}

	refuse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer refuse.Close()
	mints.Store(0)
	h = &harness{http: refuse.Client(), tokens: newTokenSource("old", func(context.Context) (string, error) {
		return "new" + string(rune('0'+mints.Add(1))), nil
	})}
	err := h.get(context.Background(), refuse.URL+"/x", nil)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("a server refusing every token: err = %v, want the 401", err)
	}
	if n := mints.Load(); n != 1 {
		t.Errorf("minted %d tokens against a server refusing them all, want one retry", n)
	}
}

// The page system is the one named, or the fleet view's first; a name the
// fleet lacks, or a fleet with no systems, is refused before the window.
func TestPickPageSystem(t *testing.T) {
	fleet := []byte(`{"locations":[],"systems":[{"id":"u-1","name":"room-1"},{"id":"u-2","name":"room-2"}]}`)
	for name, want := range map[string]string{"": "u-1", "room-2": "u-2"} {
		got, err := PickPageSystem(fleet, name)
		if err != nil || got != want {
			t.Errorf("--page-system %q = %q, %v; want %q", name, got, err, want)
		}
	}
	if _, err := PickPageSystem(fleet, "room-9"); err == nil || !strings.Contains(err.Error(), `--page-system "room-9"`) {
		t.Errorf("an unknown system: err = %v, want one naming the flag", err)
	}
	if _, err := PickPageSystem([]byte(`{"systems":[]}`), ""); err == nil {
		t.Error("a fleet with no systems picked one")
	}
}

// A window that fails partway leaves no page client loading after it
// returns: in a sampling run the next sample's window would otherwise share
// the server with the failed one's leftover loads.
func TestLoadWindowStopsThePageClientsOnAFailedRead(t *testing.T) {
	srv, asked := pageServer(t, "/api/v1/views/fleet")
	h := &harness{cfg: Config{Out: io.Discard}, http: srv.Client(), env: map[string]string{"LAB_TOKEN": "tok", "LAB_HOST_URL": srv.URL}}
	cfg := ScaleConfig{Window: 600 * time.Millisecond, FleetReads: 30, PageClients: 3, PageLoads: 6}
	var run ScaleRun
	if err := h.loadWindow(context.Background(), cfg, "sys-1", &run); err == nil || !strings.Contains(err.Error(), "fleet view") {
		t.Fatalf("err = %v, want the failed fleet view read", err)
	}
	at := len(asked())
	time.Sleep(300 * time.Millisecond)
	if after := len(asked()); after != at {
		t.Errorf("%d reads arrived after the failed window returned, want none", after-at)
	}
}
