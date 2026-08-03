package api_test

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// The watch e2e's pacing, all derived from the two configured intervals so a
// slow box cannot fail a presence assertion: awaits are generous multiples,
// while the absence window spans enough detector ticks that a wrong
// implementation notifies inside it.
const (
	watchDetector  = 50 * time.Millisecond
	watchHeartbeat = 150 * time.Millisecond
	watchAwait     = 100 * watchDetector // 5s
	watchQuiet     = 10 * watchDetector  // 500ms, ten detector passes
)

// sseLine is one line of a live SSE stream: an event field, a data field, a
// comment (the heartbeat), or the retry hint.
type sseLine struct {
	kind string // "event", "data", "comment", "retry"
	text string
}

// sseClient opens a watch stream and pumps its lines into a channel, so the
// test can await events with deadlines while the connection stays open.
type sseClient struct {
	resp  *http.Response
	lines chan sseLine
}

func openWatch(t *testing.T, base, tok, name string) *sseClient {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+"/api/v1/views/"+name+":watch", nil)
	if err != nil {
		t.Fatalf("build watch request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open watch: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("watch status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		resp.Body.Close()
		t.Fatalf("watch content-type = %q, want text/event-stream", ct)
	}
	c := &sseClient{resp: resp, lines: make(chan sseLine, 64)}
	go func() {
		defer close(c.lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, ":"):
				c.lines <- sseLine{kind: "comment", text: line}
			case strings.HasPrefix(line, "event:"):
				c.lines <- sseLine{kind: "event", text: strings.TrimSpace(strings.TrimPrefix(line, "event:"))}
			case strings.HasPrefix(line, "data:"):
				c.lines <- sseLine{kind: "data", text: strings.TrimSpace(strings.TrimPrefix(line, "data:"))}
			case strings.HasPrefix(line, "retry:"):
				c.lines <- sseLine{kind: "retry", text: strings.TrimSpace(strings.TrimPrefix(line, "retry:"))}
			}
		}
	}()
	return c
}

func (c *sseClient) close() { c.resp.Body.Close() }

// awaitRetryHint asserts the stream opens with the reconnect hint, the frame
// an EventSource client reads to pace its retry.
func (c *sseClient) awaitRetryHint(t *testing.T, want string) {
	t.Helper()
	select {
	case l, ok := <-c.lines:
		if !ok {
			t.Fatalf("stream closed before the retry hint")
		}
		if l.kind != "retry" || l.text != want {
			t.Fatalf("first frame = %+v, want the retry hint %s", l, want)
		}
	case <-time.After(watchAwait):
		t.Fatalf("no retry hint within %v", watchAwait)
	}
}

// awaitClose asserts the stream ends (the server closed it) within the window.
func (c *sseClient) awaitClose(t *testing.T, within time.Duration) {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case _, ok := <-c.lines:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatalf("stream still open after %v", within)
		}
	}
}

// awaitEvent waits for the next event line of the given name, draining
// heartbeats and data lines, failing after the deadline.
func (c *sseClient) awaitEvent(t *testing.T, name string, within time.Duration) {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case l, ok := <-c.lines:
			if !ok {
				t.Fatalf("stream closed while awaiting %q", name)
			}
			if l.kind == "event" && l.text == name {
				return
			}
		case <-deadline:
			t.Fatalf("no %q event within %v", name, within)
		}
	}
}

// awaitComment waits for a heartbeat comment. Presence and absence get their
// own windows deliberately: sharing one would couple a "no change events"
// assertion to heartbeat timing.
func (c *sseClient) awaitComment(t *testing.T, within time.Duration) {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case l, ok := <-c.lines:
			if !ok {
				t.Fatalf("stream closed while awaiting a heartbeat")
			}
			if l.kind == "comment" {
				return
			}
		case <-deadline:
			t.Fatalf("no heartbeat comment within %v", within)
		}
	}
}

// quietWindow reads for the given window asserting no event of the given name
// arrives; heartbeats and other frames are drained.
func (c *sseClient) quietWindow(t *testing.T, name string, window time.Duration) {
	t.Helper()
	deadline := time.After(window)
	for {
		select {
		case l, ok := <-c.lines:
			if !ok {
				t.Fatalf("stream closed during the quiet window")
			}
			if l.kind == "event" && l.text == name {
				t.Fatalf("unexpected %q event during a quiet window", name)
			}
		case <-deadline:
			return
		}
	}
}

