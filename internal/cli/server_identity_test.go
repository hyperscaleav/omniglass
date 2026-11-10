package cli

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The server's identity against a real database (#1644): a boot writes the
// build it runs and when it started on its platform component, and a second
// boot of the same server, a restart through a fresh gateway, moves the start
// time forward and rewrites a changed version.
func TestServerBootWritesItsIdentity(t *testing.T) {
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	all := scope.Set{All: true}

	boot := func(version string, at time.Time) storage.PlatformServer {
		t.Helper()
		gw, err := storage.NewPG(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer gw.Close()
		if err := seed.Run(ctx, gw); err != nil {
			t.Fatal(err)
		}
		id, err := gw.EnsureServerComponent(ctx, "lab-server")
		if err != nil {
			t.Fatal(err)
		}
		bootServerIdentity(ctx, gw, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), id, version, at)
		v, err := gw.PlatformView(ctx, all, all, all)
		if err != nil {
			t.Fatalf("platform view: %v", err)
		}
		if len(v.Servers) != 1 || v.Servers[0].ComponentID != id {
			t.Fatalf("servers = %+v, want the one booted", v.Servers)
		}
		return v.Servers[0]
	}

	first := time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC)
	got := boot("v1.0.0", first)
	if got.Version == nil || *got.Version != "v1.0.0" || got.StartedAt == nil || !got.StartedAt.Equal(first) {
		t.Fatalf("first boot identity = %v started %v, want v1.0.0 started %s", got.Version, got.StartedAt, first)
	}

	second := first.Add(90 * time.Minute)
	got = boot("v1.1.0", second)
	if got.Version == nil || *got.Version != "v1.1.0" {
		t.Errorf("second boot version = %v, want the new build v1.1.0", got.Version)
	}
	if got.StartedAt == nil || !got.StartedAt.Equal(second) {
		t.Errorf("second boot started at = %v, want it moved forward to %s", got.StartedAt, second)
	}
}

// A boot whose component is gone logs the miss and carries on: the identity
// is a reading, never a reason to refuse to serve.
func TestServerIdentityOnAMissingComponentIsLogged(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	bootServerIdentity(ctx, gw, slog.New(slog.NewTextHandler(&buf, nil)), "00000000-0000-0000-0000-000000000001", "v1", time.Now())
	if !strings.Contains(buf.String(), "server identity") {
		t.Errorf("log = %q, want the identity write's miss logged", buf.String())
	}
}
