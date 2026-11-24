package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestASubtreeViewerReadsTheSystemsAndComponentsItsHealthNames is #1774: the
// shape devseed ships as `viewer-hq` (the viewer role, so `*:read`, on a
// location subtree) reads the systems and components placed under its
// location, list and get, and they are exactly the systems its location health
// roll-up names. Before #1498's create-and-read half (PR #1990) the health
// roll-up named a room's system while the system list was empty and the system
// itself 404ed, because a system and a component were contained by their own
// scope kind alone, so a location grant reached neither. The deploy shape that
// PR tested (tech-east) is a different role and scope op; this pins the
// read-only viewer's.
//
// The fleet: hq (campus) > west (building) > meeting-301 (room), and lab, a
// campus outside the grant. A system sits in the room and one in the building;
// a component sits in the room, one is placed by its primary system alone (no
// location of its own), and each tier has a twin in lab.
func TestASubtreeViewerReadsTheSystemsAndComponentsItsHealthNames(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
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

	for _, loc := range []map[string]any{
		{"name": "hq", "location_type": "campus"},
		{"name": "west", "location_type": "building", "parent": "hq"},
		{"name": "meeting-301", "location_type": "room", "parent": "west"},
		{"name": "lab", "location_type": "campus"},
	} {
		c.do(owner, http.MethodPost, "/locations", loc, http.StatusCreated)
	}
	for _, sys := range []map[string]any{
		{"name": "meeting", "system_type_id": "board", "location": "meeting-301"},
		{"name": "west-signage", "system_type_id": "board", "location": "west"},
		{"name": "lab-av", "system_type_id": "board", "location": "lab"},
	} {
		c.do(owner, http.MethodPost, "/systems", sys, http.StatusCreated)
	}
	for _, comp := range []map[string]any{
		{"name": "display", "product": "generic-device", "location": "meeting-301"},
		{"name": "codec", "product": "generic-device", "system": "meeting"},
		{"name": "lab-display", "product": "generic-device", "location": "lab"},
	} {
		c.do(owner, http.MethodPost, "/components", comp, http.StatusCreated)
	}

	// viewer-hq exactly as devseed seeds it: one viewer grant on the hq subtree
	// (the "" op is subtree, the root included), and nothing else.
	hqID := entityID(t, c, owner, "/locations", "hq")
	viewer := userWithGrants(t, ctx, dsn, "viewer-hq", []grant{{role: "viewer", scopeKind: "location", scopeID: hqID}})

	// AC1: the list is exactly what is placed under hq.
	wantSystems := []string{"meeting", "west-signage"}
	systems := listNames(t, c, viewer, "/systems")
	slices.Sort(systems)
	if !slices.Equal(systems, wantSystems) {
		t.Errorf("viewer-hq lists systems %v, want %v", systems, wantSystems)
	}
	components := listNames(t, c, viewer, "/components")
	slices.Sort(components)
	if want := []string{"codec", "display"}; !slices.Equal(components, want) {
		t.Errorf("viewer-hq lists components %v, want %v", components, want)
	}

	// AC1: each opens by its name and by its full dotted address.
	for _, path := range []string{
		"/systems/meeting",
		"/systems/hq.west.meeting-301.$sys.meeting",
		"/systems/west-signage",
		"/systems/hq.west.$sys.west-signage",
		"/components/display",
		"/components/hq.west.meeting-301.$comp.display",
		"/components/codec",
	} {
		c.do(viewer, http.MethodGet, path, nil, http.StatusOK)
	}

	// AC2: nothing outside hq, on the list (asserted above by equality) or the
	// get, by name or by address; the owner opens each, so every 404 is the
	// scope boundary rather than a bad path.
	for _, path := range []string{
		"/systems/lab-av",
		"/systems/lab.$sys.lab-av",
		"/components/lab-display",
		"/components/lab.$comp.lab-display",
		"/locations/lab",
	} {
		c.do(viewer, http.MethodGet, path, nil, http.StatusNotFound)
		c.do(owner, http.MethodGet, path, nil, http.StatusOK)
	}

	// AC3: the health roll-up, the list and the get agree. The room's roll-up
	// names the room's system, which the viewer just opened; hq's names exactly
	// the systems the viewer lists; and lab's is as absent as lab itself.
	healthSystems := func(path string) []string {
		t.Helper()
		var w healthWire
		if err := json.Unmarshal(c.do(viewer, http.MethodGet, path, nil, http.StatusOK), &w); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		names := make([]string, 0, len(w.Systems))
		for _, s := range w.Systems {
			names = append(names, s.Name)
		}
		slices.Sort(names)
		return names
	}
	if got, want := healthSystems("/locations/meeting-301/health"), []string{"meeting"}; !slices.Equal(got, want) {
		t.Errorf("meeting-301 health names systems %v, want %v", got, want)
	}
	if got := healthSystems("/locations/hq/health"); !slices.Equal(got, systems) {
		t.Errorf("hq health names systems %v, but the viewer lists %v", got, systems)
	}
	c.do(viewer, http.MethodGet, "/systems/meeting/health", nil, http.StatusOK)
	c.do(viewer, http.MethodGet, "/locations/lab/health", nil, http.StatusNotFound)
	c.do(viewer, http.MethodGet, "/systems/lab-av/health", nil, http.StatusNotFound)

	// The bulk health read the system list's health column draws on is keyed by
	// uuid and covers the same rows the list does.
	var bulk struct {
		Systems []struct {
			System string `json:"system"`
		} `json:"systems"`
	}
	if err := json.Unmarshal(c.do(viewer, http.MethodGet, "/systems:health", nil, http.StatusOK), &bulk); err != nil {
		t.Fatalf("decode bulk health: %v", err)
	}
	bulkIDs := make([]string, 0, len(bulk.Systems))
	for _, s := range bulk.Systems {
		bulkIDs = append(bulkIDs, s.System)
	}
	slices.Sort(bulkIDs)
	wantIDs := []string{entityID(t, c, owner, "/systems", "meeting"), entityID(t, c, owner, "/systems", "west-signage")}
	slices.Sort(wantIDs)
	if !slices.Equal(bulkIDs, wantIDs) {
		t.Errorf("viewer-hq bulk health covers %v, want the listed systems %v", bulkIDs, wantIDs)
	}
}