// TestViewsWatchAPI drives the live seam end to end: a watch delivers the
// connect baseline, then a change notification within a detector interval of
// a data change; a quiet window carries heartbeats only; the stream is scoped
// (an out-of-scope change never notifies a scoped watcher); authorization
// matches :run exactly; and a reconnect resumes with a fresh baseline.
// Skipped under -short.
func TestViewsWatchAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	all := scope.Set{All: true}
	for _, name := range []string{"disp-1", "cam-1"} {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name}, all); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `insert into interface (name, type, component, params) values
		('disp-1-icmp', (select id from interface_type where name = 'icmp'), (select id from component where name = 'disp-1'), '{"target":"10.0.0.1"}'::jsonb),
		('cam-1-icmp',  (select id from interface_type where name = 'icmp'), (select id from component where name = 'cam-1'),  '{"target":"10.0.0.2"}'::jsonb)`); err != nil {
		t.Fatalf("insert interfaces: %v", err)
	}
	// A views-only role for the authz-parity leg, inserted before the first
	// request so the cached role index carries it.
	if _, err := conn.Exec(ctx,
		`insert into role (name, official, permissions, inherits) values ('views-only', false, '{"view:read"}', '{}')`); err != nil {
		t.Fatalf("insert views-only role: %v", err)
	}
	verdict := func(owner, instance, value string) {
		t.Helper()
		if err := gw.InsertStateSamples(ctx, []storage.StateSampleWrite{
			{OwnerKind: "component", OwnerID: owner, Key: "interface.reachable", Instance: instance, Value: value, Source: "icmp", TS: time.Now().UTC()},
		}); err != nil {
			t.Fatalf("insert verdict: %v", err)
		}
	}
	verdict("disp-1", "disp-1-icmp", "up")

	// Tight intervals so the e2e stays fast.
	srv := httptest.NewServer(api.NewHandler(gw, api.WithWatchIntervals(watchDetector, watchHeartbeat)))
	defer srv.Close()

	// The stream opens with the reconnect hint, then the connect baseline
	// arrives without any data change, so a client that missed changes while
	// disconnected always refetches once.
	c := openWatch(t, srv.URL, ownerTok, "component-reachability")
	defer c.close()
	c.awaitRetryHint(t, "3000")
	c.awaitEvent(t, "change", watchAwait)

	// A data change notifies within a detector interval or two.
	verdict("disp-1", "disp-1-icmp", "down")
	c.awaitEvent(t, "change", watchAwait)

	// A quiet window carries no change events, and a quiet stream is kept
	// alive by heartbeat comments.
	c.quietWindow(t, "change", watchQuiet)
	c.awaitComment(t, watchAwait)

	// A watcher scoped to cam-1 is never notified of disp-1's changes: after
	// its baseline, an out-of-scope flip stays silent; an in-scope flip
	// notifies.
	var camID string
	if err := conn.QueryRow(ctx, `select id from component where name = 'cam-1'`).Scan(&camID); err != nil {
		t.Fatalf("cam id: %v", err)
	}
	viewerTok := setupScopedViewer(t, ctx, dsn, "viewer-cam-watch", "viewer", "component", camID)
	sc := openWatch(t, srv.URL, viewerTok, "component-reachability")
	defer sc.close()
	sc.awaitEvent(t, "change", watchAwait)
	verdict("disp-1", "disp-1-icmp", "up")
	sc.quietWindow(t, "change", watchQuiet)
	verdict("cam-1", "cam-1-icmp", "up")
	sc.awaitEvent(t, "change", watchAwait)

	// Authorization matches :run: view:read alone opens the directory but not
	// a watch on a view whose declared permission the caller lacks.
	viewsOnlyTok := principalWithGrants(t, ctx, dsn, "views-only-watch", []grant{{role: "views-only", scopeKind: "all"}})
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/views/component-reachability:watch", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+viewsOnlyTok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("watch as views-only: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("watch without the declared permission = %d, want 403", resp.StatusCode)
	}
	// An unknown view is a 404, as on :run.
	req, err = http.NewRequest(http.MethodGet, srv.URL+"/api/v1/views/nope:watch", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+ownerTok)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("watch unknown view: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("watch of an unknown view = %d, want 404", resp.StatusCode)
	}

	// A reconnect resumes cleanly: a fresh stream begins with its own
	// baseline, so nothing missed while disconnected is lost.
	c.close()
	c2 := openWatch(t, srv.URL, ownerTok, "component-reachability")
	defer c2.close()
	c2.awaitEvent(t, "change", watchAwait)
}

