package labfootprint

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// The worklist read (#1868) reads each node's worst pull loss reported in the
// window, by node name: a node refused in one window reads 1 however its
// other windows read, a node served throughout reads 0, a reading outside
// the window counts for nothing, and a node with no reading in the window
// reads as unreported rather than dropping out.
func TestIntegrationWorklistQueryReadsEachNodesWorstWindow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if _, err := gw.EnsureMetricPartitions(ctx); err != nil {
		t.Fatal(err)
	}
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	platform := map[string]string{}
	for _, name := range []string{"edge-hq", "edge-a", "edge-b", "edge-c"} {
		n, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: name}, all, all)
		if err != nil {
			t.Fatal(err)
		}
		platform[name] = *n.PlatformComponentID
	}
	to := time.Now().UTC().Truncate(time.Second)
	from := to.Add(-5 * time.Minute)
	in := from.Add(time.Minute)
	if err := gw.RecordPlatformMetrics(ctx, []storage.PlatformMetric{
		{ComponentID: platform["edge-hq"], Key: writerclass.WorklistPullLossRatio, Value: 0, TS: in},
		{ComponentID: platform["edge-hq"], Key: writerclass.WorklistPullLossRatio, Value: 1, TS: in.Add(30 * time.Second)},
		{ComponentID: platform["edge-a"], Key: writerclass.WorklistPullLossRatio, Value: 0, TS: in},
		{ComponentID: platform["edge-b"], Key: writerclass.WorklistPullLossRatio, Value: 1, TS: from.Add(-time.Minute)},
		{ComponentID: platform["edge-b"], Key: writerclass.WorklistPullLossRatio, Value: 0, TS: in},
		{ComponentID: platform["edge-c"], Key: writerclass.WorklistPullLossRatio, Value: 1, TS: from.Add(-time.Minute)},
	}); err != nil {
		t.Fatal(err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	rows, err := conn.Query(ctx, WorklistQuery(from, to))
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	got, unreported, err := ParseWorklist(strings.Join(lines, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got["edge-hq"] != 1 || got["edge-a"] != 0 || got["edge-b"] != 0 {
		t.Errorf("read %v, want edge-hq 1, edge-a 0, edge-b 0", got)
	}
	if !slices.Equal(unreported, []string{"edge-c"}) {
		t.Errorf("unreported %v, want edge-c", unreported)
	}
}
