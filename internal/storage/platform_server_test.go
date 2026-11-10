package storage_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// serverComponent ensures the server named identity its component and reads
// it back.
func serverComponent(t *testing.T, ctx context.Context, gw *storage.PG, identity string) *storage.Component {
	t.Helper()
	id, err := gw.EnsureServerComponent(ctx, identity)
	if err != nil {
		t.Fatalf("ensure server component %q: %v", identity, err)
	}
	c, err := gw.GetComponent(ctx, id, all)
	if err != nil {
		t.Fatalf("read server %q's component: %v", identity, err)
	}
	return c
}

// TestEnsureServerComponent: a server's boot gives it one component in the
// platform system (#1627), of the server product, named after the server; a
// second boot of the same server, through a second gateway as a restart
// opens, finds the same component; a second server gets its own, minted past
// the first's name when both sanitize alike.
func TestEnsureServerComponent(t *testing.T) {
	t.Parallel()
	gw, ctx, dsn := platformGatewayDSN(t)
	first := serverComponent(t, ctx, gw, "Lab-Server.local")
	if first.Name != "lab-server-local" {
		t.Errorf("server component name = %q, want lab-server-local", first.Name)
	}
	if first.PrimarySystem == nil || *first.PrimarySystem != storage.PlatformSystemName {
		t.Errorf("server component's primary system = %v, want %s", first.PrimarySystem, storage.PlatformSystemName)
	}
	if first.ProductHandle == nil || *first.ProductHandle != storage.ServerProductName {
		t.Errorf("server component's product = %v, want %s", first.ProductHandle, storage.ServerProductName)
	}

	again, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("reopen gateway: %v", err)
	}
	defer again.Close()
	if id, err := again.EnsureServerComponent(ctx, "Lab-Server.local"); err != nil || id != first.ID {
		t.Errorf("second boot = %q (%v), want the first boot's component %q", id, err, first.ID)
	}

	second := serverComponent(t, ctx, gw, "lab_server.local")
	if second.ID == first.ID || second.Name != "lab-server-local-2" {
		t.Errorf("a second server's component = %q %q, want its own, lab-server-local-2", second.ID, second.Name)
	}
}

// Servers booting at once under one identity leave one component.
func TestEnsureServerComponentSerializesConcurrentBoots(t *testing.T) {
	t.Parallel()
	gw, ctx := platformGateway(t)
	const boots = 8
	ids := make(chan string, boots)
	var wg sync.WaitGroup
	for range boots {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := gw.EnsureServerComponent(ctx, "ha-server")
			if err != nil {
				t.Errorf("concurrent boot: %v", err)
			}
			ids <- id
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		seen[id] = true
	}
	if len(seen) != 1 {
		t.Errorf("concurrent boots made %d components, want one", len(seen))
	}
}

// A database the boot seed has not given a platform system has nowhere to
// put a server's component: the step says so rather than inventing one.
func TestEnsureServerComponentNeedsThePlatform(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if _, err := gw.EnsureServerComponent(ctx, "lonely"); !errors.Is(err, storage.ErrPlatformSystemMissing) {
		t.Errorf("ensure on an unseeded database = %v, want ErrPlatformSystemMissing", err)
	}
}

