package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	if _, err := conn.Exec(ctx, `insert into interface (name, type, component, params) values
		('disp-1-icmp', (select id from interface_type where name = 'icmp'), (select id from component where name = 'disp-1'), '{"target":"10.0.0.1"}'::jsonb),
		('cam-1-icmp',  (select id from interface_type where name = 'icmp'), (select id from component where name = 'cam-1'),  '{"target":"10.0.0.2"}'::jsonb)`); err != nil {
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
	if len(vr.Rows) != 2 {
		t.Fatalf("all-scope rows = %d, want 2: %s", len(vr.Rows), run)
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
	// Rows are component-ordered: cam-1 up, then disp-1 down. Cells are
	// asserted by CONTENT through the column index, the renderer's own path.
	if vr.Rows[0][col["component"]] != "cam-1" || vr.Rows[0][col["state"]] != "up" {
		t.Errorf("row 0 = %v, want cam-1 up", vr.Rows[0])
	}
	if vr.Rows[1][col["component"]] != "disp-1" || vr.Rows[1][col["state"]] != "down" {
		t.Errorf("row 1 = %v, want disp-1 down", vr.Rows[1])
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
	if len(svr.Rows) != 1 || svr.Rows[0][col["component"]] != "cam-1" {
		t.Fatalf("scoped rows = %v, want only cam-1", svr.Rows)
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
