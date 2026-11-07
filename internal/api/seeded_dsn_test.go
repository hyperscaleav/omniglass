package api_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// seededDSN is storagetest.NewDSN for a test that starts from the boot seed.
// Most of this package's tests open a gateway and run seed.Run before their
// first request, which was about 115 ms of every test's setup (#1540). The
// seed is the same for all of them, so it runs once into a derived template
// and each test clones that. A test that is about the seeding itself, or that
// needs an unseeded database, still calls storagetest.NewDSN and seeds (or not)
// on its own.
func seededDSN(t testing.TB) string {
	t.Helper()
	return storagetest.NewDSNFrom(t, "seeded", func(ctx context.Context, dsn string) error {
		gw, err := storage.NewPG(ctx, dsn)
		if err != nil {
			return err
		}
		defer gw.Close()
		return seed.Run(ctx, gw)
	})
}
