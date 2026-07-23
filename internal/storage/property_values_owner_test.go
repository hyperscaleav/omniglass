package storage_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestEffectivePropertiesByOwnerKind proves the resolver is owner-generic: a system
// resolves against its standard's contract and a location against its
// location_type's, with the same contract-default < instance-override < ad-hoc
// shape the component path already has. An instance with no classifier (a one-off
// system) resolves only what it sets directly.
func TestEffectivePropertiesByOwnerKind(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	// --- system: resolves against its standard's contract ---------------------

	if _, err := gw.SetStandardProperty(ctx, "", "huddle-room", storage.StandardPropertySpec{
		PropertyTypeName: "model_number", DefaultValue: json.RawMessage(`"HR-1"`),
	}); err != nil {
		t.Fatalf("declare on standard: %v", err)
	}
	std := "huddle-room"
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "hq-huddle", StandardID: &std}, all); err != nil {
		t.Fatalf("create system: %v", err)
	}

	sys := byName(mustResolveOwner(t, gw, "system", "hq-huddle", all))
	if got := sys["model_number"]; string(got.Value) != `"HR-1"` || got.IsSet || !got.FromContract {
		t.Fatalf("system inherits the standard default: want HR-1 unset from-contract, got %+v", got)
	}

	// The system's own value overrides the standard's default.
	if _, err := gw.SetProperty(ctx, "", "system", "hq-huddle", "model_number", "", json.RawMessage(`"HR-2"`), all); err != nil {
		t.Fatalf("override on system: %v", err)
	}
	sys = byName(mustResolveOwner(t, gw, "system", "hq-huddle", all))
	if got := sys["model_number"]; string(got.Value) != `"HR-2"` || !got.IsSet {
		t.Fatalf("system override: want HR-2 set, got %+v", got)
	}

	// A property the standard does not declare still resolves, flagged off-contract.
	if _, err := gw.SetProperty(ctx, "", "system", "hq-huddle", "serial_number", "", json.RawMessage(`"S-1"`), all); err != nil {
		t.Fatalf("ad-hoc on system: %v", err)
	}
	sys = byName(mustResolveOwner(t, gw, "system", "hq-huddle", all))
	if got := sys["serial_number"]; !got.IsSet || got.FromContract {
		t.Fatalf("system ad-hoc: want set and off-contract, got %+v", got)
	}

	// --- one-off system: no standard, so only what it sets directly -----------

	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "one-off"}, all); err != nil {
		t.Fatalf("create one-off system: %v", err)
	}
	if _, err := gw.SetProperty(ctx, "", "system", "one-off", "serial_number", "", json.RawMessage(`"S-9"`), all); err != nil {
		t.Fatalf("set on one-off: %v", err)
	}
	oneOff := mustResolveOwner(t, gw, "system", "one-off", all)
	if len(oneOff) != 1 || oneOff[0].PropertyTypeName != "serial_number" || oneOff[0].FromContract {
		t.Fatalf("one-off system: want a single ad-hoc serial_number, got %+v", oneOff)
	}

	// --- location: resolves against its location_type's contract --------------

	if _, err := gw.SetLocationTypeProperty(ctx, "", "room", storage.LocationTypePropertySpec{
		PropertyTypeName: "model_number", DefaultValue: json.RawMessage(`"ROOM"`), Required: true,
	}); err != nil {
		t.Fatalf("declare on location_type: %v", err)
	}
	// A room is not allowed at root (allowed_parent_types), so it hangs off a campus.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq-campus", LocationType: "campus"}, all); err != nil {
		t.Fatalf("create campus: %v", err)
	}
	campus := "hq-campus"
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq-room-1", LocationType: "room", ParentName: &campus}, all); err != nil {
		t.Fatalf("create location: %v", err)
	}
	loc := byName(mustResolveOwner(t, gw, "location", "hq-room-1", all))
	if got := loc["model_number"]; string(got.Value) != `"ROOM"` || got.IsSet || !got.FromContract || !got.Required {
		t.Fatalf("location inherits the type default: want ROOM unset from-contract required, got %+v", got)
	}

	// --- the component path is unchanged (the PR5 shape still holds) ----------

	if _, err := gw.SetProperty(ctx, "", "component", "ghost-x", "serial_number", "", json.RawMessage(`"x"`), all); err == nil {
		t.Fatal("unknown component owner: want a not-found error, got nil")
	}
}

func mustResolveOwner(t *testing.T, gw *storage.PG, kind, id string, s scope.Set) []storage.EffectiveProperty {
	t.Helper()
	got, err := gw.EffectiveProperties(context.Background(), kind, id, s)
	if err != nil {
		t.Fatalf("effective properties %s/%s: %v", kind, id, err)
	}
	return got
}
