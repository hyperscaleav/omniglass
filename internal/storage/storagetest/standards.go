package storagetest

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/template"
)

// ForkShippedStandards forks every standard template the binary ships into
// gw, parents before variants, through the same primitive POST
// /standards:fork uses. The boot seed plants no standard (ADR-0827), so a
// test that builds a system on a shipped standard forks it first, as an
// operator would. Run it after seed.Run: a role names the component types
// and products the seed plants.
func ForkShippedStandards(t testing.TB, gw storage.Gateway) {
	t.Helper()
	if _, err := template.ForkAllStandards(context.Background(), gw, template.Shipped(), ""); err != nil {
		t.Fatalf("fork the shipped standards: %v", err)
	}
}
