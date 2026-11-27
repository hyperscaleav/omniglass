package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/world"
)

type labCalendarBody struct {
	Start   time.Time `json:"start"`
	End     time.Time `json:"end"`
	Entries []struct {
		Kind     string         `json:"kind"`
		Start    time.Time      `json:"start"`
		End      time.Time      `json:"end"`
		Site     string         `json:"site"`
		Building string         `json:"building"`
		Room     string         `json:"room"`
		System   string         `json:"system"`
		Payload  map[string]any `json:"payload"`
	} `json:"entries"`
}

func labCalKnobs() calendar.Knobs {
	return calendar.Knobs{
		BookingRate: 0.8, SlotMix: map[int]float64{30: 1, 60: 1}, NoShowRate: 0.2, EndEarlyRate: 0.1, EndLateRate: 0.1,
		Attendance: calendar.Range{Min: 2, Max: 8}, WalkInRate: 0.3, Hours: calendar.Hours{Start: 8, End: 18}, RecordingRate: 0.5,
	}
}

func labCalWorld() *world.World {
	return &world.World{
		Version: world.Version, Scenario: "t", Outpost: "o", CalendarSeed: 77,
		Meetings: map[string]calendar.Knobs{"conference": labCalKnobs()},
		Sites: []world.Site{{Name: "hq", Rooms: []world.Room{
			{Name: "r101", Building: "tower", Standard: "conference", System: "sys-a", Capacity: 8},
			{Name: "r102", Building: "tower", Standard: "conference", System: "sys-b", Capacity: 8},
		}}},
	}
}

