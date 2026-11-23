package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

type appliedPlan struct {
	ImportID     string `json:"import_id"`
	ValidateOnly bool   `json:"validate_only"`
	Counts       struct {
		StandardsCreated int `json:"standards_created"`
		Locations        int `json:"locations"`
		Systems          int `json:"systems"`
		Components       int `json:"components"`
	} `json:"counts"`
	StandardsCreated []string `json:"standards_created"`
	Locations        []struct {
		Key        string `json:"key"`
		Location   string `json:"location"`
		LocationID string `json:"location_id"`
		Parent     string `json:"parent"`
	} `json:"locations"`
	Systems []struct {
		System     string `json:"system"`
		SystemID   string `json:"system_id"`
		Standard   string `json:"standard"`
		Components []struct {
			Component string `json:"component"`
			Slot      string `json:"slot"`
		} `json:"components"`
	} `json:"systems"`
	Components []struct {
		Component string `json:"component"`
	} `json:"components"`
}

// campusPlan is a plan of 500 rows: the meeting-room standard from its
// template, a building and 23 floors, 125 meeting rooms built to it (two per
// floor, give or take) each staffed with a video bar and a main display, and a
// declared input on 100 of those displays. 1 + 24 + 125 + 250 + 100 = 500.
func campusPlan(campus, bar, display string) map[string]any {
	locations := []map[string]any{{"key": "b", "name": campus, "type": "building"}}
	for i := range 23 {
		locations = append(locations, map[string]any{"key": fmt.Sprintf("f%d", i), "parent_key": "b", "name": fmt.Sprintf("floor-%d", i), "type": "floor"})
	}
	var systems []map[string]any
	for i := range 125 {
		screen := map[string]any{"slot": "main-display", "product": display}
		if i < 100 {
			screen["properties"] = []map[string]any{{"name": "video-input", "value": "hdmi1"}}
		}
		systems = append(systems, map[string]any{
			"location_key": fmt.Sprintf("f%d", i%23), "standard": "meeting-room", "type": "meeting",
			"components": []map[string]any{{"slot": "video-bar", "product": bar}, screen},
		})
	}
	return map[string]any{"standards": []string{"meeting-room"}, "locations": locations, "systems": systems}
}

// TestApplyPlanAPI (#2368): a 500-row plan dry-runs (nothing written, the
// plan reported), then applies whole, every audit row it wrote carrying the
// one import id the response returns.
func TestApplyPlanAPI(t *testing.T) {
	t.Parallel()
	f := newInstFixture(t)
	bar := storagetest.MintProduct(t, f.ctx, f.gw, "video-bar")
	disp := storagetest.MintProduct(t, f.ctx, f.gw, "display")
	rows := func() int {
		return f.count(`select count(*) from location`) + f.count(`select count(*) from system`) + f.count(`select count(*) from component`) +
			f.count(`select count(*) from standard`) + f.count(`select count(*) from property where provenance = 'declared'`) + f.count(`select count(*) from audit_log`)
	}

	plan := campusPlan("plan-campus", bar.Name, disp.Name)
	plan["validate_only"] = true
	before := rows()
	var dry appliedPlan
	if err := json.Unmarshal(f.c.do(f.owner, http.MethodPost, "/plans:apply", plan, http.StatusOK), &dry); err != nil {
		t.Fatal(err)
	}
	if n := rows(); n != before {
		t.Fatalf("a dry run wrote %d rows", n-before)
	}
	if !dry.ValidateOnly || dry.ImportID != "" || dry.Counts.StandardsCreated != 1 || dry.Counts.Locations != 24 || dry.Counts.Systems != 125 || dry.Counts.Components != 250 {
		t.Fatalf("dry run = %+v, want the whole plan reported and no import", dry.Counts)
	}
	if dry.Locations[0].Location != "plan-campus" || dry.Locations[1].Parent != "plan-campus" || dry.Locations[0].LocationID != "" || dry.Systems[0].System == "" || dry.Systems[0].SystemID != "" {
		t.Fatalf("dry run names = %+v / %+v, want planned names and no ids", dry.Locations[:2], dry.Systems[0])
	}

	delete(plan, "validate_only")
	auditBefore := f.count(`select count(*) from audit_log`)
	var got appliedPlan
	if err := json.Unmarshal(f.c.do(f.owner, http.MethodPost, "/plans:apply", plan, http.StatusCreated), &got); err != nil {
		t.Fatal(err)
	}
	if got.ImportID == "" || got.ValidateOnly || got.Counts != dry.Counts || strings.Join(got.StandardsCreated, ",") != "meeting-room" {
		t.Fatalf("apply = %+v, want the plan the dry run reported under an import", got)
	}
	if got.Systems[0].Standard != "meeting-room" || got.Systems[0].SystemID == "" || len(got.Systems[0].Components) != 2 || got.Systems[0].Components[1].Slot != "main-display" {
		t.Fatalf("first system = %+v", got.Systems[0])
	}
	if n := f.count(`select count(*) from component where location_id in (select id from location where parent_id = $1)`, got.Locations[0].LocationID); n != 250 {
		t.Fatalf("components on the campus floors = %d, want 250", n)
	}
	if n := f.count(`select count(*) from system_slot_assignment`); n != 250 {
		t.Fatalf("slot assignments = %d, want 250", n)
	}
	wrote := f.count(`select count(*) from audit_log`) - auditBefore
	stamped := f.count(`select count(*) from audit_log where new->>'from_import' = $1`, got.ImportID)
	if wrote < 500 || stamped != wrote {
		t.Fatalf("the apply wrote %d audit rows, %d under its import, want every one", wrote, stamped)
	}
}

