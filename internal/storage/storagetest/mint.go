package storagetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// MintProduct creates a throwaway product classified under componentType and
// returns it. A behavior test that needs "a product classified display" mints
// one here instead of borrowing a seeded SKU, so the shipped catalog can
// change without touching it (#804). The name is random, so a collision is
// out of the ordinary and nothing can come to depend on it; the classification is the
// only fact the caller chose, and an empty componentType takes the gateway's
// own floor (the generic of the product's kind).
func MintProduct(t testing.TB, ctx context.Context, gw storage.Gateway, componentType string) storage.Product {
	t.Helper()
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("mint product name: %v", err)
	}
	suffix := hex.EncodeToString(b[:])
	p, err := gw.CreateProduct(ctx, "", storage.Product{
		Name:          "mint-" + suffix,
		Label:         "Mint " + suffix,
		ComponentType: componentType,
	})
	if err != nil {
		t.Fatalf("mint product (%s): %v", componentType, err)
	}
	return *p
}
