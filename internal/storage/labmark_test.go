package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The decision a server makes at boot about the database it was handed, by
// whether it runs as a lab and what the database holds.
func TestCheckLabMark(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		labMode, marked, holding bool
		want                     error
	}{
		{"a real server on a real database", false, false, true, nil},
		{"a real server on an empty database", false, false, false, nil},
		{"a real server on a lab's database", false, true, true, storage.ErrLabDatabase},
		{"a real server on a marked but empty database", false, true, false, storage.ErrLabDatabase},
		{"a lab on its own database", true, true, true, nil},
		{"a lab on a fresh database", true, false, false, nil},
		{"a lab on a real database", true, false, true, storage.ErrNotLabDatabase},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := storage.CheckLabMark(tc.labMode, tc.marked, tc.holding)
			if !errors.Is(err, tc.want) || (tc.want == nil) != (err == nil) {
				t.Fatalf("CheckLabMark(lab=%v, marked=%v, holding=%v) = %v, want %v", tc.labMode, tc.marked, tc.holding, err, tc.want)
			}
		})
	}
}

// Each refusal says why and what to do, in words an operator can act on.
func TestLabMarkRefusalsSayWhatToDo(t *testing.T) {
	for _, err := range []error{storage.ErrLabDatabase, storage.ErrNotLabDatabase} {
		msg := err.Error()
		for _, want := range []string{"lab", "database"} {
			if !strings.Contains(msg, want) {
				t.Errorf("%q does not mention %q", msg, want)
			}
		}
	}
}

// The mark round-trips: a fresh database is unmarked and holds no data; the
// lab marks it; marking again keeps the first mark; and an owner makes it hold
// data.
func TestLabMarkRoundTrip(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	st, err := gw.LabState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Marked || st.HoldsData {
		t.Fatalf("a fresh database: %+v, want unmarked and holding nothing", st)
	}
	if err := gw.MarkLab(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := gw.LabState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Marked || first.MarkedAt.IsZero() {
		t.Fatalf("after MarkLab: %+v, want marked with a time", first)
	}
	if err := gw.MarkLab(ctx); err != nil {
		t.Fatal(err)
	}
	again, err := gw.LabState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !again.MarkedAt.Equal(first.MarkedAt) {
		t.Fatalf("a second MarkLab moved the mark from %s to %s", first.MarkedAt, again.MarkedAt)
	}
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "owner", SecretHash: []byte("h"), Prefix: "p"}); err != nil {
		t.Fatal(err)
	}
	if st, err := gw.LabState(ctx); err != nil || !st.HoldsData {
		t.Fatalf("with an owner: %+v (%v), want it to hold data", st, err)
	}
}
