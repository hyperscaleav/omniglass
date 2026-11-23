package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

type batchRow struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Parent   *string `json:"parent"`
	ParentID *string `json:"parent_id"`
	Location *string `json:"location"`
	System   *string `json:"system"`
}

type batchOut struct {
	BatchID      string     `json:"batch_id"`
	ValidateOnly bool       `json:"validate_only"`
	Locations    []batchRow `json:"locations"`
	Systems      []batchRow `json:"systems"`
	Components   []batchRow `json:"components"`
}

type batchRefusal struct {
	Detail string `json:"detail"`
	Errors []struct {
		Location string `json:"location"`
	} `json:"errors"`
}

func decodeBatch(t *testing.T, raw []byte) batchOut {
	t.Helper()
	var out batchOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return out
}

// refusedAt asserts a refusal names the row at path, in its detail and as an
// error location.
func refusedAt(t *testing.T, raw []byte, path string) {
	t.Helper()
	var r batchRefusal
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	if !strings.HasPrefix(r.Detail, path+": ") {
		t.Fatalf("refusal detail %q, want it led by %s", r.Detail, path)
	}
	for _, e := range r.Errors {
		if e.Location == "body."+path {
			return
		}
	}
	t.Fatalf("refusal %s carries no error at body.%s", raw, path)
}

// TestBatchCreateAPI (#2368): a locations batch dry-runs (nothing written,
// the rows reported, a floor naming the building the same batch creates as its
// parent), then writes whole under one batch id on every audit row; a systems
// batch lands at those floors, and a components batch names one of those
// systems, each linked by name as its single create links.
func TestBatchCreateAPI(t *testing.T) {
	t.Parallel()
	f := newInstFixture(t)
	disp := storagetest.MintProduct(t, f.ctx, f.gw, "display")
	rows := func() int {
		return f.count(`select count(*) from location`) + f.count(`select count(*) from system`) +
			f.count(`select count(*) from component`) + f.count(`select count(*) from audit_log`)
	}

	locations := []map[string]any{{"name": "bc-campus", "location_type": "building"}}
	for i := range 3 {
		locations = append(locations, map[string]any{"name": fmt.Sprintf("bc-floor-%d", i), "location_type": "floor", "parent": "bc-campus"})
	}
	before := rows()
	dry := decodeBatch(t, f.c.do(f.owner, http.MethodPost, "/locations:batchCreate",
		map[string]any{"requests": locations, "validate_only": true}, http.StatusOK))
	if n := rows(); n != before {
		t.Fatalf("a dry run wrote %d rows", n-before)
	}
	if !dry.ValidateOnly || dry.BatchID != "" || len(dry.Locations) != 4 {
		t.Fatalf("dry run = %+v, want four rows reported and no batch id", dry)
	}
	if dry.Locations[0].Name != "bc-campus" || dry.Locations[0].ID != "" || dry.Locations[1].ParentID != nil ||
		dry.Locations[1].Parent == nil || *dry.Locations[1].Parent != "bc-campus" {
		t.Fatalf("dry rows = %+v, want the names, the in-batch parent by name and no ids", dry.Locations[:2])
	}

	auditBefore := f.count(`select count(*) from audit_log`)
	got := decodeBatch(t, f.c.do(f.owner, http.MethodPost, "/locations:batchCreate", map[string]any{"requests": locations}, http.StatusCreated))
	if got.BatchID == "" || got.ValidateOnly || len(got.Locations) != 4 || got.Locations[0].ID == "" {
		t.Fatalf("write = %+v, want four rows under a batch id", got)
	}
	if n := f.count(`select count(*) from location where parent_id = $1`, got.Locations[0].ID); n != 3 {
		t.Fatalf("floors under the batch's building = %d, want 3", n)
	}
	wrote := f.count(`select count(*) from audit_log`) - auditBefore
	if stamped := f.count(`select count(*) from audit_log where new->>'from_batch' = $1`, got.BatchID); wrote < 4 || stamped != wrote {
		t.Fatalf("the batch wrote %d audit rows, %d under its batch id, want every one", wrote, stamped)
	}

	var systems []map[string]any
	for i := range 3 {
		systems = append(systems, map[string]any{"system_type_id": "meeting", "location": fmt.Sprintf("bc-floor-%d", i)})
	}
	sys := decodeBatch(t, f.c.do(f.owner, http.MethodPost, "/systems:batchCreate", map[string]any{"requests": systems}, http.StatusCreated))
	if sys.BatchID == "" || sys.BatchID == got.BatchID || len(sys.Systems) != 3 || sys.Systems[0].Name == "" {
		t.Fatalf("systems batch = %+v, want three systems under a batch id of their own", sys)
	}
	if n := f.count(`select count(*) from system where location_id in (select id from location where parent_id = $1)`, got.Locations[0].ID); n != 3 {
		t.Fatalf("systems on the batch's floors = %d, want 3", n)
	}

	comps := decodeBatch(t, f.c.do(f.owner, http.MethodPost, "/components:batchCreate", map[string]any{"requests": []map[string]any{
		{"product": disp.Name, "location": "bc-floor-0", "system": sys.Systems[0].Name},
		{"product": disp.Name, "location": "bc-floor-1"},
	}}, http.StatusCreated))
	if len(comps.Components) != 2 || comps.Components[0].System == nil || *comps.Components[0].System != sys.Systems[0].Name {
		t.Fatalf("components batch = %+v, want the first in the system the earlier batch created", comps)
	}
}