// TestLabCalendarAPI drives GET /lab/calendar against a real database: the
// window's entries, the simulation:read gate, the caller's system scope, the
// window bound and the filters, and the 404 of a server that is no lab.
func TestLabCalendarAPI(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	ids := map[string]string{}
	for _, s := range []string{"sys-a", "sys-b"} {
		sys, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: s}, all, all)
		if err != nil {
			t.Fatal(err)
		}
		ids[s] = sys.ID
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `insert into role (name, permissions) values ('cal-system-reader', '{"system:read"}'), ('cal-sim-reader', '{"system:read","simulation:read"}')`); err != nil {
		t.Fatal(err)
	}
	grant := func(role, kind, id string) string {
		tok, h, p, err := auth.NewBearerToken()
		if err != nil {
			t.Fatal(err)
		}
		var pid string
		if err := conn.QueryRow(ctx, `insert into users (kind) values ('machine') returning id`).Scan(&pid); err != nil {
			t.Fatal(err)
		}
		var scopeID any
		if id != "" {
			scopeID = id
		}
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`insert into machine (user_id, name) values ($1, $2)`, []any{pid, "cal-" + role + "-" + kind + id[:min(len(id), 4)]}},
			{`insert into credential (user_id, kind, secret_hash, prefix) values ($1, 'bearer', $2, $3)`, []any{pid, h, p}},
			{`insert into user_grant (user_id, role_id, scope_kind, scope_id) values ($1, (select id from role where name = $2), $3, $4)`, []any{pid, role, kind, scopeID}},
		} {
			if _, err := conn.Exec(ctx, q.sql, q.args...); err != nil {
				t.Fatalf("%s: %v", q.sql, err)
			}
		}
		return tok
	}
	noSim := grant("cal-system-reader", "all", "")
	onlyA := grant("cal-sim-reader", "system", ids["sys-a"])
	bothRooms := grant("cal-sim-reader", "all", "")

	w := labCalWorld()
	lab := httptest.NewServer(api.NewHandler(gw, api.WithLab(true), api.WithLabCalendar(api.WorldsCalendar(w))))
	defer lab.Close()
	notLab := httptest.NewServer(api.NewHandler(gw, api.WithLabCalendar(api.WorldsCalendar(w))))
	defer notLab.Close()
	c := &apiClient{t: t, ctx: ctx, base: lab.URL}

	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	q := func(extra url.Values) string {
		v := url.Values{"start": {day.Format(time.RFC3339)}, "end": {day.Add(24 * time.Hour).Format(time.RFC3339)}}
		for k, vs := range extra {
			v[k] = vs
		}
		return "/lab/calendar?" + v.Encode()
	}
	read := func(tok string, extra url.Values) labCalendarBody {
		var b labCalendarBody
		if err := json.Unmarshal(c.do(tok, http.MethodGet, q(extra), nil, http.StatusOK), &b); err != nil {
			t.Fatal(err)
		}
		return b
	}

	// The window's entries are what the world's own calendar derives.
	want := map[string]int{}
	for _, r := range []struct{ room, system string }{{"r101", "sys-a"}, {"r102", "sys-b"}} {
		want[r.system] = len(w.Calendar().Entries(calendar.Room{Site: "hq", Building: "tower", Room: r.room}, day, day.Add(24*time.Hour)))
	}
	if want["sys-a"] == 0 || want["sys-b"] == 0 {
		t.Fatalf("the pinned day holds no meetings: %v", want)
	}
	got := read(ownerTok, nil)
	counts := map[string]int{}
	for i, e := range got.Entries {
		counts[e.System]++
		if e.Kind != "meeting" || e.Site != "hq" || e.Building != "tower" || e.Payload["outcome"] == nil || e.Payload["organizer"] == nil {
			t.Fatalf("entry %d = %+v", i, e)
		}
		if i > 0 && e.Start.Before(got.Entries[i-1].Start) {
			t.Fatalf("entries are not in start order at %d", i)
		}
	}
	if counts["sys-a"] != want["sys-a"] || counts["sys-b"] != want["sys-b"] {
		t.Fatalf("entries per system = %v, want %v", counts, want)
	}

	// A caller without simulation:read is refused; no credential is 401.
	c.do(noSim, http.MethodGet, q(nil), nil, http.StatusForbidden)
	c.do("", http.MethodGet, q(nil), nil, http.StatusUnauthorized)

	// A caller reads only the rooms whose system is in scope.
	for _, e := range read(onlyA, nil).Entries {
		if e.System != "sys-a" {
			t.Fatalf("a caller scoped to sys-a sees %s", e.System)
		}
	}
	if n := len(read(onlyA, nil).Entries); n != want["sys-a"] {
		t.Fatalf("scoped caller sees %d entries, want %d", n, want["sys-a"])
	}
	if n := len(read(bothRooms, nil).Entries); n != want["sys-a"]+want["sys-b"] {
		t.Fatalf("all-scope caller sees %d entries", n)
	}
	// A filter naming a room outside the scope reads empty, not forbidden.
	if n := len(read(onlyA, url.Values{"system": {"sys-b"}}).Entries); n != 0 {
		t.Fatalf("a filter on an out-of-scope system returned %d entries", n)
	}

	// Filters narrow.
	if n := len(read(ownerTok, url.Values{"room": {"r102"}}).Entries); n != want["sys-b"] {
		t.Fatalf("room filter returned %d, want %d", n, want["sys-b"])
	}
	if n := len(read(ownerTok, url.Values{"site": {"elsewhere"}}).Entries); n != 0 {
		t.Fatalf("site filter returned %d", n)
	}

	// The window is bounded, and ordered.
	if code, body := c.send(ownerTok, http.MethodGet, "/lab/calendar?start="+url.QueryEscape(day.Format(time.RFC3339))+"&end="+url.QueryEscape(day.AddDate(0, 0, 32).Format(time.RFC3339)), nil); code != http.StatusUnprocessableEntity || !strings.Contains(string(body), "31 days") {
		t.Fatalf("a 32-day window = %d %s, want 422 naming 31 days", code, body)
	}
	if code, _ := c.send(ownerTok, http.MethodGet, "/lab/calendar?start="+url.QueryEscape(day.Format(time.RFC3339))+"&end="+url.QueryEscape(day.Format(time.RFC3339)), nil); code != http.StatusUnprocessableEntity {
		t.Fatalf("an empty window = %d, want 422", code)
	}
	if code, _ := c.send(ownerTok, http.MethodGet, "/lab/calendar", nil); code != http.StatusUnprocessableEntity && code != http.StatusBadRequest {
		t.Fatalf("no window = %d, want a refusal", code)
	}
	// A 31-day window is allowed.
	if code, _ := c.send(ownerTok, http.MethodGet, "/lab/calendar?start="+url.QueryEscape(day.Format(time.RFC3339))+"&end="+url.QueryEscape(day.AddDate(0, 0, 31).Format(time.RFC3339)), nil); code != http.StatusOK {
		t.Fatalf("a 31-day window = %d", code)
	}

	// A server that is no lab answers 404.
	nl := &apiClient{t: t, ctx: ctx, base: notLab.URL}
	nl.do(ownerTok, http.MethodGet, q(nil), nil, http.StatusNotFound)
}
