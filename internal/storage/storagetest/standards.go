package storagetest

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/template"
)

// CreateShippedStandards creates a standard from every standard template the binary ships into
// gw, parents before variants, through the same primitive POST
// /standards:createFromTemplate uses. The boot seed plants no standard (ADR-0827), so a
// test that builds a system on a shipped standard creates it first, as an
// operator would. Run it after seed.Run: a role names the component types
// and products the seed plants.
func CreateShippedStandards(t testing.TB, gw storage.Gateway) {
	t.Helper()
	if _, err := template.CreateAllStandards(context.Background(), gw, template.Shipped(), ""); err != nil {
		t.Fatalf("create the shipped standards: %v", err)
	}
}