// TestBatchCreateRefusesWhole (#2368): a refused row rolls back the whole
// batch and is named by its position in the request, on a dry run too; a caller
// without create scope at a location is refused for the row there; a batch
// over the row limit is a 413 naming the limit; a viewer is refused at the
// gate.
func TestBatchCreateRefusesWhole(t *testing.T) {
	t.Parallel()
	f := newInstFixture(t)
	disp := storagetest.MintProduct(t, f.ctx, f.gw, "display")
	rows := func() int {
		return f.count(`select count(*) from location`) + f.count(`select count(*) from system`) +
			f.count(`select count(*) from component`) + f.count(`select count(*) from audit_log`)
	}
	before := rows()

	// The second location names a type the fleet lacks: the first never lands.
	bad := map[string]any{"requests": []map[string]any{
		{"name": "doomed", "location_type": "building"},
		{"name": "doomed-too", "location_type": "no-such-type"},
	}}
	refusedAt(t, f.c.do(f.owner, http.MethodPost, "/locations:batchCreate", bad, http.StatusUnprocessableEntity), "requests[1]")
	bad["validate_only"] = true
	refusedAt(t, f.c.do(f.owner, http.MethodPost, "/locations:batchCreate", bad, http.StatusUnprocessableEntity), "requests[1]")

	// A component naming a product the fleet lacks refuses its batch the same way.
	refusedAt(t, f.c.do(f.owner, http.MethodPost, "/components:batchCreate", map[string]any{"requests": []map[string]any{
		{"product": disp.Name, "location": "hq"},
		{"product": "no-such-product", "location": "hq"},
	}}, http.StatusUnprocessableEntity), "requests[1]")
	if n := rows(); n != before {
		t.Fatalf("refused batches left %d rows", n-before)
	}

	// component:create reaches only annex: a component at hq is refused, and
	// the one the batch placed at annex before it rolls back with it.
	annexOnly := f.split("annex-placer", "annex")
	scoped := func(place string) map[string]any {
		return map[string]any{"requests": []map[string]any{{"location": "annex", "product": disp.Name}, {"location": place, "product": disp.Name}}}
	}
	refusedAt(t, f.c.do(annexOnly, http.MethodPost, "/components:batchCreate", scoped("hq"), http.StatusForbidden), "requests[1]")
	if n := rows(); n != before {
		t.Fatalf("a scope-refused batch left %d rows", n-before)
	}
	if ok := decodeBatch(t, f.c.do(annexOnly, http.MethodPost, "/components:batchCreate", scoped("annex"), http.StatusCreated)); len(ok.Components) != 2 {
		t.Fatalf("an in-scope batch = %+v, want two components", ok)
	}

	// Over the row limit is refused before anything is read, naming the limit.
	var many []map[string]any
	for range 1001 {
		many = append(many, map[string]any{"location_type": "building"})
	}
	if detail := string(f.c.do(f.owner, http.MethodPost, "/locations:batchCreate", map[string]any{"requests": many}, http.StatusRequestEntityTooLarge)); !strings.Contains(detail, "1000") {
		t.Fatalf("over-limit refusal = %s, want it to name the limit", detail)
	}

	// A viewer holds no location:create: refused at the gate.
	viewer := userWithGrants(t, f.ctx, f.dsn, "batch-viewer", []grant{{role: "viewer", scopeKind: "all"}})
	f.c.do(viewer, http.MethodPost, "/locations:batchCreate", map[string]any{"requests": many[:1]}, http.StatusForbidden)
}
