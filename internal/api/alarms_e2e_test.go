package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A component's verdict on the alarms read (#1454): "no alarms" is two
// different facts, and the read must not blur them. A component something has
// observed, with nothing wrong, is healthy; a component nothing has ever
// observed (no sample, no alarm, ever) is unknown, because healthy there would
// be a false green.
func TestComponentAlarmsVerdictIsUnknownUntilSomethingObservesIt(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	srv := httptest.NewServer(api.NewHandler(gw))
	t.Cleanup(srv.Close)
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	owner := userWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})

	verdictOf := func(name string) string {
		t.Helper()
		var env struct {
			Verdict string `json:"health"`
		}
		raw := c.do(owner, http.MethodGet, "/components/"+name+"/alarms", nil, http.StatusOK)
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("decode alarms: %v", err)
		}
		return env.Verdict
	}

	c.do(owner, http.MethodPost, "/components", map[string]any{"name": "cam-1", "product": "generic-device"}, http.StatusCreated)
	if got := verdictOf("cam-1"); got != "unknown" {
		t.Fatalf("a never-observed component's verdict = %q, want unknown (healthy would be a false green)", got)
	}

	// An alarm is an observation; clearing it leaves the component observed
	// and now clean, which is healthy.
	id := raiseFor(t, c, owner, "cam-1", "high", "no-signal", "no signal")
	if got := verdictOf("cam-1"); got != "outage" {
		t.Fatalf("verdict with a high alarm = %q, want outage", got)
	}
	c.do(owner, http.MethodDelete, "/components/cam-1/alarms/"+id, nil, http.StatusNoContent)
	if got := verdictOf("cam-1"); got != "healthy" {
		t.Fatalf("verdict after the alarm cleared = %q, want healthy (observed, nothing wrong)", got)
	}

	// An endpoint that has delivered anything is an observation too.
	c.do(owner, http.MethodPost, "/components", map[string]any{"name": "cam-2", "product": "generic-device"}, http.StatusCreated)
	var ep struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.do(owner, http.MethodPost, "/endpoints", map[string]any{"transport": "icmp", "component": "cam-2"}, http.StatusCreated), &ep); err != nil {
		t.Fatalf("decode endpoint: %v", err)
	}
	if got := verdictOf("cam-2"); got != "unknown" {
		t.Fatalf("a component whose endpoint nothing has collected from = %q, want unknown", got)
	}
	if err := gw.StampEndpointsSeen(ctx, []string{ep.ID}); err != nil {
		t.Fatalf("stamp endpoint seen: %v", err)
	}
	if got := verdictOf("cam-2"); got != "healthy" {
		t.Fatalf("a component whose endpoint has delivered = %q, want healthy", got)
	}
}

