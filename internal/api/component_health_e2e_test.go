package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

type healthHistoryWire struct {
	OwnerKind   string `json:"owner_kind"`
	Owner       string `json:"owner"`
	Verdict     string `json:"health"`
	Transitions []struct {
		TS     time.Time `json:"ts"`
		Health string    `json:"health"`
	} `json:"transitions"`
	CarriedIn       string     `json:"carried_in"`
	FirstRecordedAt *time.Time `json:"first_recorded_at"`
}

// TestComponentHealthHistoryRange drives GET /components/{name}/health the way
// a range picker does (#2305): the recorded transitions inside since..until and
// none outside it, a 400 for an inverted range, and a non-disclosing 404 for a
// viewer whose scope does not reach the component. The same range parameters
// bound the system and location reads.
func TestComponentHealthHistoryRange(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := seededDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	owner := userWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})
	for _, name := range []string{"mine", "theirs"} {
		c.do(owner, http.MethodPost, "/components", map[string]any{"name": name, "product": "generic-device"}, http.StatusCreated)
	}
	mineID := entityID(t, c, owner, "/components", "mine")
	alarm := raiseFor(t, c, owner, "mine", "high", "no-signal", "no signal")
	c.do(owner, http.MethodDelete, "/components/mine/alarms/"+alarm, nil, http.StatusNoContent)

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	// Place the recorded edges: the first 40 days back, the rest in the last
	// ten days, so the default 30-day window must drop exactly the first.
	if _, err := conn.Exec(ctx, `
		update property p set ts = case when r.n = 1 then now() - interval '40 days'
			else now() - interval '10 days' + (r.n - 2) * interval '1 hour' end
		from (select id, row_number() over (order by id) n from property
			where component_id = $1 and provenance = 'calculated' and instance = '') r
		where p.id = r.id`, mineID); err != nil {
		t.Fatalf("place transitions: %v", err)
	}

	read := func(tok, path string, want int) healthHistoryWire {
		t.Helper()
		var w healthHistoryWire
		raw := c.do(tok, http.MethodGet, path, nil, want)
		if want == http.StatusOK {
			if err := json.Unmarshal(raw, &w); err != nil {
				t.Fatalf("decode %s: %v", path, err)
			}
		}
		return w
	}
	at := func(d time.Duration) string { return url.QueryEscape(time.Now().UTC().Add(d).Format(time.RFC3339)) }

	c.do(owner, http.MethodGet, "/components/mine", nil, http.StatusOK)
	all := read(owner, "/components/mine/health?since="+at(-60*24*time.Hour), http.StatusOK)
	if all.OwnerKind != "component" || all.Owner != "mine" {
		t.Fatalf("owner = %s/%s, want component/mine", all.OwnerKind, all.Owner)
	}
	n := len(all.Transitions)
	if n < 2 {
		t.Fatalf("recorded %d transitions, want at least 2 (raise and clear)", n)
	}
	if all.Verdict != "healthy" {
		t.Errorf("verdict after clear = %q, want healthy", all.Verdict)
	}

	if got := read(owner, "/components/mine/health", http.StatusOK); len(got.Transitions) != n-1 {
		t.Errorf("default window returned %d transitions, want %d (the 40-day-old edge is outside it)", len(got.Transitions), n-1)
	}
	if got := read(owner, "/components/mine/health?since="+at(-60*24*time.Hour)+"&until="+at(-20*24*time.Hour), http.StatusOK); len(got.Transitions) != 1 {
		t.Errorf("60d..20d returned %d transitions, want 1", len(got.Transitions))
	}
	// Only until: the default span is the 30 days before it, so ten days ago
	// reaches back past the 40-day edge's neighbours but not to it.
	got := read(owner, "/components/mine/health?until="+at(-5*24*time.Hour), http.StatusOK)
	if len(got.Transitions) != n-1 {
		t.Errorf("until alone returned %d transitions, want %d", len(got.Transitions), n-1)
	}
	// The derived start is a range start the caller bounded, so the 40-day-old
	// edge is carried in; the default window carries it in too, from the start
	// it opens at (#2735), so a reader never takes its first edge for the
	// health before it.
	if got.CarriedIn == "" {
		t.Errorf("until alone: carried_in is absent, want the 40-day-old edge carried in")
	}
	if def := read(owner, "/components/mine/health", http.StatusOK); def.CarriedIn == "" || def.CarriedIn != got.CarriedIn {
		t.Errorf("default window: carried_in = %q, want the 40-day-old edge (%q) carried in", def.CarriedIn, got.CarriedIn)
	}
	if got := read(owner, "/components/mine/health?since="+at(-1*time.Hour), http.StatusOK); len(got.Transitions) != 0 {
		t.Errorf("a range after every edge returned %d transitions, want 0", len(got.Transitions))
	}

	// since after until is a 400, on the component route and on the others.
	bad := "?since=" + at(-time.Hour) + "&until=" + at(-48*time.Hour)
	for _, p := range []string{"/components/mine/health", "/systems/none/health", "/locations/none/health"} {
		read(owner, p+bad, http.StatusBadRequest)
	}

	// The scope: a viewer holding only "theirs" cannot tell "mine" exists.
	theirsID := entityID(t, c, owner, "/components", "theirs")
	viewer := userWithGrants(t, ctx, dsn, "theirs-viewer", []grant{{role: "viewer", scopeKind: "component", scopeID: theirsID}})
	read(viewer, "/components/theirs/health", http.StatusOK)
	read(viewer, "/components/mine/health", http.StatusNotFound)
	read(viewer, "/components/no-such-component/health", http.StatusNotFound)
}

