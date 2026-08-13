package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// There are TWO type-chain walks in the gateway and this is the reason that is
// safe (#695).
//
// They exist for different reasons rather than by accident. ResolveTypeFacts
// reads one node at a time through a caller's own querier, because the name
// generator and the label renderer resolve facts INSIDE a transaction and have
// to see the write still in flight. ResolvedComponentTypeIcons is a pure pass
// over a listing the caller already holds, because resolving per row on a list
// read would turn one query into one per row per level.
//
// What must never differ is the ANSWER. The console reads the listing's icon
// and the write paths read the query walk's, so a disagreement between them is
// the same defect #695 closed across the language boundary, reopened inside one
// package. This runs both over the whole seeded registry, every row, and
// compares. It is the test that permits the duplication.
func TestBothTypeChainWalksAgreeOnEverySeededRow(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cts, err := gw.ListComponentTypes(ctx)
	if err != nil {
		t.Fatalf("ListComponentTypes: %v", err)
	}
	if len(cts) == 0 {
		t.Fatal("no seeded component types")
	}
	icons := storage.ResolvedComponentTypeIcons(cts)
	for _, ct := range cts {
		_, want, _, _, err := gw.ResolveTypeFacts(ctx, ct.ID)
		if err != nil {
			t.Fatalf("ResolveTypeFacts(%s): %v", ct.Name, err)
		}
		if got := icons[ct.ID]; got != want {
			t.Errorf("component_type %s: the listing resolves icon %q, the query walk resolves %q", ct.Name, got, want)
		}
	}

	sts, err := gw.ListSystemTypes(ctx)
	if err != nil {
		t.Fatalf("ListSystemTypes: %v", err)
	}
	if len(sts) == 0 {
		t.Fatal("no seeded system types")
	}
	sicons := storage.ResolvedSystemTypeIcons(sts)
	for _, st := range sts {
		_, want, _, err := gw.ResolveSystemTypeFacts(ctx, st.ID)
		if err != nil {
			t.Fatalf("ResolveSystemTypeFacts(%s): %v", st.Name, err)
		}
		if got := sicons[st.ID]; got != want {
			t.Errorf("system_type %s: the listing resolves icon %q, the query walk resolves %q", st.Name, got, want)
		}
	}
}
