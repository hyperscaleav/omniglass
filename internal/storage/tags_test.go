package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// tagGateway opens a plain Gateway (tags are plaintext, no secret provider) and
// seeds the reference data.
func tagGateway(t *testing.T) storage.Gateway {
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

func mustTag(t *testing.T, gw storage.Gateway, name string, appliesTo []string, propagates bool) *storage.Tag {
	t.Helper()
	tg, err := gw.CreateTag(context.Background(), "", storage.TagSpec{
		Name: name, AppliesTo: appliesTo, Propagates: propagates,
	}, all)
	if err != nil {
		t.Fatalf("create tag %s: %v", name, err)
	}
	return tg
}

// TestTagRegistryCRUD covers minting keys, the all-scope gate, key/applies_to
// validation, the duplicate conflict, listing, and update.
func TestTagRegistryCRUD(t *testing.T) {
	gw := tagGateway(t)
	ctx := context.Background()

	// Minting needs an all-scope grant.
	if _, err := gw.CreateTag(ctx, "", storage.TagSpec{Name: "category"}, scope.Set{}); !errors.Is(err, storage.ErrTagForbidden) {
		t.Errorf("mint without all = %v, want ErrTagForbidden", err)
	}
	// A non-normalized key is refused before the write.
	if _, err := gw.CreateTag(ctx, "", storage.TagSpec{Name: "Environment"}, all); !errors.Is(err, storage.ErrTagKeyInvalid) {
		t.Errorf("bad key = %v, want ErrTagKeyInvalid", err)
	}
	// An unknown entity kind in applies_to is refused.
	if _, err := gw.CreateTag(ctx, "", storage.TagSpec{Name: "region", AppliesTo: []string{"planet"}}, all); !errors.Is(err, storage.ErrTagAppliesToInvalid) {
		t.Errorf("bad applies_to = %v, want ErrTagAppliesToInvalid", err)
	}

	cat := mustTag(t, gw, "category", []string{"component"}, true)
	if !cat.Propagates || len(cat.AppliesTo) != 1 || cat.AppliesTo[0] != "component" {
		t.Errorf("category = %+v, want propagates + applies_to component", cat)
	}
	mustTag(t, gw, "environment", nil, true)

	// Duplicate key name is a conflict.
	if _, err := gw.CreateTag(ctx, "", storage.TagSpec{Name: "category"}, all); !errors.Is(err, storage.ErrTagExists) {
		t.Errorf("dup key = %v, want ErrTagExists", err)
	}

	list, err := gw.ListTags(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].Name != "category" || list[1].Name != "environment" {
		t.Fatalf("list = %+v, want [category environment] ordered", list)
	}

	// Update the governance fields; the name is fixed.
	upd, err := gw.UpdateTag(ctx, "", "category", storage.TagSpec{AppliesTo: []string{"component", "system"}, Propagates: false}, all)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.Propagates || len(upd.AppliesTo) != 2 {
		t.Errorf("updated = %+v, want propagates=false, applies_to len 2", upd)
	}
	// Updating an unknown key is a non-disclosing not-found.
	if _, err := gw.UpdateTag(ctx, "", "ghost", storage.TagSpec{}, all); !errors.Is(err, storage.ErrTagNotFound) {
		t.Errorf("update ghost = %v, want ErrTagNotFound", err)
	}
}

// TestTagBindingLifecycle covers the applies_to gate, the value gate, upsert
// (replace), direct listing, and delete, all on a component binding.
func TestTagBindingLifecycle(t *testing.T) {
	gw := tagGateway(t)
	ctx := context.Background()
	seedTree(t, gw)

	mustTag(t, gw, "category", []string{"component"}, true)
	mustTag(t, gw, "rack_only", []string{"location"}, true) // does not apply to a component

	// A key that does not apply to the owner kind is refused.
	if _, err := gw.SetTagBinding(ctx, "", "rack_only", "component", strptr("codec-1"), "x", all, all); !errors.Is(err, storage.ErrTagKindNotAllowed) {
		t.Errorf("kind not allowed = %v, want ErrTagKindNotAllowed", err)
	}
	// An empty value is refused.
	if _, err := gw.SetTagBinding(ctx, "", "category", "component", strptr("codec-1"), "  ", all, all); !errors.Is(err, storage.ErrTagValueInvalid) {
		t.Errorf("empty value = %v, want ErrTagValueInvalid", err)
	}
	// Binding an unknown key is a not-found.
	if _, err := gw.SetTagBinding(ctx, "", "ghost", "component", strptr("codec-1"), "x", all, all); !errors.Is(err, storage.ErrTagNotFound) {
		t.Errorf("unknown key = %v, want ErrTagNotFound", err)
	}

	b, err := gw.SetTagBinding(ctx, "", "category", "component", strptr("codec-1"), "audio-dsp", all, all)
	if err != nil {
		t.Fatalf("set binding: %v", err)
	}
	if b.Key != "category" || b.Value != "audio-dsp" || b.OwnerKind != "component" || b.OwnerName != "codec-1" {
		t.Errorf("binding = %+v, want category=audio-dsp on component codec-1", b)
	}

	// Setting again replaces the value (upsert), not a duplicate.
	if _, err := gw.SetTagBinding(ctx, "", "category", "component", strptr("codec-1"), "video-dsp", all, all); err != nil {
		t.Fatalf("upsert binding: %v", err)
	}
	direct, err := gw.ListEntityTags(ctx, "component", strptr("codec-1"), all)
	if err != nil {
		t.Fatalf("list entity tags: %v", err)
	}
	if len(direct) != 1 || direct[0].Value != "video-dsp" {
		t.Fatalf("direct = %+v, want one binding valued video-dsp", direct)
	}
	if direct[0].OwnerName != "codec-1" {
		t.Errorf("direct binding owner_name = %q, want codec-1", direct[0].OwnerName)
	}

	// Delete the binding; the key survives.
	if err := gw.DeleteTagBinding(ctx, "", "category", "component", strptr("codec-1"), all, all); err != nil {
		t.Fatalf("delete binding: %v", err)
	}
	if again, _ := gw.ListEntityTags(ctx, "component", strptr("codec-1"), all); len(again) != 0 {
		t.Errorf("after delete = %+v, want none", again)
	}
	// Deleting an absent binding is a not-found.
	if err := gw.DeleteTagBinding(ctx, "", "category", "component", strptr("codec-1"), all, all); !errors.Is(err, storage.ErrTagBindingNotFound) {
		t.Errorf("delete absent = %v, want ErrTagBindingNotFound", err)
	}
}

// TestTagBindingScope covers the owner-update gate: binding on an entity outside
// the action scope is forbidden (readable) or not-found (unreadable), and a
// global binding needs an all-scope action.
func TestTagBindingScope(t *testing.T) {
	gw := tagGateway(t)
	ctx := context.Background()
	seedTree(t, gw)
	mustTag(t, gw, "environment", nil, true)

	// A read-only scope over the component: readable but not actionable -> forbidden.
	comp, err := gw.GetComponent(ctx, "codec-1", all)
	if err != nil {
		t.Fatalf("get component: %v", err)
	}
	readOnly := scope.Set{IDs: []string{comp.ID}}
	if _, err := gw.SetTagBinding(ctx, "", "environment", "component", strptr("codec-1"), "prod", readOnly, scope.Set{}); !errors.Is(err, storage.ErrComponentForbidden) {
		t.Errorf("bind out of action scope = %v, want ErrComponentForbidden", err)
	}
	// Unreadable component -> non-disclosing not-found.
	if _, err := gw.SetTagBinding(ctx, "", "environment", "component", strptr("codec-1"), "prod", scope.Set{}, scope.Set{}); !errors.Is(err, storage.ErrComponentNotFound) {
		t.Errorf("bind out of read scope = %v, want ErrComponentNotFound", err)
	}
	// A global binding needs an all-scope action.
	if _, err := gw.SetTagBinding(ctx, "", "environment", "global", nil, "prod", scope.Set{}, scope.Set{}); !errors.Is(err, storage.ErrTagForbidden) {
		t.Errorf("global bind without all = %v, want ErrTagForbidden", err)
	}
	if _, err := gw.SetTagBinding(ctx, "", "environment", "global", nil, "prod", all, all); err != nil {
		t.Fatalf("global bind with all: %v", err)
	}
}

// TestTagCascadeResolve is the resolver: keys union down the cascade while values
// override most-specific-wins, and a non-propagating key resolves only from the
// component itself.
func TestTagCascadeResolve(t *testing.T) {
	gw := tagGateway(t)
	ctx := context.Background()
	comp := seedTree(t, gw)

	mustTag(t, gw, "environment", nil, true) // cascades
	mustTag(t, gw, "asset_id", nil, false)   // flat, per-entity only

	// environment overridden most-specific-wins down the cascade.
	mustBind(t, gw, "environment", "global", nil, "prod")
	mustBind(t, gw, "environment", "location", strptr("campus"), "staging")
	mustBind(t, gw, "environment", "component", strptr("codec-1"), "dev")
	// asset_id bound above the component (should NOT resolve, it is non-propagating)
	// and directly on the component (should resolve).
	mustBind(t, gw, "asset_id", "location", strptr("campus"), "LOC-1")
	mustBind(t, gw, "asset_id", "component", strptr("codec-1"), "A-42")

	resolved, err := gw.ResolveTags(ctx, comp.ID, all)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	winners := map[string]storage.ResolvedTag{}
	for _, r := range resolved {
		if r.Winner {
			winners[r.Key] = r
		}
	}
	// environment: union produced one key, the component value wins over the
	// location and global candidates.
	if w := winners["environment"]; w.Value != "dev" || w.OwnerKind != "component" {
		t.Errorf("environment winner = %+v, want dev on component", w)
	}
	// asset_id: only the component binding resolves; the location one is dropped
	// by the non-propagating rule, so the component value wins uncontested.
	if w := winners["asset_id"]; w.Value != "A-42" || w.OwnerKind != "component" {
		t.Errorf("asset_id winner = %+v, want A-42 on component", w)
	}
	// The non-propagating location binding must not appear as a candidate at all.
	for _, r := range resolved {
		if r.Key == "asset_id" && r.OwnerKind == "location" {
			t.Errorf("non-propagating location binding leaked into resolve: %+v", r)
		}
	}

	// Drop the component environment binding: the deeper cascade (location) wins.
	if err := gw.DeleteTagBinding(ctx, "", "environment", "component", strptr("codec-1"), all, all); err != nil {
		t.Fatalf("delete env component binding: %v", err)
	}
	resolved, _ = gw.ResolveTags(ctx, comp.ID, all)
	for _, r := range resolved {
		if r.Key == "environment" && r.Winner && (r.OwnerKind != "location" || r.Value != "staging") {
			t.Errorf("environment winner after drop = %+v, want staging on location", r)
		}
	}

	// A component outside the read scope does not disclose its cascade.
	if _, err := gw.ResolveTags(ctx, comp.ID, scope.Set{IDs: []string{"00000000-0000-0000-0000-000000000000"}}); !errors.Is(err, storage.ErrComponentNotFound) {
		t.Errorf("out-of-scope resolve = %v, want ErrComponentNotFound", err)
	}
}

// TestTagDeleteCascadesBindings covers that deleting a key removes its bindings.
func TestTagDeleteCascadesBindings(t *testing.T) {
	gw := tagGateway(t)
	ctx := context.Background()
	seedTree(t, gw)
	mustTag(t, gw, "environment", nil, true)
	mustBind(t, gw, "environment", "component", strptr("codec-1"), "prod")

	if err := gw.DeleteTag(ctx, "", "environment", all); err != nil {
		t.Fatalf("delete tag: %v", err)
	}
	if binds, _ := gw.ListEntityTags(ctx, "component", strptr("codec-1"), all); len(binds) != 0 {
		t.Errorf("bindings after key delete = %+v, want none (cascade)", binds)
	}
}

// seedTree builds a campus/building/room location tree, a system, and a
// component under both, returning the component for resolver tests.
func seedTree(t *testing.T, gw storage.Gateway) *storage.Component {
	t.Helper()
	ctx := context.Background()
	mustLoc(t, gw, "campus", "campus", nil)
	mustLoc(t, gw, "bldg", "building", strptr("campus"))
	mustLoc(t, gw, "room", "room", strptr("bldg"))
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "sys"}, all); err != nil {
		t.Fatalf("system: %v", err)
	}
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "codec-1", SystemName: strptr("sys"), LocationName: strptr("room"),
	}, all)
	if err != nil {
		t.Fatalf("component: %v", err)
	}
	return comp
}

func mustBind(t *testing.T, gw storage.Gateway, key, ownerKind string, ownerName *string, value string) {
	t.Helper()
	if _, err := gw.SetTagBinding(context.Background(), "", key, ownerKind, ownerName, value, all, all); err != nil {
		t.Fatalf("bind %s@%s: %v", key, ownerKind, err)
	}
}