// TestHealthHistoryCarriesInTheHealthAtSince: a range with no edge inside it
// still says what the health was when it began (carried_in, set only when since
// is given); an entity first recorded inside the range has none, and every read
// says when the entity was first recorded (#2305).
func TestHealthHistoryCarriesInTheHealthAtSince(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := seededDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	owner := userWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	read := func(path string) healthHistoryWire {
		t.Helper()
		var w healthHistoryWire
		if err := json.Unmarshal(c.do(owner, http.MethodGet, path, nil, http.StatusOK), &w); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		return w
	}
	at := func(d time.Duration) string { return url.QueryEscape(time.Now().UTC().Add(d).Format(time.RFC3339)) }

	// quiet: its only edges are 40 days old (raised, then cleared: healthy last).
	c.do(owner, http.MethodPost, "/components", map[string]any{"name": "quiet", "product": "generic-device"}, http.StatusCreated)
	quietID := entityID(t, c, owner, "/components", "quiet")
	alarm := raiseFor(t, c, owner, "quiet", "high", "no-signal", "no signal")
	c.do(owner, http.MethodDelete, "/components/quiet/alarms/"+alarm, nil, http.StatusNoContent)
	if _, err := conn.Exec(ctx, `
		update property p set ts = now() - interval '40 days' + r.n * interval '1 hour'
		from (select id, row_number() over (order by id) n from property
			where component_id = $1 and provenance = 'calculated' and instance = '') r
		where p.id = r.id`, quietID); err != nil {
		t.Fatalf("place transitions: %v", err)
	}
	got := read("/components/quiet/health?since=" + at(-5*24*time.Hour))
	if len(got.Transitions) != 0 || got.CarriedIn != "healthy" {
		t.Errorf("quiet range = %d transitions, carried_in %q; want none and healthy", len(got.Transitions), got.CarriedIn)
	}
	if got.FirstRecordedAt == nil || time.Since(*got.FirstRecordedAt) < 39*24*time.Hour {
		t.Errorf("first_recorded_at = %v, want about 40 days ago", got.FirstRecordedAt)
	}
	if w := read("/components/quiet/health?since=" + at(-60*24*time.Hour)); w.CarriedIn != "" {
		t.Errorf("since before every edge: carried_in = %q, want absent", w.CarriedIn)
	}
	// No since: the default window opens 30 days ago, after the quiet
	// component's 40-day-old edges, so their last one is carried in (#2735).
	if w := read("/components/quiet/health"); w.CarriedIn != "healthy" {
		t.Errorf("no since: carried_in = %q, want healthy, carried in at the default window's start", w.CarriedIn)
	}

	// fresh: first recorded inside the range, so nothing was in force.
	c.do(owner, http.MethodPost, "/components", map[string]any{"name": "fresh", "product": "generic-device"}, http.StatusCreated)
	raiseFor(t, c, owner, "fresh", "high", "no-signal", "no signal")
	fresh := read("/components/fresh/health?since=" + at(-5*24*time.Hour))
	if fresh.CarriedIn != "" {
		t.Errorf("an entity first recorded inside the range has carried_in %q, want absent", fresh.CarriedIn)
	}
	if len(fresh.Transitions) == 0 || fresh.FirstRecordedAt == nil || !fresh.FirstRecordedAt.Equal(fresh.Transitions[0].TS) {
		t.Errorf("fresh = %+v, want first_recorded_at to be its first transition", fresh)
	}
}
