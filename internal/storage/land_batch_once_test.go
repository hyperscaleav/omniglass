package storage_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/querycount"
)

// The statements a node's tick of eight task groups costs (#1928), each
// group five metric samples and two properties on its own component,
// delivered by its own endpoint. The batch takes each lock and reads each
// rule set once, in lockGroups, and every lane writer answers from what it
// took; before, the metric and property lanes each read the rule rows and
// marked calc rules again, and the property lane took its series locks a
// second time. The endpoints' seen stamp is one statement of the batch's
// own transaction, not a transaction of its own after it.
const (
	// maxSteadyBatchStatements is a tick whose properties are unchanged.
	maxSteadyBatchStatements = 15
	// maxJudgedBatchStatements is a tick whose endpoint-reachable flips on
	// every group, so the seeded rules reading it judge eight properties.
	maxJudgedBatchStatements = 80
)

func TestLandBatchTakesEachLockAndRuleReadOnce(t *testing.T) {
	t.Parallel()
	f := newGroupFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	const groups = 8
	ids, endpoints := make([]string, groups), make([]string, groups)
	for i := range ids {
		c, err := f.gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: fmt.Sprintf("once-disp-%d", i)}, all, all, all, all)
		if err != nil {
			t.Fatalf("create component: %v", err)
		}
		ids[i] = c.ID
		if err := f.conn.QueryRow(ctx, `insert into endpoint (name, transport, component, params)
			values ($1, 'tcp', $2::uuid, '{}'::jsonb) returning id`, fmt.Sprintf("ep-%d", i), c.ID).Scan(&endpoints[i]); err != nil {
			t.Fatalf("create endpoint: %v", err)
		}
	}
	counter := querycount.New()
	gw, err := storage.NewPG(ctx, f.dsn, storage.WithQueryTracer(counter))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	tick := func(id, reach string, ts time.Time) storage.IngestBatch {
		b := storage.IngestBatch{BatchID: id, Writer: storage.NodeWriter("", "")}
		for i := range groups {
			ep := fmt.Sprintf("ep-%d", i)
			w := storage.NodeWriter(ids[i], ep)
			w.Node = f.node
			g := storage.IngestGroup{TaskID: fmt.Sprintf("task-%d", i), Writer: w, SeenEndpoint: endpoints[i]}
			for j := range 5 {
				g.Metrics = append(g.Metrics, storage.MetricSampleWrite{OwnerKind: "component", OwnerID: ids[i],
					Key: "room-temperature", Instance: fmt.Sprintf("i%d", j), Value: 21.5, Source: "poll", TS: ts})
			}
			g.Properties = []storage.PropertySampleWrite{
				{OwnerKind: "component", OwnerID: ids[i], Key: "endpoint-reachable", Instance: ep, Value: reach, Source: "reachability", TS: ts},
				{OwnerKind: "component", OwnerID: ids[i], Key: "power-state", Value: "on", Source: "poll", TS: ts},
			}
			b.Groups = append(b.Groups, g)
		}
		return b
	}
	base := time.Now().UTC().Add(-10 * time.Minute)
	land := func(id, reach string, ts time.Time) []string {
		t.Helper()
		counter.Reset()
		res, err := gw.LandBatch(ctx, tick(id, reach, ts))
		if err != nil || !res.Landed || len(res.Refused) != 0 {
			t.Fatalf("land %s: %+v, %v", id, res, err)
		}
		out := counter.Statements()
		for i, s := range out {
			out[i] = strings.Join(strings.Fields(s), " ")
		}
		return out
	}
	check := func(name string, got []string, ceiling int) {
		t.Helper()
		t.Logf("%s batch: %d statements", name, len(got))
		count := func(match func(string) bool) int {
			n := 0
			for _, s := range got {
				if match(s) {
					n++
				}
			}
			return n
		}
		bad := false
		for _, c := range []struct {
			what  string
			match func(string) bool
		}{
			{"rule row reads", func(s string) bool { return strings.Contains(s, "from event_rule where enabled and sample_key = any") }},
			{"calc rule probes", func(s string) bool { return strings.HasPrefix(s, "with k(lane, name) as") }},
			{"property series lock passes", func(s string) bool {
				return strings.HasPrefix(s, "select pg_advisory_xact_lock(hashtextextended(t.k, 0)) from unnest")
			}},
			{"endpoint seen stamps", func(s string) bool { return strings.Contains(s, "insert into \"endpoint_live\"") }},
			{"commits", func(s string) bool { return s == "commit" }},
		} {
			if n := count(c.match); n != 1 {
				t.Errorf("%s batch: %d %s, want one", name, n, c.what)
				bad = true
			}
		}
		if len(got) > ceiling {
			t.Errorf("%s batch: %d statements, want at most %d", name, len(got), ceiling)
			bad = true
		}
		if bad {
			for i, s := range got {
				t.Logf("  %02d %.120s", i, s)
			}
		}
	}
	land("first", "up", base)
	check("steady", land("steady", "up", base.Add(30*time.Second)), maxSteadyBatchStatements)
	check("judged", land("judged", "down", base.Add(60*time.Second)), maxJudgedBatchStatements)
}
