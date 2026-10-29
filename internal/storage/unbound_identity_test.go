package storage_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestUnboundIdentityInbox: an identity a conduit's bindings do not place is
// kept, never dropped and never filed on the conduit as a sample (#1282,
// ADR-1282 point 7): one row per conduit, key and value, whose count grows
// with a steady stream, landed under the batch's receipt (a redelivery counts
// nothing twice), bounded per conduit with the least recently heard evicted,
// aged out after seven days, and read only inside the caller's component read.
func TestUnboundIdentityInbox(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newWriterFixture(t)
	all := scope.Set{All: true}
	if _, err := f.gw.CreatePropertyType(f.ctx, "", storage.PropertyTypeSpec{Name: "test-room-id", DataType: "string"}); err != nil {
		t.Fatalf("create the match key: %v", err)
	}
	comp, err := f.gw.GetComponent(f.ctx, "disp-1", all)
	if err != nil {
		t.Fatalf("get conduit: %v", err)
	}
	t0 := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	land := func(id string, us ...storage.UnboundIdentityWrite) {
		t.Helper()
		if _, err := f.gw.LandBatch(f.ctx, storage.IngestBatch{BatchID: id, Writer: storage.PushWriter(), Unbound: us}); err != nil {
			t.Fatalf("land %s: %v", id, err)
		}
	}
	heard := func(value string, n int64, at time.Time, keys ...string) storage.UnboundIdentityWrite {
		return storage.UnboundIdentityWrite{ConduitID: comp.ID, Key: "test-room-id", Value: value, Count: n, FirstAt: at, LastAt: at, Keys: keys}
	}

	land("b1", heard("r-9", 2, t0, "call-state"))
	land("b2", heard("r-9", 3, t0.Add(10*time.Second), "call-started", "call-state"))
	land("b2", heard("r-9", 3, t0.Add(10*time.Second), "call-started")) // a redelivery
	got, err := f.gw.ListUnboundIdentities(f.ctx, "disp-1", all)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("a steady stream of one identity reads as %d rows, want one: %+v", len(got), got)
	}
	u := got[0]
	if u.Key != "test-room-id" || u.Value != "r-9" || u.Count != 5 {
		t.Errorf("row = %+v, want test-room-id r-9 heard 5 times (the redelivery counted once)", u)
	}
	if !u.FirstHeardAt.Equal(t0) || !u.LastHeardAt.Equal(t0.Add(10*time.Second)) {
		t.Errorf("heard %v to %v, want %v to %v", u.FirstHeardAt, u.LastHeardAt, t0, t0.Add(10*time.Second))
	}
	if fmt.Sprint(u.Keys) != "[call-started call-state]" {
		t.Errorf("keys carried = %v, want the union, sorted", u.Keys)
	}
	if n := f.count(t, `select count(*) from property`); n != 0 {
		t.Errorf("an unbound identity landed %d property rows; it lands on no owner", n)
	}

	// Bounded per conduit: the least recently heard evicted.
	var many []storage.UnboundIdentityWrite
	for i := range storage.UnboundBound + 5 {
		many = append(many, heard(fmt.Sprintf("v-%04d", i), 1, t0.Add(time.Duration(i+20)*time.Second)))
	}
	land("b3", many...)
	if n := f.count(t, `select count(*) from unbound_identity where conduit_id = $1`, comp.ID); n != storage.UnboundBound {
		t.Errorf("the inbox holds %d rows for one conduit, want the bound %d", n, storage.UnboundBound)
	}
	if n := f.count(t, `select count(*) from unbound_identity where value in ('r-9', 'v-0000')`); n != 0 {
		t.Errorf("the least recently heard were not evicted (%d left)", n)
	}

	// Aged out: a row unheard for past the window is not read, and the next
	// write lets it go.
	if _, err := f.conn.Exec(f.ctx, `update unbound_identity set last_heard_at = now() - interval '8 days' where value = 'v-0010'`); err != nil {
		t.Fatalf("age a row: %v", err)
	}
	got, err = f.gw.ListUnboundIdentities(f.ctx, comp.ID, all)
	if err != nil {
		t.Fatalf("list by id: %v", err)
	}
	for _, r := range got {
		if r.Value == "v-0010" {
			t.Error("a row unheard for eight days is still read")
		}
	}
	land("b4", heard("v-new", 1, time.Now().UTC()))
	if n := f.count(t, `select count(*) from unbound_identity where value = 'v-0010'`); n != 0 {
		t.Error("an aged row survived the next write")
	}

	// Scope: a conduit outside the caller's read is the non-disclosing miss.
	none := scope.Set{}
	if _, err := f.gw.ListUnboundIdentities(f.ctx, "disp-1", none); !errors.Is(err, storage.ErrComponentNotFound) {
		t.Errorf("a conduit outside read scope: err = %v, want ErrComponentNotFound", err)
	}
}

// TestTheInboxNamesKeysAcrossARename: an inbox row records the keys its
// samples carried by type id, so renaming a key's type lists it under its
// new name (#1188 makes telemetry types renameable).
func TestTheInboxNamesKeysAcrossARename(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newWriterFixture(t)
	all := scope.Set{All: true}
	for _, name := range []string{"test-room-id", "room-mood"} {
		if _, err := f.gw.CreatePropertyType(f.ctx, "", storage.PropertyTypeSpec{Name: name, DataType: "string"}); err != nil {
			t.Fatal(err)
		}
	}
	comp, err := f.gw.GetComponent(f.ctx, "disp-1", all)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := f.gw.LandBatch(f.ctx, storage.IngestBatch{BatchID: "k1", Writer: storage.PushWriter(), Unbound: []storage.UnboundIdentityWrite{
		{ConduitID: comp.ID, Key: "test-room-id", Value: "r-1", Count: 1, FirstAt: now, LastAt: now, Keys: []string{"room-mood", "uptime"}},
	}}); err != nil {
		t.Fatal(err)
	}
	// Telemetry types are not renameable through the API yet (#1188); the
	// row's name moves here as that rename will move it, by its uuid.
	if _, err := f.conn.Exec(f.ctx, `update property_type set name = 'room-feeling' where name = 'room-mood'`); err != nil {
		t.Fatalf("rename the key's type: %v", err)
	}
	got, err := f.gw.ListUnboundIdentities(f.ctx, "disp-1", all)
	if err != nil || len(got) != 1 {
		t.Fatalf("inbox = %+v (%v)", got, err)
	}
	if fmt.Sprint(got[0].Keys) != "[room-feeling uptime]" {
		t.Errorf("keys carried = %v, want the renamed type under its new name beside uptime", got[0].Keys)
	}
}
