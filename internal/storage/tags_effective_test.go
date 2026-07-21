package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// tagEstate builds a campus/building/room location tree, a system PLACED in the
// room, and a component under both the system and the room, returning their ids.
// The placed system is the point: it lets the resolver test that a system
// inherits its own location's tags.
func tagEstate(t *testing.T, gw storage.Gateway) (campus, room, sysID, compID string) {
	t.Helper()
	ctx := context.Background()
	mustLoc(t, gw, "campus", "campus", nil)
	mustLoc(t, gw, "bldg", "building", strptr("campus"))
	mustLoc(t, gw, "room", "room", strptr("bldg"))
	l, err := gw.GetLocation(ctx, "campus", all)
	if err != nil {
		t.Fatalf("get campus: %v", err)
	}
	r, err := gw.GetLocation(ctx, "room", all)
	if err != nil {
		t.Fatalf("get room: %v", err)
	}
	sys, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "av", SystemType: "meeting-room", LocationName: strptr("room")}, all)
	if err != nil {
		t.Fatalf("system: %v", err)
	}
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "codec", SystemName: strptr("av"), LocationName: strptr("room"),
	}, all)
	if err != nil {
		t.Fatalf("component: %v", err)
	}
	return l.ID, r.ID, sys.ID, comp.ID
}

// TestEffectiveTagsComponent covers the full four-band cascade for a component:
// union on key, override on value (system beats location beats global), and the
// non-propagating key resolving only from the component itself.
func TestEffectiveTagsComponent(t *testing.T) {
	gw := tagGateway(t)
	ctx := context.Background()
	_, _, _, compID := tagEstate(t, gw)

	mustTag(t, gw, "environment", nil, true)  // cascades
	mustTag(t, gw, "compliance", nil, true)   // cascades
	mustTag(t, gw, "asset_id", nil, false)    // non-propagating (flat)

	mustBind(t, gw, "environment", "global", nil, "prod")
	mustBind(t, gw, "environment", "location", strptr("campus"), "staging")
	mustBind(t, gw, "environment", "system", strptr("av"), "dev") // most specific band for this key
	mustBind(t, gw, "compliance", "location", strptr("campus"), "pci")
	mustBind(t, gw, "asset_id", "system", strptr("av"), "SYS-1")   // must NOT reach the component
	mustBind(t, gw, "asset_id", "component", strptr("codec"), "C-42")

	got, err := gw.EffectiveTags(ctx, "component", []string{compID})
	if err != nil {
		t.Fatalf("effective: %v", err)
	}
	m := got[compID]
	if m["environment"] != "dev" {
		t.Errorf("environment = %q, want dev (system band wins)", m["environment"])
	}
	if m["compliance"] != "pci" {
		t.Errorf("compliance = %q, want pci (inherited from campus location)", m["compliance"])
	}
	if m["asset_id"] != "C-42" {
		t.Errorf("asset_id = %q, want C-42 (own binding; the non-propagating system value must not reach here)", m["asset_id"])
	}
}

// TestEffectiveTagsSystem covers the design decision that a system inherits its
// own location's tags: global + its location tree + its system tree.
func TestEffectiveTagsSystem(t *testing.T) {
	gw := tagGateway(t)
	ctx := context.Background()
	_, _, sysID, _ := tagEstate(t, gw)

	mustTag(t, gw, "environment", nil, true)
	mustTag(t, gw, "compliance", nil, true)
	mustTag(t, gw, "asset_id", nil, false)

	mustBind(t, gw, "environment", "global", nil, "prod")
	mustBind(t, gw, "compliance", "location", strptr("campus"), "pci") // only at the location
	mustBind(t, gw, "asset_id", "location", strptr("campus"), "LOC-1") // non-propagating: must NOT reach the system
	mustBind(t, gw, "asset_id", "system", strptr("av"), "SYS-1")       // own binding: resolves

	got, err := gw.EffectiveTags(ctx, "system", []string{sysID})
	if err != nil {
		t.Fatalf("effective: %v", err)
	}
	m := got[sysID]
	if m["environment"] != "prod" {
		t.Errorf("environment = %q, want prod (global)", m["environment"])
	}
	if m["compliance"] != "pci" {
		t.Errorf("compliance = %q, want pci: a placed system inherits its location's tags", m["compliance"])
	}
	if m["asset_id"] != "SYS-1" {
		t.Errorf("asset_id = %q, want SYS-1 (own; the non-propagating location value must not reach the system)", m["asset_id"])
	}
}