// watchFixture stands up a seeded gateway with one probed component interface
// and an owner token, the common ground of the stream-lifecycle tests.
func watchFixture(t *testing.T) (storage.Gateway, string) {
	t.Helper()
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, scope.Set{All: true}); err != nil {
		t.Fatalf("create component: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `insert into interface (name, type, component, params) values
		('disp-1-icmp', (select id from interface_type where name = 'icmp'), (select id from component where name = 'disp-1'), '{"target":"10.0.0.1"}'::jsonb)`); err != nil {
		t.Fatalf("insert interface: %v", err)
	}
	return gw, tok
}

// TestViewsWatchGracefulShutdown pins the shutdown contract: a streaming
// response never goes idle, so without the stream-shutdown signal
// http.Server.Shutdown waits out its whole deadline and returns a timeout,
// which would turn every rolling restart into a stall and a nonzero exit while
// one console is watching. The control leg proves the deadline really does
// expire without the signal, so the fix cannot be deleted silently.
func TestViewsWatchGracefulShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	gw, ownerTok := watchFixture(t)

	serve := func(t *testing.T, endStreams chan struct{}) (*http.Server, string) {
		t.Helper()
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		srv := &http.Server{
			Handler:           api.NewHandler(gw, api.WithWatchIntervals(watchDetector, watchHeartbeat), api.WithStreamShutdown(endStreams)),
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() { _ = srv.Serve(ln) }()
		return srv, "http://" + ln.Addr().String()
	}

	t.Run("signalled", func(t *testing.T) {
		endStreams := make(chan struct{})
		srv, base := serve(t, endStreams)
		c := openWatch(t, base, ownerTok, "component-reachability")
		defer c.close()
		c.awaitEvent(t, "change", watchAwait)

		close(endStreams)
		ctx, cancel := context.WithTimeout(context.Background(), watchAwait)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			t.Fatalf("shutdown with a watcher connected = %v, want a clean nil", err)
		}
	})

	t.Run("unsignalled control", func(t *testing.T) {
		srv, base := serve(t, nil)
		c := openWatch(t, base, ownerTok, "component-reachability")
		defer c.close()
		c.awaitEvent(t, "change", watchAwait)

		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		if err := srv.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("shutdown without the signal = %v, want the deadline to expire (the regression this guards)", err)
		}
		_ = srv.Close()
	})
}

// TestViewsWatchLifetimeCap pins the bounded stream: every connection ends at
// the cap so the client reconnects through the full admission path, which is
// what stops a revoked grant or a narrowed scope from outliving a connection.
func TestViewsWatchLifetimeCap(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	gw, ownerTok := watchFixture(t)
	srv := httptest.NewServer(api.NewHandler(gw,
		api.WithWatchIntervals(watchDetector, watchHeartbeat),
		api.WithWatchLifetime(300*time.Millisecond)))
	defer srv.Close()

	c := openWatch(t, srv.URL, ownerTok, "component-reachability")
	defer c.close()
	c.awaitEvent(t, "change", watchAwait)
	c.awaitClose(t, watchAwait)
}

// TestViewsWatchErrorFrame pins the mid-stream failure path: a view that stops
// running says so and closes rather than stalling on a silent connection.
func TestViewsWatchErrorFrame(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	gw, ownerTok := watchFixture(t)
	srv := httptest.NewServer(api.NewHandler(gw, api.WithWatchIntervals(watchDetector, watchHeartbeat)))
	defer srv.Close()

	c := openWatch(t, srv.URL, ownerTok, "component-reachability")
	defer c.close()
	c.awaitEvent(t, "change", watchAwait)

	// The database goes away under the open stream: the next detector pass
	// fails, and the stream must report and end.
	gw.Close()
	c.awaitEvent(t, "error", watchAwait)
	c.awaitClose(t, watchAwait)
}
