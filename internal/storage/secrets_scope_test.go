package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestListSecretsScopeInjection pins the directory's per-row scope semantics
// across the owner arc, the behavior #458's scope-injecting rewrite must
// preserve exactly: a location-rooted read sees the location-owned secrets in
// its subtree and nothing in a sibling subtree; scope roots match only their
// own tier's tree (a location root does not reach a component-owned secret,
// the deliberate own-tier thin cut); a platform secret appears only to the all
// scope; a self root sees exactly its own row; and the admin-sensitive drop
// composes with scope instead of replacing it.
func TestListSecretsScopeInjection(t *testing.T) {
	gw, _ := secretGateway(t)
	ctx := context.Background()
	all := scope.Set{All: true}

	// Location forest: campus > bldg > room, and a sibling annex under campus.
	mustLoc(t, gw, "campus", "campus", nil)
	mustLoc(t, gw, "bldg", "building", strptr("campus"))
	mustLoc(t, gw, "room", "room", strptr("bldg"))
	mustLoc(t, gw, "annex", "building", strptr("campus"))
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "codec-1", LocationName: strptr("room"),
	}, all, all, all)
	if err != nil {
		t.Fatalf("component: %v", err)
	}

	mk := func(name, ownerKind string, ownerName *string, adminSensitive bool) {
		t.Helper()
		spec := storage.SecretSpec{
			Name: name, SecretType: "snmp-community", OwnerKind: ownerKind, OwnerName: ownerName,
			Fields: map[string]string{"community": "x"},
		}
		if adminSensitive {
			v := true
			spec.AdminSensitive = &v
		}
		if _, err := gw.CreateSecret(ctx, "", spec, all, true); err != nil {
			t.Fatalf("secret %s: %v", name, err)
		}
	}
	mk("in-room", "location", strptr("room"), false)
	mk("in-annex", "location", strptr("annex"), false)
	mk("on-codec", "component", strptr("codec-1"), false)
	mk("room-admin", "location", strptr("room"), true)
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "install-wide", SecretType: "snmp-community", OwnerKind: "platform",
		Fields: map[string]string{"community": "x"},
	}, all, true); err != nil {
		t.Fatalf("platform secret: %v", err)
	}

	bldg := mustLocID(t, gw, "bldg")
	names := func(set scope.Set, canAdmin bool) map[string]bool {
		t.Helper()
		got, err := gw.ListSecrets(ctx, set, canAdmin)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		out := map[string]bool{}
		for _, s := range got {
			out[s.Name] = true
		}
		return out
	}

	// A bldg-rooted read: the room location secrets, nothing in annex, no
	// component owner (own-tier), no platform row without all.
	got := names(scope.Set{IDs: []string{bldg}}, true)
	if !got["in-room"] || !got["room-admin"] || got["in-annex"] || got["on-codec"] || got["install-wide"] {
		t.Errorf("bldg-rooted list = %v, want in-room+room-admin only", got)
	}

	// The same read without the admin tier drops the admin-sensitive row.
	got = names(scope.Set{IDs: []string{bldg}}, false)
	if !got["in-room"] || got["room-admin"] {
		t.Errorf("bldg-rooted non-admin list = %v, want in-room without room-admin", got)
	}

	// A component-tier root reaches the component-owned secret and nothing else.
	got = names(scope.Set{IDs: []string{comp.ID}}, true)
	if !got["on-codec"] || len(got) != 1 {
		t.Errorf("component-rooted list = %v, want on-codec only", got)
	}

	// A self root sees exactly its own row, not the subtree below it.
	campus := mustLocID(t, gw, "campus")
	roomID := mustLocID(t, gw, "room")
	got = names(scope.Set{SelfIDs: []string{roomID}}, true)
	if !got["in-room"] || got["in-annex"] || got["install-wide"] {
		t.Errorf("self(room) list = %v, want in-room only among locations", got)
	}

	// subtree_excl_root: campus excluded as a root still admits its descendants.
	got = names(scope.Set{IDs: []string{campus}, ExcludeRootIDs: []string{campus}}, true)
	if !got["in-room"] || !got["in-annex"] || got["install-wide"] {
		t.Errorf("excl-root(campus) list = %v, want both location rows, no platform", got)
	}

	// The all scope sees everything.
	got = names(all, true)
	for _, want := range []string{"in-room", "in-annex", "on-codec", "room-admin", "install-wide"} {
		if !got[want] {
			t.Errorf("all list missing %s (got %v)", want, got)
		}
	}
}

// mustLocID resolves a location name to its id through the gateway read.
func mustLocID(t *testing.T, gw storage.Gateway, name string) string {
	t.Helper()
	loc, err := gw.GetLocation(context.Background(), name, scope.Set{All: true})
	if err != nil {
		t.Fatalf("get location %s: %v", name, err)
	}
	return loc.ID
}
