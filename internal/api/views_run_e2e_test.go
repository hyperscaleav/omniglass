package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// viewsFixture is a real server over a real bus: what lands through
// /telemetry:push is what the views tail carries, as in production. Two
// locations each hold one component; group A is location loc-a.
type viewsFixture struct {
	ctx      context.Context
	dsn      string
	base     string
	ownerTok string
	c        *apiClient
}

func newViewsFixture(t *testing.T, opts ...api.Option) *viewsFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
	}
	ctx := context.Background()
	dsn := seededDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	all := scope.Set{All: true}
	for _, g := range []string{"a", "b"} {
		loc := "loc-" + g
		if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: loc, LocationType: "building"}, all); err != nil {
			t.Fatalf("create location: %v", err)
		}
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "codec-" + g, LocationName: &loc}, all, all, all, all); err != nil {
			t.Fatalf("create component: %v", err)
		}
	}
	busSrv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	t.Cleanup(busSrv.Shutdown)
	srv := httptest.NewServer(api.NewHandler(gw, append([]api.Option{api.WithTelemetryPublisher(busSrv)}, opts...)...))
	t.Cleanup(srv.Close)
	return &viewsFixture{ctx: ctx, dsn: dsn, base: srv.URL, ownerTok: ownerTok, c: &apiClient{t: t, ctx: ctx, base: srv.URL}}
}

// push lands telemetry on a component through the real push route.
func (f *viewsFixture) push(component string, body map[string]any) {
	body["owner"] = map[string]any{"kind": "component", "ref": component}
	body["source"] = "views-test"
	f.c.do(f.ownerTok, http.MethodPost, "/telemetry:push", body, http.StatusAccepted)
}

func (f *viewsFixture) pushMetric(component string, v float64) {
	f.push(component, map[string]any{"metrics": []map[string]any{{"name": "room-temperature", "value": v}}})
}

