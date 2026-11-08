package bus

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestWorklistReplySizeMeasure is the measurement behind #1579: the corporate
// plan seeded with a node per site, each node's worklist built through the
// path the server answers from (buildWorklistReply, then json.Marshal) and its
// bytes reported against max payload. It is a probe, not a gate: set
// WORKLIST_MEASURE to the sizes (e.g. 100,1000) to run it.
func TestWorklistReplySizeMeasure(t *testing.T) {
	sizes := os.Getenv("WORKLIST_MEASURE")
	if sizes == "" {
		t.Skip("set WORKLIST_MEASURE=100,1000 to measure")
	}
	for _, f := range strings.Split(sizes, ",") {
		size, err := strconv.Atoi(f)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(f, func(t *testing.T) { measureWorklists(t, size) })
	}
}

// corporateFleet is the corporate plan at systems seeded on a fresh database,
// a node per site (named by nodeOf), with the headquarters' archetype name.
func corporateFleet(t *testing.T, systems int) (gw *storage.PG, plan *scenario.Plan, hq string, nodeOf func(site string) string) {
	t.Helper()
	m, err := scenario.Shipped("corporate")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := scenario.ShippedCatalog()
	if err != nil {
		t.Fatal(err)
	}
	plan, err = scenario.Resolve(m, cat, systems)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range m.Archetypes {
		if a.Headquarters {
			hq = a.Name.Name
		}
	}
	ctx := context.Background()
	gw, err = storage.NewPG(ctx, storagetest.NewDSN(t),
		storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	nodeOf = func(site string) string { return "edge-" + site }
	for _, s := range plan.Sites {
		if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: nodeOf(s.Name), Label: s.Name}, all, all); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := devseed.SeedPlan(ctx, gw, "", plan, devseed.PlanOptions{Endpoints: true, NodeFor: nodeOf}); err != nil {
		t.Fatal(err)
	}
	return gw, plan, hq, nodeOf
}

func measureWorklists(t *testing.T, systems int) {
	gw, plan, hq, nodeOf := corporateFleet(t, systems)
	srv := &Server{store: gw}
	worst, worstNode := 0, ""
	for _, s := range plan.Sites {
		reply, err := srv.buildWorklistReply(context.Background(), nodeOf(s.Name))
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(reply)
		if err != nil {
			t.Fatal(err)
		}
		if s.Archetype == hq {
			t.Logf("HQ %s: %d tasks, %d bytes (%.0f%% of max payload %d)", nodeOf(s.Name), len(reply.Tasks), len(b), 100*float64(len(b))/DefaultMaxPayload, DefaultMaxPayload)
		}
		if len(b) > worst {
			worst, worstNode = len(b), nodeOf(s.Name)
		}
	}
	t.Logf("%d systems, %d sites: largest worklist %s at %d bytes", systems, len(plan.Sites), worstNode, worst)
}
