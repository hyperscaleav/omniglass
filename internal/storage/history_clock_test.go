package storage_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// Every windowed history read is asserted against one property: the server
// process never names the boundary. Each of these reads filters a `ts` column
// stamped `default now()` by the DATABASE, so a boundary built from
// `time.Now()` would put two clocks on one comparison and the edge of the
// window would move with the skew between them (#719, ADR-0108).
//
// It is asserted at the seam rather than by outcome, deliberately and with the
// limit stated. Two clocks that agree produce the same rows as one clock, so no
// fixture can tell them apart on a box where the database and the process share
// a clock, which is every box this suite runs on. What CAN be observed is
// whether a timestamp crosses the seam at all: a process that sends no instant
// cannot be the one deciding the boundary, whatever its clock says. So the
// assertion is that the statement carries `now()` and binds no time.Time, which
// fails immediately against the construction this replaced (`ts >= $n` with a
// Go-computed argument) and cannot be satisfied by a read that computes the
// edge in Go.
func TestHistoryWindowsAreBoundedByTheDatabaseClock(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, counter := storagetest.NewCountingDB(t)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "clock-1", ProductName: strPtr("boreal-edge-55")}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	sys, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "clock-sys"}, all, all)
	if err != nil {
		t.Fatalf("create system: %v", err)
	}

	reads := []struct {
		name string
		run  func() error
	}{
		{"a system's health transitions", func() error {
			_, err := gw.SystemHealth(ctx, sys.ID, 30*24*time.Hour, all)
			return err
		}},
		{"a component's property transitions", func() error {
			_, err := gw.PropertyTransitions(ctx, comp.ID, "interface-reachable", "eth0", 24*time.Hour)
			return err
		}},
		{"a component's events", func() error {
			_, err := gw.ListComponentEvents(ctx, comp.ID, 24*time.Hour, 50)
			return err
		}},
		{"a component's logs", func() error {
			_, err := gw.ListComponentLogs(ctx, comp.ID, 24*time.Hour, 50)
			return err
		}},
		{"a node's logs", func() error {
			_, err := gw.ListNodeLogs(ctx, "clock-node", 24*time.Hour, 50)
			return err
		}},
	}
	for _, read := range reads {
		t.Run(read.name, func(t *testing.T) {
			counter.Reset()
			if err := read.run(); err != nil {
				t.Fatalf("run the read: %v", err)
			}
			var bounded int
			for _, call := range counter.Calls() {
				for _, arg := range call.Args {
					if ts, ok := arg.(time.Time); ok {
						t.Errorf("the read sent the boundary as an argument (%s), so the process decided it:\n\t%s",
							ts.Format(time.RFC3339Nano), strings.Join(strings.Fields(call.SQL), " "))
					}
				}
				if strings.Contains(call.SQL, "now() - make_interval") {
					bounded++
				}
			}
			if bounded != 1 {
				t.Errorf("%d of the read's statements bound ts against the database's clock, want exactly 1; the statements were:\n\t%s",
					bounded, strings.Join(counter.Summary(), "\n\t"))
			}
		})
	}
}

// A window of zero is the unwindowed read, and it must bind nothing at all
// rather than a boundary far enough in the past to look like none. Same
// property as above from the other side: the process names no instant, and here
// it names no interval either.
func TestAZeroWindowBindsNoBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, counter := storagetest.NewCountingDB(t)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "clock-2", ProductName: strPtr("boreal-edge-55")}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}

	counter.Reset()
	if _, err := gw.ListComponentEvents(ctx, comp.ID, 0, 50); err != nil {
		t.Fatalf("unwindowed events read: %v", err)
	}
	for _, call := range counter.Calls() {
		if strings.Contains(call.SQL, "make_interval") {
			t.Errorf("the unwindowed read still bound a window:\n\t%s", strings.Join(strings.Fields(call.SQL), " "))
		}
		for _, arg := range call.Args {
			if ts, ok := arg.(time.Time); ok {
				t.Errorf("the unwindowed read sent a boundary (%s)", ts.Format(time.RFC3339Nano))
			}
		}
	}
}
