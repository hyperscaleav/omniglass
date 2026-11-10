package api_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/platformmetric"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// lockedBuffer is a log buffer the server's handlers write at once.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestFleetViewLogsItsCause: a fleet view the database cannot answer is an
// opaque 500 to its caller and its cause in the server's log, where an
// operator reading the log after the fact can find it (#1627).
func TestFleetViewLogsItsCause(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := seededDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	tok := bootstrapOwnerTok(t, ctx, gw)
	var logs lockedBuffer
	srv := httptest.NewServer(api.NewHandler(gw, api.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil)))))
	t.Cleanup(srv.Close)
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	c.do(tok, http.MethodGet, "/views/fleet", nil, http.StatusOK)

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	if _, err := conn.Exec(ctx, `alter table system_member rename to system_member_gone`); err != nil {
		t.Fatalf("break the fleet read: %v", err)
	}

	body := c.do(tok, http.MethodGet, "/views/fleet", nil, http.StatusInternalServerError)
	if strings.Contains(string(body), "system_member") {
		t.Errorf("the 500 leaked its cause to the caller: %s", body)
	}
	got := logs.String()
	if !strings.Contains(got, `"msg":"read fleet view"`) || !strings.Contains(got, "system_member") {
		t.Errorf("server log = %s, want the fleet read's failure and its cause", got)
	}
}

// TestServerRequestMetricsLandOnTheServerComponent: after a window of API
// calls, the server's own component holds http-request-count,
// http-request-duration and http-request-duration-max samples, one per (key,
// instance) per window, each instance a route template: two components read
// by name share one instance, and a path no route takes is `unmatched`.
func TestServerRequestMetricsLandOnTheServerComponent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := seededDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	tok := bootstrapOwnerTok(t, ctx, gw)
	serverID, err := gw.EnsureServerComponent(ctx, "e2e-server")
	if err != nil {
		t.Fatalf("ensure the server component: %v", err)
	}
	rec := platformmetric.NewRecorder(time.Now)
	srv := httptest.NewServer(api.NewHandler(gw, api.WithRequestRecorder(rec)))
	t.Cleanup(srv.Close)
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	for _, name := range []string{"codec-a", "codec-b"} {
		c.do(tok, http.MethodPost, "/components", map[string]any{"name": name, "product": "generic-device"}, http.StatusCreated)
	}
	// The window starts clean of the creates, so the reads below are all it
	// counts.
	if err := platformmetric.NewReporter(rec, gw, serverID, nil, nil).Report(ctx); err != nil {
		t.Fatalf("report the setup window: %v", err)
	}
	c.do(tok, http.MethodGet, "/components/codec-a", nil, http.StatusOK)
	c.do(tok, http.MethodGet, "/components/codec-b", nil, http.StatusOK)
	c.do(tok, http.MethodGet, "/components/no-such-codec", nil, http.StatusNotFound)
	c.do(tok, http.MethodGet, "/no/such/route", nil, http.StatusNotFound)
	// A sample's ts is stored to the millisecond, so the window's bound is
	// taken a millisecond back; the setup window flushed four requests ago.
	before := time.Now().Add(-time.Millisecond).Truncate(time.Millisecond)
	if err := platformmetric.NewReporter(rec, gw, serverID, nil, nil).Report(ctx); err != nil {
		t.Fatalf("report the window: %v", err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	rows, err := conn.Query(ctx, `
		select t.name, s.instance, count(*), max(m.value)
		from metric m
		join metric_series s on s.id = m.series_id
		join metric_type t on t.id = s.metric_type_id
		where s.owner_kind = 'component' and s.owner_id = $1::uuid and m.ts >= $2
		group by 1, 2`, serverID, before)
	if err != nil {
		t.Fatalf("read the window: %v", err)
	}
	type series struct{ key, instance string }
	got := map[series]float64{}
	for rows.Next() {
		var s series
		var n int
		var v float64
		if err := rows.Scan(&s.key, &s.instance, &n, &v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if n != 1 {
			t.Errorf("%s %q has %d samples in one window, want 1", s.key, s.instance, n)
		}
		got[s] = v
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	wantCount := map[string]float64{
		"GET /api/v1/components/{name} 2xx": 2,
		"GET /api/v1/components/{name} 4xx": 1,
		"unmatched":                         1,
	}
	for inst, n := range wantCount {
		if v, ok := got[series{writerclass.HTTPRequestCount, inst}]; !ok || v != n {
			t.Errorf("http-request-count %q = %v (present %v), want %v", inst, v, ok, n)
		}
		for _, key := range []string{writerclass.HTTPRequestDuration, writerclass.HTTPRequestDurationMax} {
			if v, ok := got[series{key, inst}]; !ok || v <= 0 {
				t.Errorf("%s %q = %v (present %v), want a positive duration", key, inst, v, ok)
			}
		}
	}
	for s := range got {
		if strings.Contains(s.instance, "codec-") {
			t.Errorf("an instance carries a raw path: %s %q", s.key, s.instance)
		}
	}
}
