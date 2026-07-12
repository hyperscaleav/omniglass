package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// variableGateway opens a plain Gateway (no secret provider needed: variables are
// plaintext) and seeds the reference data.
func variableGateway(t *testing.T) storage.Gateway {
	t.Helper()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return gw
}

// TestVariableCreateRoundTrip covers the jsonb value surviving create -> store ->
// list for each scalar shape, typed by value_type.
func TestVariableCreateRoundTrip(t *testing.T) {
	gw := variableGateway(t)
	ctx := context.Background()

	mustVar(t, gw, "poll_interval", "int", "global", nil, `30`)
	mustVar(t, gw, "base_url", "string", "global", nil, `"https://api.example"`)
	mustVar(t, gw, "opts", "json", "global", nil, `{"retries":3,"backoff":"1s"}`)

	list, err := gw.ListVariables(ctx, all)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("list = %d, want 3", len(list))
	}
	byName := map[string]storage.Variable{}
	for _, v := range list {
		byName[v.Name] = v
	}
	if byName["poll_interval"].ValueType != "int" || string(byName["poll_interval"].Value) != "30" {
		t.Errorf("poll_interval = %+v, want int 30", byName["poll_interval"])
	}
	// The json object round-trips (compare structurally, key order is not fixed).
	var got, want map[string]any
	_ = json.Unmarshal(byName["opts"].Value, &got)
	_ = json.Unmarshal([]byte(`{"retries":3,"backoff":"1s"}`), &want)
	if got["retries"] != want["retries"] || got["backoff"] != want["backoff"] {
		t.Errorf("opts value = %s, want the object round-tripped", byName["opts"].Value)
	}
}

// TestVariableValueValidation covers the create gate: an unknown value_type and a
// value that does not match its declared type are both refused before any write.
func TestVariableValueValidation(t *testing.T) {
	gw := variableGateway(t)
	ctx := context.Background()

	if _, err := gw.CreateVariable(ctx, "", storage.VariableSpec{
		Name: "bad_type", ValueType: "date", OwnerKind: "global", Value: json.RawMessage(`"x"`),
	}, all); !errors.Is(err, storage.ErrUnknownValueType) {
		t.Errorf("unknown value_type = %v, want ErrUnknownValueType", err)
	}
	if _, err := gw.CreateVariable(ctx, "", storage.VariableSpec{
		Name: "bad_val", ValueType: "int", OwnerKind: "global", Value: json.RawMessage(`"not-an-int"`),
	}, all); !errors.Is(err, storage.ErrVariableValueInvalid) {
		t.Errorf("mismatched value = %v, want ErrVariableValueInvalid", err)
	}
}

// TestVariableOwnerScope covers the owner-arc scope gate on create and the
// all-scope-only list.
func TestVariableOwnerScope(t *testing.T) {
	gw := variableGateway(t)
	ctx := context.Background()
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "rm", LocationType: "room"}, all); err != nil {
		t.Fatalf("seed location: %v", err)
	}

	// A global variable needs an all create scope.
	if _, err := gw.CreateVariable(ctx, "", storage.VariableSpec{
		Name: "g", ValueType: "int", OwnerKind: "global", Value: json.RawMessage(`1`),
	}, scope.Set{}); !errors.Is(err, storage.ErrVariableForbidden) {
		t.Errorf("global create without all = %v, want ErrVariableForbidden", err)
	}
	// An unknown owner name is a 422, not a 500.
	if _, err := gw.CreateVariable(ctx, "", storage.VariableSpec{
		Name: "l", ValueType: "int", OwnerKind: "location", OwnerName: strptr("ghost"), Value: json.RawMessage(`1`),
	}, all); !errors.Is(err, storage.ErrVariableOwnerNotFound) {
		t.Errorf("unknown owner = %v, want ErrVariableOwnerNotFound", err)
	}
	// A location-owned variable lands on the location arc.
	locVar, err := gw.CreateVariable(ctx, "", storage.VariableSpec{
		Name: "l", ValueType: "int", OwnerKind: "location", OwnerName: strptr("rm"), Value: json.RawMessage(`5`),
	}, all)
	if err != nil {
		t.Fatalf("create location variable: %v", err)
	}
	if locVar.OwnerKind != "location" || locVar.OwnerName != "rm" {
		t.Errorf("owner = %s/%s, want location/rm", locVar.OwnerKind, locVar.OwnerName)
	}
	// Duplicate name at the same owner is refused.
	if _, err := gw.CreateVariable(ctx, "", storage.VariableSpec{
		Name: "l", ValueType: "int", OwnerKind: "location", OwnerName: strptr("rm"), Value: json.RawMessage(`6`),
	}, all); !errors.Is(err, storage.ErrVariableExists) {
		t.Errorf("dup owner+name = %v, want ErrVariableExists", err)
	}
	// List is all-scope only.
	if _, err := gw.ListVariables(ctx, scope.Set{IDs: []string{"whatever"}}); !errors.Is(err, storage.ErrVariableForbidden) {
		t.Errorf("non-all list = %v, want ErrVariableForbidden", err)
	}
}

