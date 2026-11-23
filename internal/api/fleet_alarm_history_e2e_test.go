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

// TestFleetAlarmsReadHistoryOverARange drives GET /alarms as the alarms page's
// state filter does (#2426): active by default, cleared, or all, with since and
// until keeping the alarms whose raised-to-cleared interval overlaps the range
// (one raised before it and open in it is kept), an inverted range a 400, a
// narrow caller never learning a cleared alarm out of its scope, and a
// component placed only through its system named at that system's location.
func TestFleetAlarmsReadHistoryOverARange(t *testing.T) {
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

	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "hist-hq", "location_type": "campus"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/components", map[string]any{"name": "hist-disp", "product": "generic-device", "location": "hist-hq"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/components", map[string]any{"name": "hist-other", "product": "generic-device", "location": "hist-hq"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/components", map[string]any{"name": "hist-placed", "product": "generic-device"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/systems", map[string]any{"name": "hist-room", "location": "hist-hq"}, http.StatusCreated)
	c.do(owner, http.MethodPut, "/systems/hist-room/members/hist-placed", nil, http.StatusNoContent)

	before := raiseFor(t, c, owner, "hist-disp", "high", "before", "cleared before the range")
	c.do(owner, http.MethodDelete, "/components/hist-disp/alarms/"+before, nil, http.StatusNoContent)
	inside := raiseFor(t, c, owner, "hist-disp", "high", "inside", "raised and cleared in range")
	c.do(owner, http.MethodDelete, "/components/hist-disp/alarms/"+inside, nil, http.StatusNoContent)
	across := raiseFor(t, c, owner, "hist-disp", "warning", "across", "open across the range")
	after := raiseFor(t, c, owner, "hist-disp", "warning", "after", "raised after the range")
	hidden := raiseFor(t, c, owner, "hist-other", "high", "hidden", "cleared, out of scope")
	c.do(owner, http.MethodDelete, "/components/hist-other/alarms/"+hidden, nil, http.StatusNoContent)
	placed := raiseFor(t, c, owner, "hist-placed", "high", "placed", "placed through its system")

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	place := func(id, raised, cleared string) {
		t.Helper()
		q := `update alarm set raised_at = now() - $2::interval, cleared_at = case when $3 = '' then null else now() - $3::interval end where id = $1`
		if _, err := conn.Exec(ctx, q, id, raised, cleared); err != nil {
			t.Fatalf("place %s: %v", id, err)
		}
	}
	// The range is 10h..2h ago.
	place(before, "20 hours", "15 hours")
	place(inside, "6 hours", "5 hours")
	place(across, "12 hours", "")
	place(after, "1 hour", "")

	names := map[string]string{before: "before", inside: "inside", across: "across", after: "after", hidden: "hidden", placed: "placed"}
	type row struct {
		ID         string     `json:"id"`
		ClearedAt  *time.Time `json:"cleared_at"`
		Location   string     `json:"location"`
		SystemIDs  []string   `json:"system_ids"`
		LocationID string     `json:"location_id"`
	}
	read := func(tok, query string, want int) []row {
		t.Helper()
		raw := c.do(tok, http.MethodGet, "/alarms"+query, nil, want)
		var body struct {
			Alarms []row `json:"alarms"`
		}
		if want == http.StatusOK {
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
		}
		return body.Alarms
	}
	ids := func(rows []row) string {
		out := []string{}
		for _, r := range rows {
			out = append(out, names[r.ID])
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}
	at := func(d time.Duration) string { return url.QueryEscape(time.Now().UTC().Add(d).Format(time.RFC3339)) }
	rng := "since=" + at(-10*time.Hour) + "&until=" + at(-2*time.Hour)

	for query, want := range map[string]string{
		"":                                      "across,after,placed",
		"?state=active":                         "across,after,placed",
		"?state=cleared":                        "before,hidden,inside",
		"?state=all":                            "across,after,before,hidden,inside,placed",
		"?state=all&" + rng:                     "across,inside",
		"?state=cleared&" + rng:                 "inside",
		"?" + rng:                               "across",
		"?state=cleared&location=hist-hq":       "before,hidden,inside",
		"?state=cleared&severity=warning":       "",
		"?state=all&since=" + at(-10*time.Hour): "across,after,hidden,inside,placed",
	} {
		if got := ids(read(owner, query, http.StatusOK)); got != want {
			t.Errorf("GET /alarms%s = %q, want %q", query, got, want)
		}
	}
	read(owner, "?since="+at(-time.Hour)+"&until="+at(-48*time.Hour), http.StatusBadRequest)
	read(owner, "?state=bogus", http.StatusUnprocessableEntity)

	for _, r := range read(owner, "?state=cleared", http.StatusOK) {
		if r.ClearedAt == nil {
			t.Errorf("cleared alarm %s carries no cleared_at", names[r.ID])
		}
	}
	roomID := entityID(t, c, owner, "/systems", "hist-room")
	hqID := entityID(t, c, owner, "/locations", "hist-hq")
	for _, r := range read(owner, "?state=all", http.StatusOK) {
		if names[r.ID] == "placed" && r.Location != "hist-hq" {
			t.Errorf("a component placed through its system names location %q, want hist-hq", r.Location)
		}
		if r.LocationID != hqID {
			t.Errorf("alarm %s names location id %q, want hist-hq's %s so the console can link it", names[r.ID], r.LocationID, hqID)
		}
		if names[r.ID] == "placed" && (len(r.SystemIDs) != 1 || r.SystemIDs[0] != roomID) {
			t.Errorf("a placed alarm names system ids %v, want [%s] so the console can link the system", r.SystemIDs, roomID)
		}
	}

	// A caller who reads one component sees its cleared alarms and no other's.
	dispID := entityID(t, c, owner, "/components", "hist-disp")
	narrow := userWithGrants(t, ctx, dsn, "hist-narrow", []grant{{role: "viewer", scopeKind: "component", scopeID: dispID}})
	if got := ids(read(narrow, "?state=all", http.StatusOK)); got != "across,after,before,inside" {
		t.Errorf("a hist-disp-only reader got %q on state=all, want only its own alarms", got)
	}
	if got := ids(read(narrow, "?state=cleared&"+rng, http.StatusOK)); got != "inside" {
		t.Errorf("a hist-disp-only reader got %q on cleared over a range, want inside", got)
	}
}
