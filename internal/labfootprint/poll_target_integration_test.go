package labfootprint

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The poll-target statement reads, for each node, the poll tasks the node's
// own worklist carries, each at the interval the node's scheduler gives it:
// a driver poll at its schedule (each tick without one), any other spec at
// the probe interval. Disabled and listen tasks, and endpoints on no node,
// are not targets.
func TestIntegrationPollTargetsMatchEachNodesWorklist(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	all := scope.Set{All: true}
	for _, n := range []string{"edge-a", "edge-b"} {
		if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: n}, all, all); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []string{"panel-1", "panel-2", "panel-3"} {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: c}, all, all, all, all); err != nil {
			t.Fatal(err)
		}
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	componentOf := map[string]string{}
	endpoint := func(name, transport, component, node string) {
		t.Helper()
		componentOf[name] = component
		nodeRef := "null"
		if node != "" {
			nodeRef = fmt.Sprintf("(select principal_id from node where name = '%s')", node)
		}
		exec(fmt.Sprintf(`insert into endpoint (name, transport, component, node_name, params)
			values ($1, $2, (select id from component where name = $3), %s, '{"target":"10.0.0.1"}'::jsonb)`, nodeRef),
			name, transport, component)
	}
	task := func(id, mode, endpoint, spec string, enabled bool) {
		t.Helper()
		exec(`insert into task (id, mode, endpoint_id, spec, enabled)
			values ($1, $2, (select id from endpoint where name = $3), $4::jsonb, $5)`, id, mode, endpoint, spec, enabled)
	}
	endpoint("p1-http", "http", "panel-1", "edge-a")
	endpoint("p1-tcp", "tcp", "panel-1", "edge-a")
	endpoint("p2-snmp", "snmp", "panel-2", "edge-a")
	endpoint("p3-icmp", "icmp", "panel-3", "edge-b")
	endpoint("p3-loose", "tcp", "panel-3", "")
	task("t-http", "poll", "p1-http", `{"driver":"d","function":"f","schedule":{"every":"45s"},"emits":[]}`, true)
	task("t-tcp", "poll", "p1-tcp", `{}`, true)
	task("t-snmp", "poll", "p2-snmp", `{"driver":"d","function":"f","emits":[]}`, true)
	task("t-snmp-off", "poll", "p2-snmp", `{"driver":"d","function":"g","schedule":{"every":"1s"},"emits":[]}`, false)
	task("t-listen", "listen", "p1-tcp", `{"driver":"d","function":"l","emits":[]}`, true)
	task("t-icmp", "poll", "p3-icmp", `{"probe":"icmp"}`, true)
	task("t-loose", "poll", "p3-loose", `{}`, true)

	cadence := map[string]NodeCadence{
		"edge-a": {Heartbeat: 5 * time.Second, ProbeEvery: 30 * time.Second},
		"edge-b": {Heartbeat: 10 * time.Second, ProbeEvery: 20 * time.Second},
	}
	rows, err := conn.Query(ctx, pollTargetsQuery)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	for rows.Next() {
		var node, component, transport, every string
		if err := rows.Scan(&node, &component, &transport, &every); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&out, "%s|%s|%s|%s\n", node, component, transport, every)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	got, err := parsePollTargets(out.String(), cadence)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("targets = %v, want edge-a and edge-b alone", got)
	}

	// Each node's targets are its worklist's poll tasks, at the interval the
	// node's own reading of each spec gives.
	for node, c := range cadence {
		wl, err := gw.NodeWorklist(ctx, node)
		if err != nil {
			t.Fatal(err)
		}
		var want []string
		for _, task := range wl.Tasks {
			if task.Mode != "poll" {
				continue
			}
			d := c.ProbeEvery
			if driver.IsBaked(task.Spec) {
				d = 0
				if fn, err := driver.ParseBaked(task.Spec); err == nil && fn.Schedule != nil {
					d = fn.Schedule.Interval()
				}
			}
			want = append(want, fmt.Sprintf("%s %s %s", componentOf[task.EndpointName], task.Transport, max(d, c.Heartbeat)))
		}
		var have []string
		for _, p := range got[node] {
			var component string
			if err := conn.QueryRow(ctx, `select name from component where id::text = $1`, p.ComponentID).Scan(&component); err != nil {
				t.Fatalf("%s target %+v names no component: %v", node, p, err)
			}
			have = append(have, fmt.Sprintf("%s %s %s", component, p.Transport, p.Interval))
		}
		slices.Sort(want)
		slices.Sort(have)
		if !slices.Equal(have, want) {
			t.Errorf("%s targets %v, want the worklist's %v", node, have, want)
		}
	}
	// The schedule is read, not defaulted: the http poll runs at its 45 s,
	// the unscheduled snmp poll each tick, the probes at the probe interval.
	if wa := sortedTargets(got["edge-a"]); len(wa) != 3 || wa[0].Interval != 45*time.Second || wa[1].Interval != 5*time.Second || wa[2].Interval != 30*time.Second {
		t.Errorf("edge-a targets %v, want http 45s, snmp 5s, tcp 30s", wa)
	}
}

// sortedTargets orders targets by transport.
func sortedTargets(ts []PollTarget) []PollTarget {
	out := slices.Clone(ts)
	slices.SortFunc(out, func(a, b PollTarget) int { return strings.Compare(a.Transport, b.Transport) })
	return out
}
