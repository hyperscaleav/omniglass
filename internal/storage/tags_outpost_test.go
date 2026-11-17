package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestOutpostTags covers outpost as a taggable owner kind (N2): the applies_to gate,
// the all-scope requirement (an outpost is fleet-wide), effective tags = platform +
// outpost-direct (direct wins), the direct-only list, and unbind. The ON DELETE
// CASCADE of an outpost's bindings is declared in the migration and exercised by
// DeleteOutpost (N3).
func TestOutpostTags(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	gw := tagGateway(t)
	ctx := context.Background()

	outpost, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{Name: "edge-1"}, all, all)
	if err != nil {
		t.Fatalf("create outpost: %v", err)
	}

	mustTag(t, gw, "environment", nil, true)                  // applies to all kinds, cascades
	mustTag(t, gw, "rack", []string{"outpost"}, false)        // outpost-only, flat
	mustTag(t, gw, "room-only", []string{"component"}, false) // NOT allowed on an outpost

	// applies_to gate: a component-only key cannot bind to an outpost.
	if _, err := gw.SetTagBinding(ctx, "", "room-only", "outpost", strptr("edge-1"), "x", all, all); !errors.Is(err, storage.ErrTagKindNotAllowed) {
		t.Fatalf("bind component-only key to outpost: want ErrTagKindNotAllowed, got %v", err)
	}
	// Fleet-wide: an outpost bind without all-scope is forbidden; an unknown outpost is not found.
	if _, err := gw.SetTagBinding(ctx, "", "rack", "outpost", strptr("edge-1"), "r7", scope.Set{}, scope.Set{}); !errors.Is(err, storage.ErrOutpostForbidden) {
		t.Fatalf("bind without all-scope: want ErrOutpostForbidden, got %v", err)
	}
	if _, err := gw.SetTagBinding(ctx, "", "rack", "outpost", strptr("ghost"), "r7", all, all); !errors.Is(err, storage.ErrOutpostNotFound) {
		t.Fatalf("bind unknown outpost: want ErrOutpostNotFound, got %v", err)
	}

	// Bind: a platform environment, an outpost-direct override, and an outpost-only key.
	mustBind(t, gw, "environment", "platform", nil, "prod")
	mustBind(t, gw, "environment", "outpost", strptr("edge-1"), "edge-prod")
	mustBind(t, gw, "rack", "outpost", strptr("edge-1"), "r7")

	eff, err := gw.EffectiveTags(ctx, "outpost", []string{outpost.PrincipalID})
	if err != nil {
		t.Fatalf("effective outpost tags: %v", err)
	}
	m := eff[outpost.PrincipalID]
	if m["environment"] != "edge-prod" {
		t.Errorf("environment = %q, want edge-prod (outpost-direct wins over platform)", m["environment"])
	}
	if m["rack"] != "r7" {
		t.Errorf("rack = %q, want r7 (outpost-direct)", m["rack"])
	}

	// ListEntityTags returns only the outpost's direct bindings, not the platform cascade.
	binds, err := gw.ListEntityTags(ctx, "outpost", strptr("edge-1"), all)
	if err != nil {
		t.Fatalf("list outpost tags: %v", err)
	}
	if len(binds) != 2 {
		t.Fatalf("direct outpost bindings = %d, want 2 (environment, rack)", len(binds))
	}

	// Unbind rack: gone from the effective map.
	if err := gw.DeleteTagBinding(ctx, "", "rack", "outpost", strptr("edge-1"), all, all); err != nil {
		t.Fatalf("unbind rack: %v", err)
	}
	eff2, _ := gw.EffectiveTags(ctx, "outpost", []string{outpost.PrincipalID})
	if _, ok := eff2[outpost.PrincipalID]["rack"]; ok {
		t.Errorf("rack still effective after unbind")
	}
}
