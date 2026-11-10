package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/lab"
	labscenario "github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
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
		if err := runLabSeed(ctx, &out, "dev", lab.NodeName, lab.LoginUser, lab.LoginPassword, tokenHash, noNode, nil, 0); err != nil {
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

// Each lab run mints the dev fleet's declared pusher a token (#1370, #1743),
// and the seed makes it the pusher's only one, as it re-enrolls the node: the run's token
// authenticates the booking service, and the last run's no longer does.
func TestLabSeedGivesThePusherThisRunsToken(t *testing.T) {
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	t.Setenv("OMNIGLASS_DSN", dsn)
	t.Setenv("OMNIGLASS_DATA_DIR", t.TempDir())
	_, nodeHash, _, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	run := func() []byte {
		t.Helper()
		_, hash, prefix, err := auth.NewBearerToken()
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := runLabSeed(ctx, &out, "dev", lab.NodeName, lab.LoginUser, lab.LoginPassword, hex.EncodeToString(nodeHash), false,
			map[string]lab.PusherToken{"sim-booking-service": {Hash: hex.EncodeToString(hash), Prefix: prefix}}, 0); err != nil {
			t.Fatalf("lab seed: %v", err)
		}
		return hash
	}
	first := run()
	second := run()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	pr, err := gw.AuthenticateBearer(ctx, second)
	if err != nil || pr.Service == nil || pr.Service.Name != "sim-booking-service" {
		t.Fatalf("this run's token = %+v, %v; want the booking service", pr, err)
	}
	if _, err := gw.AuthenticateBearer(ctx, first); !errors.Is(err, storage.ErrCredentialNotFound) {
		t.Fatalf("the last run's token still authenticates: %v", err)
	}
}

// A walkthrough lab's seed sets the platform's collection report window
// (#1646), so the node and the server, which both read it at start, report
// every 10s from their first window; a seed told nothing leaves the setting
// at the platform's own 60s.
func TestLabSeedSetsTheReportWindowItIsGiven(t *testing.T) {
	window := func(seconds int) int {
		t.Helper()
		ctx := context.Background()
		dsn := storagetest.NewDSN(t)
		t.Setenv("OMNIGLASS_DSN", dsn)
		t.Setenv("OMNIGLASS_DATA_DIR", t.TempDir())
		_, hash, _, err := auth.NewBearerToken()
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := runLabSeed(ctx, &out, "dev", lab.NodeName, lab.LoginUser, lab.LoginPassword, hex.EncodeToString(hash), false, nil, seconds); err != nil {
			t.Fatalf("lab seed (report seconds %d): %v", seconds, err)
		}
		gw, err := storage.NewPG(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer gw.Close()
		s, err := gw.CollectionSettings(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return s.ReportSeconds
	}
	if got := window(10); got != 10 {
		t.Errorf("seeded with 10: collection.report_seconds = %d, want 10", got)
	}
	if got := window(0); got != 60 {
		t.Errorf("seeded with none: collection.report_seconds = %d, want the platform's 60", got)
	}
}

// A scale lab's seed (#1554), against a real database: the scenario's plan
// goes down with a node per site, each enrolled with the token the lab
// minted for it, each declared pusher (#1743) holding the token minted for
// it, each site's meeting platform (#1747) simulated on its node with a
// webhook to post to and every system of its site to name, and the report
// tells the lab its nodes are present. A missing site
// hash or pusher token is refused before the database is touched, and a
// database that already holds data is refused.
func TestLabScaleSeedEnrollsEverySiteNode(t *testing.T) {
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	t.Setenv("OMNIGLASS_DSN", dsn)
	t.Setenv("OMNIGLASS_DATA_DIR", t.TempDir())
	// A known key, so this test reads back the webhook secrets the seed sealed.
	kek := bytes.Repeat([]byte{0x7}, 32)
	t.Setenv("OMNIGLASS_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(kek))
	plan := corporateScalePlan(t, 3)
	tokens := map[string][]byte{}
	var nodes []lab.SiteNode
	for _, site := range plan.Sites {
		_, hash, _, err := auth.NewBearerToken()
		if err != nil {
			t.Fatal(err)
		}
		name := labscenario.SiteNode(site.Name)
		tokens[name] = hash
		nodes = append(nodes, lab.SiteNode{Name: name, TokenHash: hex.EncodeToString(hash)})
	}
	minted, err := scaleLabPushers(plan, auth.NewBearerToken)
	if err != nil {
		t.Fatal(err)
	}
	if len(minted) == 0 {
		t.Fatal("corporate at 3 systems declares no pusher")
	}
	pushers, err := lab.ParsePusherTokens(lab.FormatPusherTokens(minted))
	if err != nil {
		t.Fatal(err)
	}
	if err := runLabScaleSeed(ctx, &bytes.Buffer{}, "corporate", 3, lab.LoginUser, lab.LoginPassword,
		lab.FormatSiteNodeTokenHashes(nodes[1:]), pushers); err == nil || !strings.Contains(err.Error(), nodes[0].Name) {
		t.Fatalf("a missing site hash: %v, want a refusal naming %s", err, nodes[0].Name)
	}
	short, _ := lab.ParsePusherTokens(lab.FormatPusherTokens(minted[1:]))
	if err := runLabScaleSeed(ctx, &bytes.Buffer{}, "corporate", 3, lab.LoginUser, lab.LoginPassword,
		lab.FormatSiteNodeTokenHashes(nodes), short); err == nil || !strings.Contains(err.Error(), minted[0].Name) {
		t.Fatalf("a missing pusher token: %v, want a refusal naming %s", err, minted[0].Name)
	}

	var out bytes.Buffer
	if err := runLabScaleSeed(ctx, &out, "corporate", 3, lab.LoginUser, lab.LoginPassword, lab.FormatSiteNodeTokenHashes(nodes), pushers); err != nil {
		t.Fatalf("lab seed --systems 3: %v", err)
	}
	senders := map[string]int{} // site -> its platforms
	for _, site := range plan.Sites {
		for _, p := range site.Pushers {
			if p.Webhook {
				senders[site.Name]++
			}
		}
	}
	if len(senders) != len(plan.Sites) {
		t.Fatalf("corporate at 3 declares platforms at %d of %d sites, want every one", len(senders), len(plan.Sites))
	}
	if res := lab.ParseSeedResult(out.String()); !res.Node || !strings.Contains(res.Summary, "3 systems") ||
		!strings.Contains(res.Summary, fmt.Sprintf("%d webhook senders", len(plan.Sites))) {
		t.Fatalf("seed result %+v, want the nodes present, the size and the webhook senders named", res)
	}
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(kek)))
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	for name, hash := range tokens {
		pr, err := gw.AuthenticateBearer(ctx, hash)
		if err != nil || pr.Node == nil || pr.Node.Name != name {
			t.Errorf("%s's token = %+v, %v; want it to authenticate %s", name, pr, err, name)
		}
	}
	for _, p := range minted {
		hash, _ := hex.DecodeString(p.TokenHash)
		pr, err := gw.AuthenticateBearer(ctx, hash)
		if err != nil || pr.Service == nil || pr.Service.Name != p.Name {
			t.Errorf("pusher %s's token = %+v, %v; want it to authenticate the pusher", p.Name, pr, err)
		}
	}
	// One node per site, at its site, simulating exactly that site's
	// components, each at a loopback address no other component has.
	all := scope.Set{All: true}
	listed, err := gw.ListNodes(ctx, all)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != len(plan.Sites) {
		t.Errorf("%d nodes for %d sites", len(listed), len(plan.Sites))
	}
	addresses := map[string]string{}
	for _, site := range plan.Sites {
		name := labscenario.SiteNode(site.Name)
		node, err := gw.GetNode(ctx, name, all)
		if err != nil {
			t.Fatal(err)
		}
		if node.LocationName == nil || *node.LocationName != site.Name {
			t.Errorf("node %s is at %v, want the site %s", name, node.LocationName, site.Name)
		}
		devices, err := gw.NodeSimDevices(ctx, name, all)
		if err != nil {
			t.Fatal(err)
		}
		if want := site.Totals.Components + senders[site.Name]; len(devices) != want {
			t.Errorf("node %s simulates %d components, want the site's %d and its platform", name, len(devices), want)
		}
		hooks, err := gw.NodeSimWebhooks(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		var rooms []string
		for _, s := range site.Systems {
			rooms = append(rooms, s.Name)
		}
		sort.Strings(rooms)
		if len(hooks) != senders[site.Name] {
			t.Errorf("node %s holds %d webhooks to post, want its site's %d platforms", name, len(hooks), senders[site.Name])
		}
		for comp, h := range hooks {
			if h.Path == "" || h.Secret == "" || !reflect.DeepEqual(h.Identities, rooms) {
				t.Errorf("platform %s posts to %q (secret set %v) naming %v, want a path, a secret and its site's systems %v",
					comp, h.Path, h.Secret != "", h.Identities, rooms)
			}
		}
		for _, d := range devices {
			if !strings.HasPrefix(d.Address, "127.") {
				t.Errorf("%s is at %q, want a loopback address", d.ComponentID, d.Address)
			}
			if other, dup := addresses[d.Address]; dup {
				t.Errorf("%s and %s share %s", d.ComponentID, other, d.Address)
			}
			addresses[d.Address] = d.ComponentID
		}
	}
	if want := plan.Totals.Components + len(plan.Sites); len(addresses) != want {
		t.Errorf("%d addresses for %d components and platforms", len(addresses), want)
	}
	if err := runLabScaleSeed(ctx, &bytes.Buffer{}, "corporate", 3, lab.LoginUser, lab.LoginPassword,
		lab.FormatSiteNodeTokenHashes(nodes), pushers); err == nil || !strings.Contains(err.Error(), "holds data") {
		t.Fatalf("a second seed: %v, want a refusal of a database holding data", err)
	}
}

func corporateScalePlan(t *testing.T, systems int) *labscenario.Plan {
	t.Helper()
	m, err := labscenario.Shipped("corporate")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := labscenario.ShippedCatalog()
	if err != nil {
		t.Fatal(err)
	}
	p, err := labscenario.Resolve(m, cat, systems)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
