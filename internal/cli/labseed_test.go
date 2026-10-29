package cli

import (
	"bytes"
	"context"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/lab"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The lab's seed with no node (#1267), against a real database: the dev
// fleet goes down without the simulated fleet's wiring, no node is enrolled
// (so it needs no node token), and the report tells the lab to run none. The
// same seed with its node wires the fleet and reports the node present.
func TestLabSeedWithNoNodeWiresNothingAndEnrollsNoNode(t *testing.T) {
	seedInto := func(noNode bool, tokenHash string) (lab.SeedResult, int) {
		t.Helper()
		ctx := context.Background()
		dsn := storagetest.NewDSN(t)
		t.Setenv("OMNIGLASS_DSN", dsn)
		t.Setenv("OMNIGLASS_DATA_DIR", t.TempDir())
		var out bytes.Buffer
		if err := runLabSeed(ctx, &out, "dev", lab.NodeName, lab.LoginUser, lab.LoginPassword, tokenHash, noNode); err != nil {
			t.Fatalf("lab seed (no node %v): %v", noNode, err)
		}
		gw, err := storage.NewPG(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer gw.Close()
		eps, err := gw.ListEndpoints(ctx, scope.Set{All: true})
		if err != nil {
			t.Fatal(err)
		}
		return lab.ParseSeedResult(out.String()), len(eps)
	}

	still, stillEndpoints := seedInto(true, "")
	if still.Node {
		t.Fatalf("no node: the seed reports a node present (%+v), so the lab would run one", still)
	}
	if !strings.Contains(still.Summary, "dev") {
		t.Fatalf("no node: summary %q does not name the fleet", still.Summary)
	}

	_, hash, _, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	live, liveEndpoints := seedInto(false, hex.EncodeToString(hash))
	if !live.Node {
		t.Fatalf("with its node: the seed reports none (%+v)", live)
	}
	if stillEndpoints >= liveEndpoints {
		t.Fatalf("no node: %d endpoints, with its node %d; want the simulated wiring left out", stillEndpoints, liveEndpoints)
	}
}