// TestEffectiveTagsLocation covers a location resolving global + its own location
// tree, most-specific-wins.
func TestEffectiveTagsLocation(t *testing.T) {
	gw := tagGateway(t)
	ctx := context.Background()
	campusID, roomID, _, _ := tagEstate(t, gw)

	mustTag(t, gw, "environment", nil, true)

	mustBind(t, gw, "environment", "global", nil, "prod")
	mustBind(t, gw, "environment", "location", strptr("campus"), "staging")
	mustBind(t, gw, "environment", "location", strptr("room"), "lab")

	got, err := gw.EffectiveTags(ctx, "location", []string{campusID, roomID})
	if err != nil {
		t.Fatalf("effective: %v", err)
	}
	if got[campusID]["environment"] != "staging" {
		t.Errorf("campus environment = %q, want staging (own binding over global)", got[campusID]["environment"])
	}
	if got[roomID]["environment"] != "lab" {
		t.Errorf("room environment = %q, want lab (nearest location wins)", got[roomID]["environment"])
	}
}

// TestEffectiveTagsBatchSharedAncestor covers batching two components that share
// an ancestor location: each resolves independently and the shared ancestor does
// not trip a false cycle.
func TestEffectiveTagsBatchSharedAncestor(t *testing.T) {
	gw := tagGateway(t)
	ctx := context.Background()
	tagEstate(t, gw)
	// A second component in the same room, no system.
	comp2, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "display", LocationName: strptr("room"),
	}, all)
	if err != nil {
		t.Fatalf("component 2: %v", err)
	}
	comp1, err := gw.GetComponent(ctx, "codec", all)
	if err != nil {
		t.Fatalf("get codec: %v", err)
	}

	mustTag(t, gw, "environment", nil, true)
	mustBind(t, gw, "environment", "location", strptr("campus"), "staging") // shared ancestor
	mustBind(t, gw, "environment", "component", strptr("codec"), "dev")     // only codec overrides

	got, err := gw.EffectiveTags(ctx, "component", []string{comp1.ID, comp2.ID})
	if err != nil {
		t.Fatalf("effective batch: %v", err)
	}
	if got[comp1.ID]["environment"] != "dev" {
		t.Errorf("codec environment = %q, want dev (own override)", got[comp1.ID]["environment"])
	}
	if got[comp2.ID]["environment"] != "staging" {
		t.Errorf("display environment = %q, want staging (inherited from shared campus)", got[comp2.ID]["environment"])
	}
}

// TestEffectiveTagsEmptyAndUnknown covers the edge inputs.
func TestEffectiveTagsEmptyAndUnknown(t *testing.T) {
	gw := tagGateway(t)
	ctx := context.Background()

	got, err := gw.EffectiveTags(ctx, "component", nil)
	if err != nil || len(got) != 0 {
		t.Errorf("empty ids = (%v, %v), want empty map, nil", got, err)
	}
	if _, err := gw.EffectiveTags(ctx, "widget", []string{"00000000-0000-0000-0000-000000000000"}); err == nil {
		t.Errorf("unknown kind, want error")
	}
	// A well-formed id with no tags is simply absent from the map (not an error).
	got, err = gw.EffectiveTags(ctx, "component", []string{"00000000-0000-0000-0000-000000000000"})
	if err != nil {
		t.Fatalf("no-tag id: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("no-tag id map = %v, want empty", got)
	}
}
