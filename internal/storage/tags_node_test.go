package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestNodeTags covers node as a taggable owner kind (N2): the applies_to gate,
// the all-scope requirement (a node is estate-wide), effective tags = platform +
// node-direct (direct wins), the direct-only list, and unbind. The ON DELETE
// CASCADE of a node's bindings is declared in the migration and exercised by
// DeleteNode (N3).
func TestNodeTags(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	gw := tagGateway(t)
	ctx := context.Background()

	node, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-1"}, all)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}

	mustTag(t, gw, "environment", nil, true)                  // applies to all kinds, cascades
	mustTag(t, gw, "rack", []string{"node"}, false)           // node-only, flat
	mustTag(t, gw, "room_only", []string{"component"}, false) // NOT allowed on a node

	// applies_to gate: a component-only key cannot bind to a node.
	if _, err := gw.SetTagBinding(ctx, "", "room_only", "node", strptr("edge-1"), "x", all, all); !errors.Is(err, storage.ErrTagKindNotAllowed) {
		t.Fatalf("bind component-only key to node: want ErrTagKindNotAllowed, got %v", err)
	}
	// Estate-wide: a node bind without all-scope is forbidden; an unknown node is not found.
	if _, err := gw.SetTagBinding(ctx, "", "rack", "node", strptr("edge-1"), "r7", scope.Set{}, scope.Set{}); !errors.Is(err, storage.ErrNodeForbidden) {
		t.Fatalf("bind without all-scope: want ErrNodeForbidden, got %v", err)
	}
	if _, err := gw.SetTagBinding(ctx, "", "rack", "node", strptr("ghost"), "r7", all, all); !errors.Is(err, storage.ErrNodeNotFound) {
		t.Fatalf("bind unknown node: want ErrNodeNotFound, got %v", err)
	}

	// Bind: a platform environment, a node-direct override, and a node-only key.
	mustBind(t, gw, "environment", "platform", nil, "prod")
	mustBind(t, gw, "environment", "node", strptr("edge-1"), "edge-prod")
	mustBind(t, gw, "rack", "node", strptr("edge-1"), "r7")

	eff, err := gw.EffectiveTags(ctx, "node", []string{node.PrincipalID})
	if err != nil {
		t.Fatalf("effective node tags: %v", err)
	}
	m := eff[node.PrincipalID]
	if m["environment"] != "edge-prod" {
		t.Errorf("environment = %q, want edge-prod (node-direct wins over platform)", m["environment"])
	}
	if m["rack"] != "r7" {
		t.Errorf("rack = %q, want r7 (node-direct)", m["rack"])
	}

	// ListEntityTags returns only the node's direct bindings, not the platform cascade.
	binds, err := gw.ListEntityTags(ctx, "node", strptr("edge-1"), all)
	if err != nil {
		t.Fatalf("list node tags: %v", err)
	}
	if len(binds) != 2 {
		t.Fatalf("direct node bindings = %d, want 2 (environment, rack)", len(binds))
	}

	// Unbind rack: gone from the effective map.
	if err := gw.DeleteTagBinding(ctx, "", "rack", "node", strptr("edge-1"), all, all); err != nil {
		t.Fatalf("unbind rack: %v", err)
	}
	eff2, _ := gw.EffectiveTags(ctx, "node", []string{node.PrincipalID})
	if _, ok := eff2[node.PrincipalID]["rack"]; ok {
		t.Errorf("rack still effective after unbind")
	}
}