type viewRunWire struct {
	Cursor  string `json:"cursor"`
	Results []struct {
		Cursor   string `json:"cursor"`
		Entities []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"entities"`
		Metric []struct {
			Entity string `json:"entity"`
			Points []struct {
				Value float64 `json:"value"`
			} `json:"points"`
		} `json:"metric"`
		Property []struct {
			Entity string `json:"entity"`
			Points []struct {
				Value string `json:"value"`
			} `json:"points"`
		} `json:"property"`
		Events []struct {
			Entity  string `json:"entity"`
			Message string `json:"message"`
		} `json:"events"`
		Logs []struct {
			Entity  string `json:"entity"`
			Message string `json:"message"`
		} `json:"logs"`
	} `json:"results"`
}

func (f *viewsFixture) run(t *testing.T, tok string, bindings ...map[string]any) viewRunWire {
	t.Helper()
	var out viewRunWire
	if err := json.Unmarshal(f.c.do(tok, http.MethodPost, "/views:run", map[string]any{"bindings": bindings}, http.StatusOK), &out); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	return out
}

func metricBinding(extra map[string]any) map[string]any {
	b := map[string]any{"primitive": "metric", "kind": "component", "keys": []string{"room-temperature"}, "window_seconds": 3600}
	for k, v := range extra {
		b[k] = v
	}
	return b
}

// sseEvent is one Server-Sent Event as the wire framed it.
type sseEvent struct {
	ID    string
	Event string
	Data  string
}

type viewTailWire struct {
	Cursor   string `json:"cursor"`
	Bindings []struct {
		Binding int `json:"binding"`
		Metric  []struct {
			Entity string  `json:"entity"`
			Value  float64 `json:"value"`
		} `json:"metric"`
		Events []struct {
			Entity  string `json:"entity"`
			Message string `json:"message"`
		} `json:"events"`
		Logs []struct {
			Entity  string `json:"entity"`
			Message string `json:"message"`
		} `json:"logs"`
	} `json:"bindings"`
}

// watch is one open /views:watch connection, its events on a channel.
type watch struct {
	events chan sseEvent
	status int
	cancel context.CancelFunc
}

// openWatch opens one stream; the test reads its events until it has what it
// waits for, then closes it.
func (f *viewsFixture) openWatch(t *testing.T, tok string, bindings []map[string]any, lastEventID string) *watch {
	t.Helper()
	raw, _ := json.Marshal(bindings)
	ctx, cancel := context.WithCancel(f.ctx)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.base+"/api/v1/views:watch?bindings="+url.QueryEscape(string(raw)), nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("open watch: %v", err)
	}
	w := &watch{events: make(chan sseEvent, 256), status: resp.StatusCode, cancel: cancel}
	t.Cleanup(cancel)
	go func() {
		defer resp.Body.Close()
		defer close(w.events)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		var ev sseEvent
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if ev.Event != "" || ev.Data != "" {
					w.events <- ev
				}
				ev = sseEvent{}
			case strings.HasPrefix(line, "id: "):
				ev.ID = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				ev.Event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				ev.Data += strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	return w
}

// until reads events until done says it has what it waits for, failing on a
// reset or the stream ending first.
func (w *watch) until(t *testing.T, what string, done func(sseEvent) bool) {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case ev, ok := <-w.events:
			if !ok {
				t.Fatalf("the stream ended before %s", what)
			}
			if ev.Event == "reset" {
				t.Fatalf("the stream reset (%s) before %s", ev.Data, what)
			}
			if done(ev) {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func tailOf(t *testing.T, ev sseEvent) viewTailWire {
	t.Helper()
	var out viewTailWire
	if ev.Event == "rows" {
		if err := json.Unmarshal([]byte(ev.Data), &out); err != nil {
			t.Fatalf("decode rows %q: %v", ev.Data, err)
		}
	}
	return out
}

// customRoleTok mints a user holding one custom role, all-scoped, so a
// test can withhold exactly one permission.
func (f *viewsFixture) customRoleTok(t *testing.T, role string, perms []string) string {
	t.Helper()
	conn, err := pgx.Connect(f.ctx, f.dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(f.ctx)
	if _, err := conn.Exec(f.ctx, `insert into role (name, official, permissions, inherits) values ($1, false, $2, '{}')`, role, perms); err != nil {
		t.Fatalf("insert role: %v", err)
	}
	return userWithGrants(t, f.ctx, f.dsn, role+"-svc", []grant{{role: role, scopeKind: "all"}})
}

// A viewer scoped to group A (location loc-a) reads through a binding only
// what that scope reaches: a kind selector resolves to group A's component
// alone, a literal ref to group B's is the same 404 as one that does not
// exist, and the live tail carries none of group B's samples.
func TestViewsBindingIsScoped(t *testing.T) {
	t.Parallel()
	f := newViewsFixture(t)
	var locA string
	conn, err := pgx.Connect(f.ctx, f.dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := conn.QueryRow(f.ctx, `select id::text from location where name = 'loc-a'`).Scan(&locA); err != nil {
		t.Fatalf("loc-a: %v", err)
	}
	conn.Close(f.ctx)
	viewer := setupScopedViewer(t, f.ctx, f.dsn, "group-a-viewer", "viewer", "location", locA)

	f.pushMetric("codec-a", 21)
	f.pushMetric("codec-b", 99)
	waitFor(t, "both samples landed", func() bool {
		r := f.run(t, f.ownerTok, metricBinding(nil))
		return len(r.Results[0].Metric) == 2
	})

	got := f.run(t, viewer, metricBinding(nil)).Results[0]
	if len(got.Entities) != 1 || got.Entities[0].Name != "codec-a" {
		t.Fatalf("the kind selector resolved %+v for a group A viewer, want codec-a alone", got.Entities)
	}
	if len(got.Metric) != 1 || got.Metric[0].Entity != got.Entities[0].ID || got.Metric[0].Points[0].Value != 21 {
		t.Fatalf("the group A viewer's series: %+v", got.Metric)
	}
	codecA := got.Entities[0].ID

	for _, ref := range []string{"codec-b", "no-such-codec"} {
		status, body := f.c.send(viewer, http.MethodPost, "/views:run", map[string]any{"bindings": []map[string]any{metricBinding(map[string]any{"refs": []string{ref}})}})
		if status != http.StatusNotFound {
			t.Fatalf("a literal ref to %s: %d %s, want the non-disclosing 404", ref, status, body)
		}
	}
	if status, _ := f.c.send(viewer, http.MethodGet, "/views:watch?bindings="+url.QueryEscape(`[{"primitive":"metric","kind":"component","refs":["codec-b"],"keys":["room-temperature"],"window_seconds":3600}]`), nil); status != http.StatusNotFound {
		t.Fatalf("a watch on an out-of-scope ref: %d, want 404 before the stream opens", status)
	}

	w := f.openWatch(t, viewer, []map[string]any{metricBinding(map[string]any{"cursor": got.Cursor})}, "")
	if w.status != http.StatusOK {
		t.Fatalf("watch status %d", w.status)
	}
	// Group B's sample lands first, so the stream has passed it by the time
	// group A's arrives.
	f.pushMetric("codec-b", 98)
	waitFor(t, "group B's second sample landed", func() bool {
		r := f.run(t, f.ownerTok, metricBinding(map[string]any{"refs": []string{"codec-b"}}))
		return len(r.Results[0].Metric) == 1 && len(r.Results[0].Metric[0].Points) == 2
	})
	f.pushMetric("codec-a", 22)
	w.until(t, "group A's live sample", func(ev sseEvent) bool {
		for _, b := range tailOf(t, ev).Bindings {
			for _, m := range b.Metric {
				if m.Entity != codecA {
					t.Fatalf("the group A viewer's tail carried another entity's sample: %+v", m)
				}
				if m.Value == 22 {
					return true
				}
			}
		}
		return false
	})
}

// The route is refused without view:read, and a binding is refused without
// its primitive's read permission (the kind it reads) even with view:read.
func TestViewsBindingNeedsViewReadAndTheKindsRead(t *testing.T) {
	t.Parallel()
	f := newViewsFixture(t)
	noView := f.customRoleTok(t, "component-reader", []string{"component:read"})
	noKind := f.customRoleTok(t, "view-only", []string{"view:read"})
	body := map[string]any{"bindings": []map[string]any{metricBinding(nil)}}
	watchPath := "/views:watch?bindings=" + url.QueryEscape(`[{"primitive":"metric","kind":"component","keys":["room-temperature"],"window_seconds":3600}]`)

	if status, _ := f.c.send(noView, http.MethodPost, "/views:run", body); status != http.StatusForbidden {
		t.Fatalf("run without view:read: %d, want 403", status)
	}
	if status, _ := f.c.send(noView, http.MethodGet, watchPath, nil); status != http.StatusForbidden {
		t.Fatalf("watch without view:read: %d, want 403", status)
	}
	status, out := f.c.send(noKind, http.MethodPost, "/views:run", body)
	if status != http.StatusForbidden || !strings.Contains(string(out), "component:read") {
		t.Fatalf("run without component:read: %d %s, want a 403 naming component:read", status, out)
	}
	if status, _ := f.c.send(noKind, http.MethodGet, watchPath, nil); status != http.StatusForbidden {
		t.Fatalf("watch without component:read: %d, want 403", status)
	}
}

// With samples landing while the history is read, the history and the live
// stream from its cursor meet with no gap and no duplicate: every sample
// arrives exactly once.
func TestViewsHistoryAndLiveMeetWithNoGapOrDuplicate(t *testing.T) {
	t.Parallel()
	f := newViewsFixture(t)
	const n = 30
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 1; i <= n; i++ {
			f.pushMetric("codec-a", float64(i))
		}
	}()
	// Read the history mid-stream of pushes.
	waitFor(t, "some samples landed", func() bool {
		r := f.run(t, f.ownerTok, metricBinding(map[string]any{"refs": []string{"codec-a"}}))
		return len(r.Results[0].Metric) == 1 && len(r.Results[0].Metric[0].Points) >= 3
	})
	hist := f.run(t, f.ownerTok, metricBinding(map[string]any{"refs": []string{"codec-a"}}))
	seen := map[float64]int{}
	for _, p := range hist.Results[0].Metric[0].Points {
		seen[p.Value]++
	}
	w := f.openWatch(t, f.ownerTok, []map[string]any{metricBinding(map[string]any{"refs": []string{"codec-a"}, "cursor": hist.Results[0].Cursor})}, "")
	w.until(t, "every sample", func(ev sseEvent) bool {
		for _, b := range tailOf(t, ev).Bindings {
			for _, m := range b.Metric {
				seen[m.Value]++
			}
		}
		return len(seen) == n
	})
	wg.Wait()
	for i := 1; i <= n; i++ {
		if seen[float64(i)] != 1 {
			t.Fatalf("sample %d arrived %d times across the history and the stream, want exactly once", i, seen[float64(i)])
		}
	}
}

// A dropped stream resumes from the last event id it delivered: the
// reconnect carries what landed while it was down, and nothing it already
// delivered, even though the binding's own cursor is older.
func TestViewsWatchResumesFromItsLastEventWithNoDuplicates(t *testing.T) {
	t.Parallel()
	f := newViewsFixture(t)
	hist := f.run(t, f.ownerTok, metricBinding(map[string]any{"refs": []string{"codec-a"}}))
	bindings := []map[string]any{metricBinding(map[string]any{"refs": []string{"codec-a"}, "cursor": hist.Results[0].Cursor})}

	w := f.openWatch(t, f.ownerTok, bindings, "")
	f.pushMetric("codec-a", 1)
	var last string
	w.until(t, "the first sample", func(ev sseEvent) bool {
		if ev.ID != "" {
			last = ev.ID
		}
		rows := tailOf(t, ev)
		return len(rows.Bindings) == 1 && len(rows.Bindings[0].Metric) == 1 && rows.Bindings[0].Metric[0].Value == 1
	})
	w.cancel()

	f.pushMetric("codec-a", 2)
	waitFor(t, "the sample sent while the stream was down", func() bool {
		r := f.run(t, f.ownerTok, metricBinding(map[string]any{"refs": []string{"codec-a"}}))
		return len(r.Results[0].Metric) == 1 && len(r.Results[0].Metric[0].Points) == 2
	})
	again := f.openWatch(t, f.ownerTok, bindings, last)
	f.pushMetric("codec-a", 3)
	var got []float64
	again.until(t, "the samples after the last event", func(ev sseEvent) bool {
		for _, b := range tailOf(t, ev).Bindings {
			for _, m := range b.Metric {
				got = append(got, m.Value)
			}
		}
		return len(got) >= 2
	})
	if fmt.Sprint(got) != "[2 3]" {
		t.Fatalf("the resumed stream carried %v, want [2 3]: the one it had delivered again, or a gap", got)
	}
}

// One stream serves every binding of a page: a metric, an event and a log
// binding over one connection each get their own rows, tagged by index.
func TestViewsWatchServesEveryBindingOnOneConnection(t *testing.T) {
	t.Parallel()
	f := newViewsFixture(t)
	bindings := []map[string]any{
		metricBinding(map[string]any{"refs": []string{"codec-a"}}),
		{"primitive": "event", "kind": "component", "refs": []string{"codec-a"}, "keys": []string{"call-started"}, "window_seconds": 3600},
		{"primitive": "log", "kind": "component", "refs": []string{"codec-a"}, "severity": "error", "window_seconds": 3600},
	}
	w := f.openWatch(t, f.ownerTok, bindings, "")
	w.until(t, "the stream's opening mark", func(ev sseEvent) bool { return ev.Event == "mark" })
	f.push("codec-a", map[string]any{
		"metrics": []map[string]any{{"name": "room-temperature", "value": 20}},
		"events":  []map[string]any{{"name": "call-started", "message": "call up"}},
		"logs":    []map[string]any{{"message": "quiet", "severity": "info"}, {"message": "loud", "severity": "error"}},
	})
	got := map[int]string{}
	w.until(t, "every binding's row", func(ev sseEvent) bool {
		for _, b := range tailOf(t, ev).Bindings {
			switch {
			case len(b.Metric) > 0:
				got[b.Binding] = fmt.Sprint(b.Metric[0].Value)
			case len(b.Events) > 0:
				got[b.Binding] = b.Events[0].Message
			case len(b.Logs) > 0:
				if len(b.Logs) != 1 {
					t.Fatalf("the log binding carried %+v, want only the error line", b.Logs)
				}
				got[b.Binding] = b.Logs[0].Message
			}
		}
		return len(got) == 3
	})
	if got[0] != "20" || got[1] != "call up" || got[2] != "loud" {
		t.Fatalf("rows per binding: %v", got)
	}
}

// ends reads the stream until the server ends it, failing on a reset or on it
// running past within.
func (w *watch) ends(t *testing.T, what string, within time.Duration) {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case ev, ok := <-w.events:
			if !ok {
				return
			}
			if ev.Event == "reset" {
				t.Fatalf("the stream reset (%s), want it to end because %s", ev.Data, what)
			}
		case <-deadline:
			t.Fatalf("the stream ran past %s, want it ended because %s", within, what)
		}
	}
}

// A watch is not forever on the scope it resolved at connect: the server ends
// it once it has run its lifetime, so the client reconnects and resolves again,
// and it carries a last mark first so the reconnect resumes with no gap.
func TestViewsWatchEndsAtItsLifetime(t *testing.T) {
	t.Parallel()
	f := newViewsFixture(t, api.WithViewWatchLimits(0, time.Second, 0))
	w := f.openWatch(t, f.ownerTok, []map[string]any{metricBinding(nil)}, "")
	start := time.Now()
	w.ends(t, "it ran its lifetime", 10*time.Second)
	if took := time.Since(start); took < 900*time.Millisecond {
		t.Fatalf("the stream ended after %s, before its lifetime", took)
	}
}

// The heartbeat rechecks the caller the stream opened for: a session revoked,
// or grants changed, ends the stream, and the reconnect answers honestly.
func TestViewsWatchEndsWhenItsCallerNoLongerStands(t *testing.T) {
	t.Parallel()
	f := newViewsFixture(t, api.WithViewWatchLimits(0, 0, 200*time.Millisecond))
	conn, err := pgx.Connect(f.ctx, f.dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(f.ctx)
	bindings := []map[string]any{metricBinding(nil)}

	revoked := userWithGrants(t, f.ctx, f.dsn, "revoked-viewer", []grant{{role: "viewer", scopeKind: "all"}})
	w := f.openWatch(t, revoked, bindings, "")
	w.until(t, "the opening mark", func(ev sseEvent) bool { return ev.Event == "mark" })
	if _, err := conn.Exec(f.ctx, `delete from credential where secret_hash = $1`, auth.HashToken(revoked)); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	w.ends(t, "its session was revoked", 5*time.Second)
	if again := f.openWatch(t, revoked, bindings, ""); again.status != http.StatusUnauthorized {
		t.Fatalf("a reconnect on the revoked session: %d, want 401", again.status)
	}

	regranted := userWithGrants(t, f.ctx, f.dsn, "regranted-viewer", []grant{{role: "viewer", scopeKind: "all"}})
	w = f.openWatch(t, regranted, bindings, "")
	w.until(t, "the opening mark", func(ev sseEvent) bool { return ev.Event == "mark" })
	if _, err := conn.Exec(f.ctx, `delete from user_grant where user_id = (select user_id from credential where secret_hash = $1)`, auth.HashToken(regranted)); err != nil {
		t.Fatalf("drop the grants: %v", err)
	}
	w.ends(t, "its grants changed", 5*time.Second)
	if again := f.openWatch(t, regranted, bindings, ""); again.status != http.StatusForbidden {
		t.Fatalf("a reconnect with no grants: %d, want 403", again.status)
	}

	steady := f.openWatch(t, f.ownerTok, bindings, "")
	beats := 0
	steady.until(t, "three heartbeats on a caller that still stands", func(ev sseEvent) bool {
		if ev.Event == "mark" {
			beats++
		}
		return beats >= 4
	})
}

// One user holds a bounded number of watches open: past it the open is a
// 429, and a watch the client closes frees its place.
func TestViewsWatchRefusesPastThePerUserLimit(t *testing.T) {
	t.Parallel()
	f := newViewsFixture(t, api.WithViewWatchLimits(2, 0, 0))
	bindings := []map[string]any{metricBinding(nil)}
	first := f.openWatch(t, f.ownerTok, bindings, "")
	second := f.openWatch(t, f.ownerTok, bindings, "")
	if first.status != http.StatusOK || second.status != http.StatusOK {
		t.Fatalf("the first two watches: %d and %d, want 200", first.status, second.status)
	}
	if third := f.openWatch(t, f.ownerTok, bindings, ""); third.status != http.StatusTooManyRequests {
		t.Fatalf("a third watch: %d, want 429", third.status)
	}
	other := userWithGrants(t, f.ctx, f.dsn, "other-viewer", []grant{{role: "viewer", scopeKind: "all"}})
	if w := f.openWatch(t, other, bindings, ""); w.status != http.StatusOK {
		t.Fatalf("another user's watch: %d, want 200: the limit is per user", w.status)
	}
	first.cancel()
	waitFor(t, "the closed watch's place freed", func() bool {
		w := f.openWatch(t, f.ownerTok, bindings, "")
		w.cancel()
		return w.status == http.StatusOK
	})
}