// TestVariableUpdate replaces a value, validating it against the fixed value_type.
func TestVariableUpdate(t *testing.T) {
	gw := variableGateway(t)
	ctx := context.Background()

	v := mustVar(t, gw, "poll_interval", "int", "global", nil, `30`)
	// A value that does not match the fixed type is refused.
	if _, err := gw.UpdateVariable(ctx, "", v.ID, json.RawMessage(`"nope"`), all, all); !errors.Is(err, storage.ErrVariableValueInvalid) {
		t.Errorf("update with bad value = %v, want ErrVariableValueInvalid", err)
	}
	updated, err := gw.UpdateVariable(ctx, "", v.ID, json.RawMessage(`60`), all, all)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if string(updated.Value) != "60" || updated.ValueType != "int" {
		t.Errorf("updated = %+v, want int 60", updated)
	}
}

// TestVariableCascadeResolve is the resolver: a name owned at several tiers
// resolves to the most-specific owner (highest band, then deepest), with the
// shadowed candidates returned too.
func TestVariableCascadeResolve(t *testing.T) {
	gw := variableGateway(t)
	ctx := context.Background()

	mustLoc(t, gw, "campus", "campus", nil)
	mustLoc(t, gw, "bldg", "building", strptr("campus"))
	mustLoc(t, gw, "room", "room", strptr("bldg"))
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "sys", SystemType: "meeting-room"}, all); err != nil {
		t.Fatalf("system: %v", err)
	}
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "codec-1", ComponentType: "codec", SystemName: strptr("sys"), LocationName: strptr("room"),
	}, all)
	if err != nil {
		t.Fatalf("component: %v", err)
	}

	mustVar(t, gw, "poll", "int", "global", nil, `10`)
	mustVar(t, gw, "poll", "int", "location", strptr("campus"), `20`)
	mustVar(t, gw, "poll", "int", "location", strptr("room"), `30`)
	mustVar(t, gw, "poll", "int", "system", strptr("sys"), `40`)
	mustVar(t, gw, "poll", "int", "component", strptr("codec-1"), `50`)

	resolved, err := gw.ResolveVariables(ctx, comp.ID, all)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(resolved) != 5 {
		t.Fatalf("resolved = %d, want 5", len(resolved))
	}
	winner := pickVarWinner(t, resolved)
	if winner.OwnerKind != "component" || string(winner.Value) != "50" {
		t.Errorf("winner = %s/%s, want component/50", winner.OwnerKind, winner.Value)
	}

	// Remove the component tier: the system tier wins.
	list, _ := gw.ListVariables(ctx, all)
	deleteVarByOwner(t, gw, list, "component")
	resolved, _ = gw.ResolveVariables(ctx, comp.ID, all)
	if w := pickVarWinner(t, resolved); w.OwnerKind != "system" {
		t.Errorf("winner after comp removed = %s, want system", w.OwnerKind)
	}

	// Remove the system tier: the deeper location (room) beats campus.
	list, _ = gw.ListVariables(ctx, all)
	deleteVarByOwner(t, gw, list, "system")
	resolved, _ = gw.ResolveVariables(ctx, comp.ID, all)
	if w := pickVarWinner(t, resolved); w.OwnerKind != "location" || w.OwnerName != "room" {
		t.Errorf("winner after system removed = %s/%s, want location/room", w.OwnerKind, w.OwnerName)
	}

	// A component outside the read scope does not disclose its cascade.
	if _, err := gw.ResolveVariables(ctx, comp.ID, scope.Set{IDs: []string{"00000000-0000-0000-0000-000000000000"}}); !errors.Is(err, storage.ErrComponentNotFound) {
		t.Errorf("out-of-scope resolve = %v, want ErrComponentNotFound", err)
	}
}

func mustVar(t *testing.T, gw storage.Gateway, name, valueType, ownerKind string, ownerName *string, value string) *storage.Variable {
	t.Helper()
	v, err := gw.CreateVariable(context.Background(), "", storage.VariableSpec{
		Name: name, ValueType: valueType, OwnerKind: ownerKind, OwnerName: ownerName, Value: json.RawMessage(value),
	}, all)
	if err != nil {
		t.Fatalf("variable %s@%s: %v", name, ownerKind, err)
	}
	return v
}

func deleteVarByOwner(t *testing.T, gw storage.Gateway, list []storage.Variable, ownerKind string) {
	t.Helper()
	for _, v := range list {
		if v.OwnerKind == ownerKind {
			if err := gw.DeleteVariable(context.Background(), "", v.ID, all, all); err != nil {
				t.Fatalf("delete %s: %v", v.ID, err)
			}
		}
	}
}

func pickVarWinner(t *testing.T, resolved []storage.ResolvedVariable) *storage.ResolvedVariable {
	t.Helper()
	for i := range resolved {
		if resolved[i].Winner {
			return &resolved[i]
		}
	}
	t.Fatalf("no winner in %d resolved", len(resolved))
	return nil
}
