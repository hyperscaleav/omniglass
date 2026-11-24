package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestTimelineReadsHonorTheRange drives the reads the overlay timeline makes
// over a page's range (#2425): a component's and a system's alarms raised
// during since..until (one open across the whole range kept, one cleared
// before it and one raised after it dropped), and the health read's
// carried_in, the verdict in force when since opened the range.
func TestTimelineReadsHonorTheRange(t *testing.T) {
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
	t.Cleanup(gw.Close)
	srv := httptest.NewServer(api.NewHandler(gw))
	t.Cleanup(srv.Close)
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	owner := userWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})

	c.do(owner, http.MethodPost, "/components", map[string]any{"name": "disp", "product": "generic-device"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/systems", map[string]any{"name": "room"}, http.StatusCreated)
	c.do(owner, http.MethodPut, "/systems/room/members/disp", nil, http.StatusNoContent)
	dispID := entityID(t, c, owner, "/components", "disp")

	before := raiseFor(t, c, owner, "disp", "high", "before", "cleared before the range")
	c.do(owner, http.MethodDelete, "/components/disp/alarms/"+before, nil, http.StatusNoContent)
	inside := raiseFor(t, c, owner, "disp", "high", "inside", "raised and cleared in range")
	c.do(owner, http.MethodDelete, "/components/disp/alarms/"+inside, nil, http.StatusNoContent)
	across := raiseFor(t, c, owner, "disp", "warning", "across", "open across the range")
	after := raiseFor(t, c, owner, "disp", "warning", "after", "raised after the range")

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	// The range is 10h..2h ago. Each alarm is placed relative to it.
	place := func(id, raised, cleared string) {
		t.Helper()
		q := `update alarm set raised_at = now() - $2::interval, cleared_at = case when $3 = '' then null else now() - $3::interval end where id = $1`
		if _, err := conn.Exec(ctx, q, id, raised, cleared); err != nil {
			t.Fatalf("place %s: %v", id, err)
		}
	}
	place(before, "20 hours", "15 hours")
	place(inside, "6 hours", "5 hours")
	place(across, "12 hours", "")
	place(after, "1 hour", "")
	// The component's health edges: degraded 20h ago, so the verdict in force
	// when the range opens is whatever the newest edge older than 10h says.
	if _, err := conn.Exec(ctx, `
		update property p set ts = now() - interval '20 hours' + r.n * interval '1 minute'
		from (select id, row_number() over (order by ts, id) n from property
			where component_id = $1 and provenance = 'calculated' and instance = '') r
		where p.id = r.id`, dispID); err != nil {
		t.Fatalf("place transitions: %v", err)
	}
	var wantCarried string
	if err := conn.QueryRow(ctx, `select value #>> '{}' from property where component_id = $1 and provenance = 'calculated' and instance = ''
		order by ts desc, id desc limit 1`, dispID).Scan(&wantCarried); err != nil {
		t.Fatalf("read newest edge: %v", err)
	}

	at := func(d time.Duration) string { return url.QueryEscape(time.Now().UTC().Add(d).Format(time.RFC3339)) }
	rng := "since=" + at(-10*time.Hour) + "&until=" + at(-2*time.Hour)
	ids := func(raw []byte) string {
		t.Helper()
		var body struct {
			Alarms []struct {
				ID string `json:"id"`
			} `json:"alarms"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode alarms: %v", err)
		}
		names := map[string]string{before: "before", inside: "inside", across: "across", after: "after"}
		out := []string{}
		for _, a := range body.Alarms {
			out = append(out, names[a.ID])
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}

	if got := ids(c.do(owner, http.MethodGet, "/components/disp/alarms?include_cleared=true&"+rng, nil, http.StatusOK)); got != "across,inside" {
		t.Errorf("component alarms in range = %s, want across,inside", got)
	}
	if got := ids(c.do(owner, http.MethodGet, "/components/disp/alarms?include_cleared=true", nil, http.StatusOK)); got != "across,after,before,inside" {
		t.Errorf("component alarms without a range = %s, want every one", got)
	}
	if got := ids(c.do(owner, http.MethodGet, "/systems/room/alarms?"+rng, nil, http.StatusOK)); got != "across,inside" {
		t.Errorf("system alarms in range = %s, want across,inside", got)
	}
	if got := ids(c.do(owner, http.MethodGet, "/systems/room/alarms", nil, http.StatusOK)); got != "across,after,before,inside" {
		t.Errorf("system alarms by default = %s, want the active ones and those cleared in 30 days", got)
	}
	bad := "?since=" + at(-time.Hour) + "&until=" + at(-48*time.Hour)
	c.do(owner, http.MethodGet, "/components/disp/alarms"+bad, nil, http.StatusBadRequest)
	c.do(owner, http.MethodGet, "/systems/room/alarms"+bad, nil, http.StatusBadRequest)

	var h struct {
		CarriedIn   string            `json:"carried_in"`
		Transitions []json.RawMessage `json:"transitions"`
	}
	if err := json.Unmarshal(c.do(owner, http.MethodGet, "/components/disp/health?"+rng, nil, http.StatusOK), &h); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if h.CarriedIn != wantCarried || len(h.Transitions) != 0 {
		t.Errorf("health over the range = carried_in %q with %d transitions, want %q and none", h.CarriedIn, len(h.Transitions), wantCarried)
	}
	h.CarriedIn = "unset"
	if err := json.Unmarshal(c.do(owner, http.MethodGet, "/components/disp/health", nil, http.StatusOK), &h); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if h.CarriedIn != "unset" {
		t.Errorf("health without since carried in %q, want the field absent", h.CarriedIn)
	}
}
