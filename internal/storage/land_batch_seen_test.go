package storage_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/querycount"
)

// TestLandBatchStampsEndpointsSeenInItsTransaction pins the seen stamp's
// place (#1928): each endpoint that delivered a landed group (and the
// batch's own) is stamped by the batch's transaction, before its one
// commit, never a refused group's; and a redelivery, which lands nothing,
// still stamps, since the endpoint delivered it.
func TestLandBatchStampsEndpointsSeenInItsTransaction(t *testing.T) {
	t.Parallel()
	f := newGroupFixture(t)
	ctx := context.Background()
	endpoint := func(name, component string) string {
		t.Helper()
		var id string
		if err := f.conn.QueryRow(ctx, `insert into endpoint (name, transport, component, params)
			values ($1, 'tcp', $2::uuid, '{}'::jsonb) returning id`, name, component).Scan(&id); err != nil {
			t.Fatalf("endpoint %s: %v", name, err)
		}
		return id
	}
	epA, epB, epTop := endpoint("ep-a", f.disp1), endpoint("ep-b", f.disp2), endpoint("ep-top", f.disp1)
	stamped := func(id string) (time.Time, bool) {
		t.Helper()
		var ts time.Time
		err := f.conn.QueryRow(ctx, `select last_seen_at from endpoint_live where endpoint_id = $1::uuid`, id).Scan(&ts)
		return ts, err == nil
	}
	counter := querycount.New()
	gw, err := storage.NewPG(ctx, f.dsn, storage.WithQueryTracer(counter))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)

	ts := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	a := taskGroup("task-a", f.disp1, f.node, "on", ts)
	a.SeenEndpoint = epA
	b := taskGroup("task-b", f.disp2, f.node, "on", ts)
	b.Metrics[0].OwnerID = f.disp1 // not task-b's component: refused
	b.SeenEndpoint = epB
	batch := storage.IngestBatch{BatchID: "tick-seen", Writer: storage.NodeWriter("", ""), SeenEndpoint: epTop,
		Groups: []storage.IngestGroup{a, b}}

	counter.Reset()
	res, err := gw.LandBatch(ctx, batch)
	if err != nil || !res.Landed || len(res.Refused) != 1 {
		t.Fatalf("land = %+v, %v; want landed with task-b refused", res, err)
	}
	stmts := counter.Statements()
	stampAt, commitAt := -1, -1
	for i, s := range stmts {
		s = strings.Join(strings.Fields(s), " ")
		switch {
		case strings.Contains(s, "endpoint_live"):
			if stampAt >= 0 {
				t.Errorf("a second endpoint stamp at statement %d", i)
			}
			stampAt = i
		case s == "commit":
			if commitAt >= 0 {
				t.Errorf("a second commit at statement %d", i)
			}
			commitAt = i
		}
	}
	if stampAt < 0 || commitAt < 0 || stampAt > commitAt {
		t.Errorf("endpoint stamp at statement %d, commit at %d: want the stamp inside the batch's one transaction", stampAt, commitAt)
	}
	if _, ok := stamped(epA); !ok {
		t.Error("the landed group's endpoint is not stamped seen")
	}
	if _, ok := stamped(epTop); !ok {
		t.Error("the batch's own endpoint is not stamped seen")
	}
	if _, ok := stamped(epB); ok {
		t.Error("the refused group's endpoint is stamped seen")
	}

	if _, err := f.conn.Exec(ctx, `update endpoint_live set last_seen_at = last_seen_at - interval '1 minute'`); err != nil {
		t.Fatal(err)
	}
	before, _ := stamped(epA)
	again, err := gw.LandBatch(ctx, batch)
	if err != nil || again.Landed {
		t.Fatalf("redelivery = %+v, %v; want nothing landed", again, err)
	}
	if after, _ := stamped(epA); !after.After(before) {
		t.Errorf("a redelivery left the endpoint's stamp at %v, want it restamped", after)
	}
}
