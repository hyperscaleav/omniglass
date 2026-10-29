package storage_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A service principal the trusted lane keeps (#1370: the lab's simulated
// pusher): ensuring it twice finds the same principal, and setting its token
// replaces the one before, so each lab run's freshly minted token is the only
// one that authenticates it. An unknown service is reported, not created.
func TestAServiceKeepsOneTokenTheTrustedLaneSets(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()

	id, err := gw.EnsureServicePrincipal(ctx, "", "sim-pusher")
	if err != nil {
		t.Fatal(err)
	}
	again, err := gw.EnsureServicePrincipal(ctx, "", "sim-pusher")
	if err != nil || again != id {
		t.Fatalf("ensuring it again = %q, %v; want the same principal %q", again, err, id)
	}

	first, second := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)
	for _, h := range [][]byte{first, second} {
		ok, err := gw.SetServiceToken(ctx, "", "sim-pusher", h, "0a0b0c0d")
		if err != nil || !ok {
			t.Fatalf("set token: %v, %v", ok, err)
		}
	}
	if _, err := gw.AuthenticateBearer(ctx, first); !errors.Is(err, storage.ErrCredentialNotFound) {
		t.Fatalf("the replaced token authenticates: %v", err)
	}
	pr, err := gw.AuthenticateBearer(ctx, second)
	if err != nil || pr.ID != id || pr.Kind != "service" || pr.Service == nil || pr.Service.Name != "sim-pusher" {
		t.Fatalf("the current token = %+v, %v; want the service sim-pusher", pr, err)
	}
	if ok, err := gw.SetServiceToken(ctx, "", "nobody", first, "0a0b0c0d"); err != nil || ok {
		t.Fatalf("a token for an unknown service: %v, %v; want false, nil", ok, err)
	}
}