// TestApplyPlanRefusesWhole (#2368): a failing piece rolls back the whole plan
// and is named by its place in it; a caller without create rights at a
// location is refused for the piece there; a plan over the row limit is a 413
// naming the limit; a caller missing a piece's permission is refused first.
func TestApplyPlanRefusesWhole(t *testing.T) {
	t.Parallel()
	f := newInstFixture(t)
	disp := storagetest.MintProduct(t, f.ctx, f.gw, "display")
	rows := func() int {
		return f.count(`select count(*) from location`) + f.count(`select count(*) from system`) + f.count(`select count(*) from component`) + f.count(`select count(*) from audit_log`)
	}
	before := rows()

	// The second standalone component names a product the fleet lacks: the
	// first location and component never land, and the detail names the row.
	bad := map[string]any{
		"locations": []map[string]any{{"key": "b", "name": "doomed", "type": "building"}},
		"components": []map[string]any{
			{"location_key": "b", "product": disp.Name},
			{"location_key": "b", "product": "no-such-product"},
		},
	}
	detail := string(f.c.do(f.owner, http.MethodPost, "/plans:apply", bad, http.StatusUnprocessableEntity))
	if !strings.Contains(detail, "components[1]") {
		t.Fatalf("refusal = %s, want it to name components[1]", detail)
	}
	bad["validate_only"] = true
	detail = string(f.c.do(f.owner, http.MethodPost, "/plans:apply", bad, http.StatusUnprocessableEntity))
	if !strings.Contains(detail, "components[1]") {
		t.Fatalf("dry-run refusal = %s, want it to name components[1]", detail)
	}
	if n := rows(); n != before {
		t.Fatalf("refused plans left %d rows", n-before)
	}

	// A key the plan does not hold, or one it holds twice, is the client's
	// mistake: a 422 naming the row, never a 500.
	for _, c := range []struct {
		body map[string]any
		path string
	}{
		{map[string]any{"locations": []map[string]any{{"key": "f", "parent_key": "b", "type": "floor"}, {"key": "b", "type": "building"}}}, "locations[0]"},
		{map[string]any{"systems": []map[string]any{{"location_key": "nope", "type": "meeting"}}}, "systems[0]"},
		{map[string]any{"components": []map[string]any{{"location_key": "nope", "product": disp.Name}}}, "components[0]"},
		{map[string]any{"locations": []map[string]any{{"key": "b", "name": "twice-a", "type": "building"}, {"key": "b", "name": "twice-b", "type": "building"}}}, "locations[1]"},
	} {
		if detail := string(f.c.do(f.owner, http.MethodPost, "/plans:apply", c.body, http.StatusUnprocessableEntity)); !strings.Contains(detail, c.path) {
			t.Fatalf("bad key refusal = %s, want it to name %s", detail, c.path)
		}
	}
	if n := rows(); n != before {
		t.Fatalf("plans with bad keys left %d rows", n-before)
	}

	// component:create reaches only annex: a component at hq is refused, and
	// the one the plan placed at annex before it rolls back with it.
	annexOnly := f.split("annex-placer", "annex")
	scoped := func(place string) map[string]any {
		return map[string]any{"components": []map[string]any{{"location": "annex", "product": disp.Name}, {"location": place, "product": disp.Name}}}
	}
	detail = string(f.c.do(annexOnly, http.MethodPost, "/plans:apply", scoped("hq"), http.StatusForbidden))
	if !strings.Contains(detail, "components[1]") {
		t.Fatalf("scope refusal = %s, want it to name components[1]", detail)
	}
	if n := rows(); n != before {
		t.Fatalf("a scope-refused plan left %d rows", n-before)
	}
	var ok appliedPlan
	if err := json.Unmarshal(f.c.do(annexOnly, http.MethodPost, "/plans:apply", scoped("annex"), http.StatusCreated), &ok); err != nil || ok.Counts.Components != 2 {
		t.Fatalf("an in-scope plan = %+v (%v), want two components", ok.Counts, err)
	}

	// Over the row limit is refused before anything is read, naming the limit.
	var many []map[string]any
	for i := range 1001 {
		many = append(many, map[string]any{"key": fmt.Sprintf("l%d", i), "type": "building"})
	}
	detail = string(f.c.do(f.owner, http.MethodPost, "/plans:apply", map[string]any{"locations": many}, http.StatusRequestEntityTooLarge))
	if !strings.Contains(detail, "1000") {
		t.Fatalf("over-limit refusal = %s, want it to name the limit", detail)
	}

	// A viewer passes the gate (location:read) but holds no location:create.
	viewer := userWithGrants(t, f.ctx, f.dsn, "plan-viewer", []grant{{role: "viewer", scopeKind: "all"}})
	detail = string(f.c.do(viewer, http.MethodPost, "/plans:apply", map[string]any{"locations": many[:1]}, http.StatusForbidden))
	if !strings.Contains(detail, "location:create") {
		t.Fatalf("viewer refusal = %s, want it to name location:create", detail)
	}
}
