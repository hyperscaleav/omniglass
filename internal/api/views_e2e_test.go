package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
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

// viewResultResp mirrors the uniform ViewResult body every view run returns.
type viewResultResp struct {
	Columns []struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Role string `json:"role"`
	} `json:"columns"`
	Rows          [][]any `json:"rows"`
	NextPageToken string  `json:"next_page_token"`
}

// viewsDirResp mirrors the GET /views directory body.
type viewsDirResp struct {
	Views []struct {
		Name         string            `json:"name"`
		Summary      string            `json:"summary"`
		Permission   string            `json:"permission"`
		FieldMapping map[string]string `json:"field_mapping"`
		Params       []struct {
			Name     string `json:"name"`
			Type     string `json:"type"`
			Required bool   `json:"required"`
		} `json:"params"`
		Columns []struct {
			Name string `json:"name"`
			Type string `json:"type"`
			Role string `json:"role"`
		} `json:"columns"`
	} `json:"views"`
}

// TestViewsAPI drives the view directory and the run route end to end: the
// directory publishes the proof view's contract (columns, field-mapping,
// declared permission); a run returns the uniform ViewResult with real rows,
// scope-bounded per caller; the declared permission is enforced in the handler
// on top of the view:read stamp; an unknown view is 404; an undeclared param
// is a 400 naming it; and a re-run changes nothing. Skipped under -short.
func TestViewsAPI(t *testing.T) {
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

	// Two root components, one interface each: disp-1 probed down, cam-1 up.
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all); err != nil {
		t.Fatalf("create disp-1: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "cam-1"}, all); err != nil {
		t.Fatalf("create cam-1: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	// cam-1 gets a second, never-probed interface so the run must surface the
	// explicit unknown state, not drop the row.
	if _, err := conn.Exec(ctx, `insert into interface (name, type, component, params) values
		('disp-1-icmp', (select id from interface_type where name = 'icmp'), (select id from component where name = 'disp-1'), '{"target":"10.0.0.1"}'::jsonb),
		('cam-1-icmp',  (select id from interface_type where name = 'icmp'), (select id from component where name = 'cam-1'),  '{"target":"10.0.0.2"}'::jsonb),
		('cam-1-tcp',   (select id from interface_type where name = 'tcp'),  (select id from component where name = 'cam-1'),  '{"target":"10.0.0.2","port":5000}'::jsonb)`); err != nil {
		t.Fatalf("insert interfaces: %v", err)
	}
	// A view:read-only role, inserted BEFORE the first request so the lazily
	// built role index includes it (the index is cached once per process).
	if _, err := conn.Exec(ctx,
		`insert into role (name, official, permissions, inherits) values ('views-only', false, '{"view:read"}', '{}')`); err != nil {
		t.Fatalf("insert views-only role: %v", err)
	}
	ts := time.Now().UTC().Truncate(time.Second)
	if err := gw.InsertStateSamples(ctx, []storage.StateSampleWrite{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "interface.reachable", Instance: "disp-1-icmp", Value: "down", Source: "icmp", TS: ts},
		{OwnerKind: "component", OwnerID: "cam-1", Key: "interface.reachable", Instance: "cam-1-icmp", Value: "up", Source: "icmp", TS: ts},
	}); err != nil {
		t.Fatalf("insert verdicts: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// The directory publishes the proof view's whole contract.
	out := c.do(ownerTok, http.MethodGet, "/views", nil, http.StatusOK)
	var dir viewsDirResp
	if err := json.Unmarshal(out, &dir); err != nil {
		t.Fatalf("decode directory: %v", err)
	}
	var found bool
	for _, v := range dir.Views {
		if v.Name != "component-reachability" {
			continue
		}
		found = true
		if v.Permission != "component:read" {
			t.Errorf("declared permission = %q, want component:read", v.Permission)
		}
		if len(v.Columns) == 0 {
			t.Errorf("directory carries no columns for the view")
		}
		if v.FieldMapping["value"] == "" || v.FieldMapping["label"] == "" {
			t.Errorf("field-mapping incomplete: %v", v.FieldMapping)
		}
	}
	if !found {
		t.Fatalf("component-reachability missing from the directory: %+v", dir.Views)
	}

	// An all-scope run returns the full estate as the uniform ViewResult.
	run := c.do(ownerTok, http.MethodGet, "/views/component-reachability:run", nil, http.StatusOK)
	var vr viewResultResp
	if err := json.Unmarshal(run, &vr); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if len(vr.Rows) != 3 {
		t.Fatalf("all-scope rows = %d, want 3: %s", len(vr.Rows), run)
	}
	col := map[string]int{}
	for i, cdef := range vr.Columns {
		col[cdef.Name] = i
	}
	for _, need := range []string{"component", "interface", "interface_type", "state", "since"} {
		if _, ok := col[need]; !ok {
			t.Fatalf("run columns missing %q: %+v", need, vr.Columns)
		}
	}
	// Rows are component-then-interface ordered: cam-1's probed icmp, cam-1's
	// never-probed tcp, then disp-1. Cells are asserted by CONTENT through the
	// column index, the renderer's own path.
	if vr.Rows[0][col["component"]] != "cam-1" || vr.Rows[0][col["state"]] != "up" {
		t.Errorf("row 0 = %v, want cam-1 up", vr.Rows[0])
	}
	// The never-probed interface is the explicit unknown state with a null
	// since, not a dropped row and not an empty cell.
	if vr.Rows[1][col["interface"]] != "cam-1-tcp" || vr.Rows[1][col["state"]] != "unknown" || vr.Rows[1][col["since"]] != nil {
		t.Errorf("row 1 = %v, want cam-1-tcp unknown with null since", vr.Rows[1])
	}
	if vr.Rows[2][col["component"]] != "disp-1" || vr.Rows[2][col["state"]] != "down" {
		t.Errorf("row 2 = %v, want disp-1 down", vr.Rows[2])
	}

	// A valid run is deterministic: a re-run changes nothing.
	again := c.do(ownerTok, http.MethodGet, "/views/component-reachability:run", nil, http.StatusOK)
	if !bytes.Equal(run, again) {
		t.Errorf("re-run differs:\n%s\n%s", run, again)
	}

	// A viewer scoped to cam-1 alone sees only cam-1's row: the same query,
	// scope-injected, with no per-view code deciding it.
	var camID string
	if err := conn.QueryRow(ctx, `select id from component where name = 'cam-1'`).Scan(&camID); err != nil {
		t.Fatalf("cam id: %v", err)
	}
	viewerTok := setupScopedViewer(t, ctx, dsn, "viewer-cam", "viewer", "component", camID)
	scoped := c.do(viewerTok, http.MethodGet, "/views/component-reachability:run", nil, http.StatusOK)
	var svr viewResultResp
	if err := json.Unmarshal(scoped, &svr); err != nil {
		t.Fatalf("decode scoped run: %v", err)
	}
	if len(svr.Rows) != 2 || svr.Rows[0][col["component"]] != "cam-1" || svr.Rows[1][col["component"]] != "cam-1" {
		t.Fatalf("scoped rows = %v, want only cam-1's two interfaces", svr.Rows)
	}

	// view:read admits the directory but NOT a view whose declared permission
	// the caller lacks: the handler-tier check on top of the route stamp.
	viewsOnlyTok := principalWithGrants(t, ctx, dsn, "views-only", []grant{{role: "views-only", scopeKind: "all"}})
	c.do(viewsOnlyTok, http.MethodGet, "/views", nil, http.StatusOK)
	c.do(viewsOnlyTok, http.MethodGet, "/views/component-reachability:run", nil, http.StatusForbidden)

	// An unknown view is a 404; an undeclared param is a 400 naming it.
	c.do(ownerTok, http.MethodGet, "/views/nope:run", nil, http.StatusNotFound)
	bad := c.do(ownerTok, http.MethodGet, "/views/component-reachability:run?param="+url.QueryEscape("bogus=1"), nil, http.StatusBadRequest)
	if !bytes.Contains(bad, []byte("bogus")) {
		t.Errorf("400 body does not name the offending param: %s", bad)
	}
}

// TestDefaultViewSetAPI drives the three views the default set adds: the
// cursor-paged event feed (a page_token walk covers the set with no duplicate
// and no gap), the sample-history series over both sample lanes, and the
// estate-counts tiles, each scope-bounded, each 404-ing a parameterized target
// the caller cannot see. Skipped under -short.
func TestDefaultViewSetAPI(t *testing.T) {
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
		('disp-1-icmp', (select id from interface_type where name = 'icmp'), (select id from component where name = 'disp-1'), '{}'::jsonb),
		('cam-1-icmp',  (select id from interface_type where name = 'icmp'), (select id from component where name = 'cam-1'),  '{}'::jsonb)`); err != nil {
		t.Fatalf("insert interfaces: %v", err)
	}

	// Five occurrences on disp-1, one on cam-1, so a paged walk crosses a page
	// boundary and the owner filter has something to exclude.
	base := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	for i := range 5 {
		if err := gw.InsertEvents(ctx, []storage.EventWrite{{
			OwnerKind: "component", OwnerID: "disp-1", Key: "call.started",
			Message: "disp event " + strconv.Itoa(i), Source: "t", TS: base.Add(time.Duration(i) * time.Minute),
		}}); err != nil {
			t.Fatalf("insert event %d: %v", i, err)
		}
	}
	if err := gw.InsertEvents(ctx, []storage.EventWrite{{
		OwnerKind: "component", OwnerID: "cam-1", Key: "call.started", Message: "cam event", Source: "t", TS: base.Add(9 * time.Minute),
	}}); err != nil {
		t.Fatalf("insert cam event: %v", err)
	}
	ts := time.Now().UTC().Truncate(time.Microsecond)
	if err := gw.InsertMetricSamples(ctx, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "icmp.rtt_avg", Instance: "disp-1-icmp", Value: 3.5, Source: "icmp", TS: ts.Add(-2 * time.Minute)},
		{OwnerKind: "component", OwnerID: "disp-1", Key: "icmp.rtt_avg", Instance: "disp-1-icmp", Value: 4.5, Source: "icmp", TS: ts.Add(-time.Minute)},
	}); err != nil {
		t.Fatalf("insert metrics: %v", err)
	}
	if err := gw.InsertStateSamples(ctx, []storage.StateSampleWrite{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "interface.reachable", Instance: "disp-1-icmp", Value: "up", Source: "icmp", TS: ts},
		{OwnerKind: "component", OwnerID: "cam-1", Key: "interface.reachable", Instance: "cam-1-icmp", Value: "down", Source: "icmp", TS: ts},
	}); err != nil {
		t.Fatalf("insert verdicts: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	decode := func(raw []byte) (viewResultResp, map[string]int) {
		t.Helper()
		var vr viewResultResp
		if err := json.Unmarshal(raw, &vr); err != nil {
			t.Fatalf("decode: %v\n%s", err, raw)
		}
		col := map[string]int{}
		for i, cd := range vr.Columns {
			col[cd.Name] = i
		}
		return vr, col
	}

	// The whole default set is published, each with its declared permission.
	dirRaw := c.do(ownerTok, http.MethodGet, "/views", nil, http.StatusOK)
	var dir viewsDirResp
	if err := json.Unmarshal(dirRaw, &dir); err != nil {
		t.Fatalf("decode directory: %v", err)
	}
	perms := map[string]string{}
	for _, v := range dir.Views {
		perms[v.Name] = v.Permission
	}
	for name, want := range map[string]string{
		"component-reachability": "component:read",
		"event-feed":             "event:read",
		"sample-history":         "component:read",
		"estate-counts":          "component:read",
	} {
		if perms[name] != want {
			t.Errorf("%s permission = %q, want %q", name, perms[name], want)
		}
	}

	// event-feed pages by cursor: two pages of four then two, walking all six
	// rows with no duplicate and no gap.
	seen := map[string]bool{}
	token := ""
	for page := 0; page < 5; page++ {
		path := "/views/event-feed:run?param=" + url.QueryEscape("limit=4")
		if token != "" {
			path += "&page_token=" + url.QueryEscape(token)
		}
		vr, col := decode(c.do(ownerTok, http.MethodGet, path, nil, http.StatusOK))
		for _, row := range vr.Rows {
			msg, _ := row[col["message"]].(string)
			if seen[msg] {
				t.Errorf("duplicate row across pages: %s", msg)
			}
			seen[msg] = true
		}
		token = vr.NextPageToken
		if token == "" {
			break
		}
	}
	if len(seen) != 6 {
		t.Errorf("paged walk saw %d of 6 rows: %v", len(seen), seen)
	}

	// A malformed page token is a clean 400, never a silent restart.
	c.do(ownerTok, http.MethodGet, "/views/event-feed:run?page_token=not-a-cursor", nil, http.StatusBadRequest)

	// A limit ABOVE the server's page cap still pages. The view clamps to the
	// same ceiling storage enforces, so a capped page is recognised as full and
	// keeps emitting a token; comparing the row count against the caller's
	// un-clamped number would read it as the last page and strand the rest.
	over, ocol := decode(c.do(ownerTok, http.MethodGet,
		"/views/event-feed:run?param="+url.QueryEscape("limit=100000"), nil, http.StatusOK))
	if len(over.Rows) != 6 {
		t.Errorf("over-cap page rows = %d, want all 6", len(over.Rows))
	}
	if _, ok := ocol["message"]; !ok {
		t.Errorf("over-cap page lost its columns: %+v", over.Columns)
	}

	// sample-history reads the numeric lane oldest-first, and the same view
	// reads the categorical lane with a null value.
	vr, col := decode(c.do(ownerTok, http.MethodGet,
		"/views/sample-history:run?param="+url.QueryEscape("owner=disp-1")+"&param="+url.QueryEscape("key=icmp.rtt_avg"), nil, http.StatusOK))
	if len(vr.Rows) != 2 {
		t.Fatalf("rtt series rows = %d, want 2", len(vr.Rows))
	}
	if vr.Rows[0][col["value"]] != 3.5 || vr.Rows[1][col["value"]] != 4.5 {
		t.Errorf("series = %v, want 3.5 then 4.5 (oldest first)", vr.Rows)
	}
	states, scol := decode(c.do(ownerTok, http.MethodGet,
		"/views/sample-history:run?param="+url.QueryEscape("owner=disp-1")+"&param="+url.QueryEscape("key=interface.reachable"), nil, http.StatusOK))
	if len(states.Rows) != 1 || states.Rows[0][scol["state"]] != "up" || states.Rows[0][scol["value"]] != nil {
		t.Errorf("state series = %v, want one up row with a null value", states.Rows)
	}
	// A missing required param is a 400 naming it.
	bad := c.do(ownerTok, http.MethodGet, "/views/sample-history:run?param="+url.QueryEscape("owner=disp-1"), nil, http.StatusBadRequest)
	if !bytes.Contains(bad, []byte("key")) {
		t.Errorf("400 body does not name the missing param: %s", bad)
	}

	// estate-counts tiles, in their declared order.
	counts, ccol := decode(c.do(ownerTok, http.MethodGet, "/views/estate-counts:run", nil, http.StatusOK))
	if len(counts.Rows) != 4 {
		t.Fatalf("tiles = %d, want 4", len(counts.Rows))
	}
	got := map[string]float64{}
	for _, row := range counts.Rows {
		name, _ := row[ccol["tile"]].(string)
		n, _ := row[ccol["count"]].(float64)
		got[name] = n
	}
	if got["components"] != 2 || got["interfaces"] != 2 || got["reachable"] != 1 || got["unreachable"] != 1 {
		t.Errorf("tiles = %v, want 2/2/1/1", got)
	}

	// Scope bounds every view, and an out-of-scope parameterized target is a
	// non-disclosing 404 rather than an empty result or a 403.
	var camID string
	if err := conn.QueryRow(ctx, `select id from component where name = 'cam-1'`).Scan(&camID); err != nil {
		t.Fatalf("cam id: %v", err)
	}
	viewerTok := setupScopedViewer(t, ctx, dsn, "viewer-cam-set", "viewer", "component", camID)

	feed, fcol := decode(c.do(viewerTok, http.MethodGet, "/views/event-feed:run", nil, http.StatusOK))
	if len(feed.Rows) != 1 || feed.Rows[0][fcol["owner"]] != "cam-1" {
		t.Errorf("scoped feed = %v, want only cam-1's row", feed.Rows)
	}
	scopedCounts, sccol := decode(c.do(viewerTok, http.MethodGet, "/views/estate-counts:run", nil, http.StatusOK))
	for _, row := range scopedCounts.Rows {
		if row[sccol["tile"]] == "components" && row[sccol["count"]] != float64(1) {
			t.Errorf("scoped component tile = %v, want 1", row[sccol["count"]])
		}
	}
	c.do(viewerTok, http.MethodGet, "/views/event-feed:run?param="+url.QueryEscape("owner=disp-1"), nil, http.StatusNotFound)
	c.do(viewerTok, http.MethodGet,
		"/views/sample-history:run?param="+url.QueryEscape("owner=disp-1")+"&param="+url.QueryEscape("key=icmp.rtt_avg"), nil, http.StatusNotFound)
	// An absent component is the same 404, so a probe cannot tell "exists but
	// hidden" from "does not exist".
	c.do(viewerTok, http.MethodGet,
		"/views/sample-history:run?param="+url.QueryEscape("owner=ghost")+"&param="+url.QueryEscape("key=icmp.rtt_avg"), nil, http.StatusNotFound)
}