// The alarm queue (#2333): GET /alarms names each row's location and systems,
// narrows by severity, location subtree, acknowledged and snoozed, pages with
// page_token, and never returns an alarm on a component outside the caller's
// component:read.
func TestFleetAlarmQueueFiltersPagesAndStaysInScope(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	srv := httptest.NewServer(api.NewHandler(gw))
	t.Cleanup(srv.Close)
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	owner := userWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})

	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "hq", "location_type": "campus"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "hq-b1", "location_type": "building", "parent": "hq"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "annex", "location_type": "campus"}, http.StatusCreated)
	for name, loc := range map[string]string{"cam-1": "hq-b1", "cam-2": "hq-b1", "cam-3": "annex"} {
		c.do(owner, http.MethodPost, "/components", map[string]any{"name": name, "product": "generic-device", "location": loc}, http.StatusCreated)
	}
	c.do(owner, http.MethodPost, "/systems", map[string]any{"name": "boardroom"}, http.StatusCreated)
	c.do(owner, http.MethodPut, "/systems/boardroom/members/cam-1", nil, http.StatusNoContent)

	crit := raiseFor(t, c, owner, "cam-1", "disaster", "dark", "dark")
	warn := raiseFor(t, c, owner, "cam-2", "warning", "warm", "warm")
	far := raiseFor(t, c, owner, "cam-3", "high", "far", "far")
	c.do(owner, http.MethodPost, "/components/cam-2/alarms/"+warn+":acknowledge", nil, http.StatusOK)
	c.do(owner, http.MethodPost, "/components/cam-3/alarms/"+far+":snooze", map[string]any{"duration": "1h"}, http.StatusOK)

	type row struct {
		ID           string   `json:"id"`
		Component    string   `json:"component"`
		Location     string   `json:"location"`
		LocationPath string   `json:"location_path"`
		Systems      []string `json:"systems"`
	}
	type page struct {
		Alarms []row  `json:"alarms"`
		Next   string `json:"next_page_token"`
	}
	read := func(tok, query string, want int) page {
		t.Helper()
		var p page
		raw := c.do(tok, http.MethodGet, "/alarms"+query, nil, want)
		if want == http.StatusOK {
			if err := json.Unmarshal(raw, &p); err != nil {
				t.Fatalf("decode: %v", err)
			}
		}
		return p
	}
	comps := func(p page) string {
		out := ""
		for _, r := range p.Alarms {
			out += r.Component + " "
		}
		return out
	}

	all := read(owner, "", http.StatusOK)
	if comps(all) != "cam-1 cam-3 cam-2 " {
		t.Fatalf("queue = %q, want worst first: cam-1 cam-3 cam-2", comps(all))
	}
	if r := all.Alarms[0]; r.ID != crit || r.Location != "hq-b1" || r.LocationPath != "hq.hq-b1" || len(r.Systems) != 1 || r.Systems[0] != "boardroom" {
		t.Errorf("first row = %+v, want its location hq-b1 (hq.hq-b1) and system boardroom", r)
	}

	for query, want := range map[string]string{
		"?severity=disaster&severity=warning": "cam-1 cam-2 ",
		"?location=hq":                        "cam-1 cam-2 ",
		"?location=annex":                     "cam-3 ",
		"?acknowledged=true":                  "cam-2 ",
		"?acknowledged=false":                 "cam-1 cam-3 ",
		"?snoozed=true":                       "cam-3 ",
		"?snoozed=false":                      "cam-1 cam-2 ",
	} {
		if got := comps(read(owner, query, http.StatusOK)); got != want {
			t.Errorf("GET /alarms%s = %q, want %q", query, got, want)
		}
	}

	// Paging one at a time walks the whole queue in order and then ends.
	walked, token := "", ""
	for range 5 {
		p := read(owner, "?limit=1&page_token="+token, http.StatusOK)
		walked += comps(p)
		if p.Next == "" {
			break
		}
		token = p.Next
	}
	if walked != comps(all) {
		t.Errorf("paged walk = %q, want %q", walked, comps(all))
	}
	read(owner, "?page_token=nonsense", http.StatusUnprocessableEntity)

	// A caller who reads one component sees its alarm and no other, whatever
	// the filter or the page.
	camID := entityID(t, c, owner, "/components", "cam-2")
	narrow := userWithGrants(t, ctx, dsn, "narrow", []grant{{role: "viewer", scopeKind: "component", scopeID: camID}})
	for _, query := range []string{"", "?location=hq", "?severity=disaster", "?limit=1"} {
		got := comps(read(narrow, query, http.StatusOK))
		if got != "cam-2 " && got != "" {
			t.Errorf("a cam-2-only reader got %q on GET /alarms%s", got, query)
		}
	}
	if got := comps(read(narrow, "", http.StatusOK)); got != "cam-2 " {
		t.Errorf("a cam-2-only reader got %q, want its one alarm", got)
	}

	// Reading a component never names a system the caller may not read: a
	// cam-1-only reader sees cam-1's alarm with no systems, and one who may
	// also read boardroom sees it named.
	cam1ID := entityID(t, c, owner, "/components", "cam-1")
	boardroomID := entityID(t, c, owner, "/systems", "boardroom")
	for _, tc := range []struct {
		user    string
		grants  []grant
		systems string
	}{
		{"cam1only", []grant{{role: "viewer", scopeKind: "component", scopeID: cam1ID}}, ""},
		{"cam1room", []grant{{role: "viewer", scopeKind: "component", scopeID: cam1ID}, {role: "viewer", scopeKind: "system", scopeID: boardroomID}}, "boardroom"},
	} {
		p := read(userWithGrants(t, ctx, dsn, tc.user, tc.grants), "", http.StatusOK)
		if len(p.Alarms) != 1 || p.Alarms[0].Component != "cam-1" {
			t.Fatalf("%s: queue = %q, want cam-1's alarm alone", tc.user, comps(p))
		}
		if got := strings.Join(p.Alarms[0].Systems, ","); got != tc.systems {
			t.Errorf("%s: cam-1's systems = %q, want %q", tc.user, got, tc.systems)
		}
	}
}
