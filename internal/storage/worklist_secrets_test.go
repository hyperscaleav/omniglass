package storage_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestNodeWorklistResolvesEachComponentsOwnSecret pins the confused-deputy
// guard of the set-based secret resolve (#1581): one node pulls endpoints on
// components of every cascade shape, every one referencing a secret by the
// same name, and the one statement that resolves them all hands each component
// the secret its own chain owns, never a sibling's. A shape whose chain owns
// none of the name, or more than one, delivers absence.
func TestNodeWorklistResolvesEachComponentsOwnSecret(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t), storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	node, drv := "edge-s", "snmp-scalar"
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: node}, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: drv, Label: "SNMP Scalar", Version: "1.0.0", Spec: []byte(snmpSpec)}); err != nil {
		t.Fatal(err)
	}
	// Two sites: site-a > bldg-a > room-a, site-b > bldg-b > room-b.
	for _, l := range []storage.LocationSpec{
		{Name: "site-a", LocationType: "campus"},
		{Name: "bldg-a", LocationType: "building", ParentName: strptr("site-a")},
		{Name: "room-a", LocationType: "room", ParentName: strptr("bldg-a")},
		{Name: "site-b", LocationType: "campus"},
		{Name: "bldg-b", LocationType: "building", ParentName: strptr("site-b")},
		{Name: "room-b", LocationType: "room", ParentName: strptr("bldg-b")},
	} {
		if _, err := gw.CreateLocation(ctx, "", l, all); err != nil {
			t.Fatalf("create location %s: %v", l.Name, err)
		}
	}
	for _, c := range []storage.ComponentSpec{
		{Name: "cmp-own"},    // owns its secret
		{Name: "cmp-parent"}, // owns the secret its child inherits
		{Name: "cmp-child", ParentName: strptr("cmp-parent")}, // inherits down the component tree
		{Name: "cmp-bldg", LocationName: strptr("room-b")},    // inherits from its building
		{Name: "cmp-site", LocationName: strptr("room-a")},    // inherits the site default
		{Name: "cmp-clash", LocationName: strptr("room-b")},   // owns one and inherits one: ambiguous
		{Name: "cmp-bare"},  // its chain owns none: a sibling's never reaches it
		{Name: "cmp-fleet"}, // a platform secret by another name
		{Name: "cmp-gone"},  // references a secret since deleted
	} {
		if _, err := gw.CreateComponent(ctx, "", c, all, all, all, all); err != nil {
			t.Fatalf("create component %s: %v", c.Name, err)
		}
	}
	platformSecret := func(name, value string) {
		t.Helper()
		if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{Name: name, SecretType: "snmp-community", OwnerKind: "platform",
			Fields: map[string]string{"community": value}}, all, true); err != nil {
			t.Fatalf("create platform secret %s: %v", name, err)
		}
	}
	// The attach refuses a name more than one visible secret shares, so every
	// endpoint attaches against a platform placeholder that is then replaced by
	// the shaped secrets the attach would have refused.
	platformSecret("cred", "placeholder")
	platformSecret("fleet", "fleet-wide")
	platformSecret("gone", "deleted")
	refOf := map[string]string{"cmp-fleet": "fleet", "cmp-gone": "gone"}
	owner := map[string]string{} // endpoint id -> component
	for _, comp := range []string{"cmp-own", "cmp-parent", "cmp-child", "cmp-bldg", "cmp-site", "cmp-clash", "cmp-bare", "cmp-fleet", "cmp-gone"} {
		ref := refOf[comp]
		if ref == "" {
			ref = "cred"
		}
		ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Driver: &drv, Component: &comp, Node: &node,
			Inputs: map[string]string{"host": "10.20.4.40", "community": ref}, SecretRead: all, CanAdmin: true}, all)
		if err != nil {
			t.Fatalf("attach %s: %v", comp, err)
		}
		owner[ep.ID] = comp
	}
	for _, name := range []string{"cred", "gone"} {
		if err := gw.DeleteSecret(ctx, "", name, all, all, true, true); err != nil {
			t.Fatalf("delete secret %s: %v", name, err)
		}
	}
	for _, s := range []struct{ kind, owner, value string }{
		{"component", "cmp-own", "own"},
		{"component", "cmp-parent", "parent"},
		{"location", "bldg-b", "bldg-b"},
		{"location", "site-a", "site-a"},
		{"component", "cmp-clash", "clash"},
	} {
		if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{Name: "cred", SecretType: "snmp-community", OwnerKind: s.kind, OwnerName: strptr(s.owner),
			Fields: map[string]string{"community": s.value}}, all, true); err != nil {
			t.Fatalf("create %s secret on %s: %v", s.kind, s.owner, err)
		}
	}

	wl, err := gw.NodeWorklist(ctx, node)
	if err != nil {
		t.Fatalf("worklist: %v", err)
	}
	want := map[string]string{ // component -> the community it is delivered, "" for absence
		"cmp-own":    "own",
		"cmp-parent": "parent",
		"cmp-child":  "parent",
		"cmp-bldg":   "bldg-b",
		"cmp-site":   "site-a",
		"cmp-clash":  "",
		"cmp-bare":   "",
		"cmp-fleet":  "fleet-wide",
		"cmp-gone":   "",
	}
	seen := map[string]bool{}
	for _, task := range wl.Tasks {
		comp, ok := owner[task.EndpointID]
		if !ok {
			t.Fatalf("task %s rides endpoint %s, which the test never created", task.ID, task.EndpointID)
		}
		seen[comp] = true
		if want[comp] == "" {
			if task.Secrets != nil {
				t.Errorf("%s task %s carried %v, want absence", comp, task.ID, task.Secrets)
			}
			continue
		}
		if got := task.Secrets["community"]["community"]; got != want[comp] || len(task.Secrets) != 1 {
			t.Errorf("%s task %s carried %v, want community %q alone", comp, task.ID, task.Secrets, want[comp])
		}
	}
	if len(seen) != len(want) {
		t.Errorf("worklist carried tasks for %d components, want %d: %v", len(seen), len(want), seen)
	}
}
