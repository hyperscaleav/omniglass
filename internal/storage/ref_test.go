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

// A reference is either form. The uuid is the canonical handle and the name is
// the friendly alias, and a caller should be able to use whichever it happens to
// hold: a script that just created something has the id, a human typing into a
// CLI has the name.
//
// Resolution tries the uuid first, which is unambiguous because a name can no
// longer be uuid-shaped (#344). Without that rule this would resolve differently
// depending on which entity happened to exist.
func TestReferencesResolveByEitherForm(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	made, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "codec"}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	byName, err := gw.GetComponent(ctx, "codec", all)
	if err != nil {
		t.Fatalf("get by name: %v", err)
	}
	byID, err := gw.GetComponent(ctx, made.ID, all)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if byName.ID != byID.ID {
		t.Errorf("the two forms resolved to different rows: %s vs %s", byName.ID, byID.ID)
	}

	// A well-formed uuid that is nobody is an ordinary not-found, not a 500 and
	// not a fallback to a name lookup that would also miss.
	if _, err := gw.GetComponent(ctx, "00000000-0000-0000-0000-000000000000", all); !errors.Is(err, storage.ErrComponentNotFound) {
		t.Errorf("unknown uuid = %v, want ErrComponentNotFound", err)
	}
	if _, err := gw.GetComponent(ctx, "no-such-thing", all); !errors.Is(err, storage.ErrComponentNotFound) {
		t.Errorf("unknown name = %v, want ErrComponentNotFound", err)
	}
}

// A join field takes either form too, so the same placement can be expressed
// with whichever handle the caller has.
func TestJoinFieldsAcceptEitherForm(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	site, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "site", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("location: %v", err)
	}
	byName := "site"
	byID := site.ID
	a, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "a", LocationName: &byName}, all)
	if err != nil {
		t.Fatalf("create by name: %v", err)
	}
	b, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "b", LocationName: &byID}, all)
	if err != nil {
		t.Fatalf("create by id: %v", err)
	}
	if a.LocationID == nil || b.LocationID == nil || *a.LocationID != *b.LocationID {
		t.Errorf("the two forms placed the components differently: %v vs %v", a.LocationID, b.LocationID)
	}
}