// TestServerComponentIsReserved: the server's component is the server's, as a
// node's is the node's: an operator may relabel it and may not rename,
// delete, re-parent or lend it to another system.
func TestServerComponentIsReserved(t *testing.T) {
	t.Parallel()
	gw, ctx := platformGateway(t)
	c := serverComponent(t, ctx, gw, "reserved-server")
	if _, err := gw.RenameComponent(ctx, "", c.Name, "taken-over", all, all); !errors.Is(err, storage.ErrPlatformComponentReserved) {
		t.Errorf("rename = %v, want ErrPlatformComponentReserved", err)
	}
	if err := gw.DeleteComponent(ctx, "", c.Name, all, all); !errors.Is(err, storage.ErrPlatformComponentReserved) {
		t.Errorf("delete = %v, want ErrPlatformComponentReserved", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "hung", ParentName: &c.Name}, all, all, all, all); !errors.Is(err, storage.ErrPlatformComponentReserved) {
		t.Errorf("create under = %v, want ErrPlatformComponentReserved", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "borrower"}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	if err := gw.AddMember(ctx, "", "borrower", c.Name, all, all, all, all); !errors.Is(err, storage.ErrPlatformComponentReserved) {
		t.Errorf("lend to an operator system = %v, want ErrPlatformComponentReserved", err)
	}
	if err := gw.RemoveMember(ctx, "", storage.PlatformSystemName, c.Name, all, all, all, all); !errors.Is(err, storage.ErrPlatformMemberReserved) {
		t.Errorf("remove from the platform system = %v, want ErrPlatformMemberReserved", err)
	}
	label := "Primary Server"
	if _, err := gw.UpdateComponent(ctx, "", c.Name, storage.ComponentPatch{Label: &label}, all, all); err != nil {
		t.Errorf("relabel the server component: %v", err)
	}
}

// serverKeys is every key the server writes of itself.
var serverKeys = []string{writerclass.HTTPRequestCount, writerclass.HTTPRequestDuration, writerclass.HTTPRequestDurationMax,
	writerclass.DBSlowQueryCount, writerclass.DBPoolAcquired, writerclass.DBPoolIdle, writerclass.DBPoolTotal,
	writerclass.DBPoolMax, writerclass.DBPoolWaitCount, writerclass.DBPoolWaitDuration}

// TestServerKeysRefuseEveryOtherWriter: the server's keys land from the
// process and are refused from a node or a push, so no device or operator
// can forge the server's account of itself.
func TestServerKeysRefuseEveryOtherWriter(t *testing.T) {
	t.Parallel()
	gw, ctx := platformGateway(t)
	c := serverComponent(t, ctx, gw, "keyed-server")
	for _, key := range serverKeys {
		for name, w := range map[string]storage.KeyWriter{"node": storage.NodeWriter("", ""), "push": storage.PushWriter()} {
			err := gw.InsertMetricSamples(ctx, w, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: c.ID, Key: key, Instance: "GET /api/v1/x 2xx", Value: 1}})
			if !errors.Is(err, storage.ErrKeyWriterRefused) {
				t.Errorf("a %s writing %s = %v, want ErrKeyWriterRefused", name, key, err)
			}
		}
	}
	at := time.Now().UTC().Truncate(time.Millisecond)
	var rows []storage.PlatformMetric
	for i, key := range serverKeys {
		rows = append(rows, storage.PlatformMetric{ComponentID: c.ID, Key: key, Value: float64(i + 1), TS: at})
	}
	if err := gw.RecordPlatformMetrics(ctx, rows); err != nil {
		t.Fatalf("the process writing the server keys: %v", err)
	}
	for i, key := range serverKeys {
		if got, err := gw.LatestMetric(ctx, c.ID, key); err != nil || got == nil || got.Value != float64(i+1) {
			t.Errorf("%s = %+v (%v), want %d", key, got, err, i+1)
		}
	}
}

// A window flushed onto a server component gone since (a database restored
// under a running server) is dropped, never a failed report.
func TestServerWindowOnAMissingComponentIsDropped(t *testing.T) {
	t.Parallel()
	gw, ctx, dsn := platformGatewayDSN(t)
	gone := "00000000-0000-0000-0000-00000000f00d"
	err := gw.RecordPlatformMetrics(ctx, []storage.PlatformMetric{{ComponentID: gone, Key: writerclass.HTTPRequestCount, Instance: "GET /api/v1/x 2xx", Value: 3, TS: time.Now()}})
	if err != nil {
		t.Fatalf("a window on a missing component = %v, want dropped", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var series, samples int
	if err := conn.QueryRow(ctx, `select (select count(*) from metric_series), (select count(*) from metric)`).Scan(&series, &samples); err != nil {
		t.Fatalf("count: %v", err)
	}
	if series != 0 || samples != 0 {
		t.Errorf("a window on a missing component wrote %d series and %d samples, want none", series, samples)
	}
}

// TestServerComponentStem: a server's identity (a hostname, as often as not)
// becomes a legal component name stem: lowercased, every run of other
// characters one hyphen, bounded so the minted ordinal still fits.
func TestServerComponentStem(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"Lab-Server.local":                     "lab-server-local",
		"lab_server.local":                     "lab-server-local",
		"  --edge..01--  ":                     "edge-01",
		"7f3c9a1b2d4e":                         "7f3c9a1b2d4e",
		"!!!":                                  "server",
		"":                                     "server",
		"6a1e4c1e-1b0b-4f3a-9c55-0f2c3a4b5d6e": "server-6a1e4c1e-1b0b-4f3a-9c55-0f2c3a4b5d6e",
	} {
		if got := storage.ServerComponentStem(in); got != want {
			t.Errorf("ServerComponentStem(%q) = %q, want %q", in, got, want)
		}
		if err := storage.ValidateEntityName(storage.ServerComponentStem(in) + "-99"); err != nil {
			t.Errorf("a minted name from %q is illegal: %v", in, err)
		}
	}
	long := storage.ServerComponentStem(strings.Repeat("a", 300))
	if err := storage.ValidateEntityName(long + "-99"); err != nil {
		t.Errorf("a long identity mints an illegal name: %v", err)
	}
}
