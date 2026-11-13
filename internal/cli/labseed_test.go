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
	"time"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/devseed"
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
		if err := runLabSeed(ctx, &out, "dev", lab.NodeName, lab.LoginUser, lab.LoginPassword, tokenHash, noNode, nil, 0, false); err != nil {
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
			map[string]lab.PusherToken{"sim-booking-service": {Hash: hex.EncodeToString(hash), Prefix: prefix}}, 0, false); err != nil {
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
		if err := runLabSeed(ctx, &out, "dev", lab.NodeName, lab.LoginUser, lab.LoginPassword, hex.EncodeToString(hash), false, nil, seconds, false); err != nil {
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

// A capture's seed (#1790) lands an hour of platform history on the lab's
// server, with no node to collect from; the same seed not asked for it lands
// none, so a learner's lab reads only what its own server measures.
func TestLabSeedLandsPlatformHistoryOnlyWhenAsked(t *testing.T) {
	seeded := func(platformHistory bool) (got, want int) {
		t.Helper()
		ctx := context.Background()
		dsn := storagetest.NewDSN(t)
		t.Setenv("OMNIGLASS_DSN", dsn)
		t.Setenv("OMNIGLASS_DATA_DIR", t.TempDir())
		var out bytes.Buffer
		if err := runLabSeed(ctx, &out, "dev", lab.NodeName, lab.LoginUser, lab.LoginPassword, "", true, nil, 0, platformHistory); err != nil {
			t.Fatalf("lab seed (platform history %v): %v", platformHistory, err)
		}
		gw, err := storage.NewPG(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer gw.Close()
		server, err := gw.EnsureServerComponent(ctx, lab.ServerName)
		if err != nil {
			t.Fatal(err)
		}
		status, err := gw.RetentionStatus(ctx)
		if err != nil {
			t.Fatal(err)
		}
		want = len(devseed.PlatformHistorySeries(status, time.Now())) * devseed.PlatformHistoryMinutes
		for _, key := range devseed.PlatformHistoryKeys() {
			samples, err := gw.ListMetricSeries(ctx, "component", server, key, 0, 100000)
			if err != nil {
				t.Fatal(err)
			}
			got += len(samples)
		}
		return got, want
	}
	if got, want := seeded(true); got != want {
		t.Errorf("seeded with platform history: %d samples on the server, want %d", got, want)
	}
	if got, _ := seeded(false); got != 0 {
		t.Errorf("seeded without platform history: %d samples on the server, want none", got)
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
		lab.FormatSiteNodeTokenHashes(nodes[1:]), pushers, false, false); err == nil || !strings.Contains(err.Error(), nodes[0].Name) {
		t.Fatalf("a missing site hash: %v, want a refusal naming %s", err, nodes[0].Name)
	}
	short, _ := lab.ParsePusherTokens(lab.FormatPusherTokens(minted[1:]))
	if err := runLabScaleSeed(ctx, &bytes.Buffer{}, "corporate", 3, lab.LoginUser, lab.LoginPassword,
		lab.FormatSiteNodeTokenHashes(nodes), short, false, false); err == nil || !strings.Contains(err.Error(), minted[0].Name) {
		t.Fatalf("a missing pusher token: %v, want a refusal naming %s", err, minted[0].Name)
	}

	var out bytes.Buffer
	if err := runLabScaleSeed(ctx, &out, "corporate", 3, lab.LoginUser, lab.LoginPassword, lab.FormatSiteNodeTokenHashes(nodes), pushers, false, false); err != nil {
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
		lab.FormatSiteNodeTokenHashes(nodes), pushers, false, false); err == nil || !strings.Contains(err.Error(), "holds data") {
		t.Fatalf("a second seed: %v, want a refusal of a database holding data", err)
	}
}

// A kept scale lab's seed (#1633), against a real database: a second seed
// with keep, on the database the first one seeded and with this run's new
// token hashes, seeds nothing (the component and node counts hold), enrolls
// every site node with its new hash (the last run's no longer authenticates)
// and says it kept the fleet. Without keep the throwaway refusal stands, and
// with keep at another size the kept fleet is not the plan, so it is refused
// naming --fresh.
func TestLabScaleSeedKeptReEnrollsWithoutReseeding(t *testing.T) {
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	t.Setenv("OMNIGLASS_DSN", dsn)
	t.Setenv("OMNIGLASS_DATA_DIR", t.TempDir())
	plan := corporateScalePlan(t, 3)
	mint := func() (map[string][]byte, string) {
		t.Helper()
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
		return tokens, lab.FormatSiteNodeTokenHashes(nodes)
	}
	firstTokens, firstHashes := mint()
	firstPushers, firstPusherTokens := scalePusherTokens(t, plan)
	if err := runLabScaleSeed(ctx, &bytes.Buffer{}, "corporate", 3, lab.LoginUser, lab.LoginPassword, firstHashes, firstPusherTokens, false, true); err != nil {
		t.Fatalf("the first seed, kept: %v", err)
	}
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	all := scope.Set{All: true}
	counts := func() (int, int) {
		t.Helper()
		cs, err := gw.ListComponents(ctx, all)
		if err != nil {
			t.Fatal(err)
		}
		ns, err := gw.ListNodes(ctx, all)
		if err != nil {
			t.Fatal(err)
		}
		return len(cs), len(ns)
	}
	components, nodes := counts()

	secondTokens, secondHashes := mint()
	secondPushers, secondPusherTokens := scalePusherTokens(t, plan)
	var out bytes.Buffer
	if err := runLabScaleSeed(ctx, &out, "corporate", 3, lab.LoginUser, lab.LoginPassword, secondHashes, secondPusherTokens, false, true); err != nil {
		t.Fatalf("the second seed, kept: %v", err)
	}
	res := lab.ParseSeedResult(out.String())
	if !res.Node || !strings.Contains(res.Summary, "kept") {
		t.Fatalf("seed result %+v, want the nodes present and the fleet kept", res)
	}
	if c, n := counts(); c != components || n != nodes {
		t.Fatalf("after a kept seed: %d components and %d nodes, want the first run's %d and %d", c, n, components, nodes)
	}
	for name, hash := range secondTokens {
		pr, err := gw.AuthenticateBearer(ctx, hash)
		if err != nil || pr.Node == nil || pr.Node.Name != name {
			t.Errorf("%s's new token = %+v, %v; want it to authenticate %s", name, pr, err, name)
		}
		if _, err := gw.AuthenticateBearer(ctx, firstTokens[name]); err == nil {
			t.Errorf("%s's last-run token still authenticates", name)
		}
	}
	// Each pusher (#1743) holds this run's token alone, as each node does.
	for i, p := range secondPushers {
		hash, _ := hex.DecodeString(p.TokenHash)
		pr, err := gw.AuthenticateBearer(ctx, hash)
		if err != nil || pr.Service == nil || pr.Service.Name != p.Name {
			t.Errorf("pusher %s's new token = %+v, %v; want it to authenticate the pusher", p.Name, pr, err)
		}
		old, _ := hex.DecodeString(firstPushers[i].TokenHash)
		if _, err := gw.AuthenticateBearer(ctx, old); err == nil {
			t.Errorf("pusher %s's last-run token still authenticates", p.Name)
		}
	}

	if err := runLabScaleSeed(ctx, &bytes.Buffer{}, "corporate", 3, lab.LoginUser, lab.LoginPassword, secondHashes, secondPusherTokens, false, false); err == nil ||
		!strings.Contains(err.Error(), "holds data") || !strings.Contains(err.Error(), "--fresh") {
		// The kept volumes outlive a throwaway run, so starting it again
		// throwaway never clears them: the refusal names --fresh.
		t.Fatalf("a throwaway seed on the kept database: %v, want the refusal of a database holding data, naming --fresh", err)
	}
	other := corporateScalePlan(t, 20)
	var otherNodes []lab.SiteNode
	for _, site := range other.Sites {
		otherNodes = append(otherNodes, lab.SiteNode{Name: labscenario.SiteNode(site.Name), TokenHash: "00"})
	}
	_, otherPushers := scalePusherTokens(t, other)
	if err := runLabScaleSeed(ctx, &bytes.Buffer{}, "corporate", 20, lab.LoginUser, lab.LoginPassword,
		lab.FormatSiteNodeTokenHashes(otherNodes), otherPushers, false, true); err == nil || !strings.Contains(err.Error(), "--fresh") {
		t.Fatalf("a kept seed at another size: %v, want a refusal naming --fresh", err)
	}
	// A first run stopped after the fleet was seeded and before its nodes
	// were placed leaves a node at no site: not this plan's fleet.
	unplaced, nowhere := labscenario.SiteNode(plan.Sites[0].Name), ""
	if _, err := gw.UpdateNode(ctx, "", unplaced, storage.NodePatch{LocationName: &nowhere}, all, all, all); err != nil {
		t.Fatal(err)
	}
	if err := runLabScaleSeed(ctx, &bytes.Buffer{}, "corporate", 3, lab.LoginUser, lab.LoginPassword, secondHashes, secondPusherTokens, false, true); err == nil ||
		!strings.Contains(err.Error(), "--fresh") || !strings.Contains(err.Error(), unplaced) {
		t.Fatalf("a kept seed with %s at no site: %v, want a refusal naming the node and --fresh", unplaced, err)
	}
	if c, n := counts(); c != components || n != nodes {
		t.Fatalf("after the refusals: %d components and %d nodes, want %d and %d", c, n, components, nodes)
	}
}

// scalePusherTokens mints a token for each pusher the plan declares, as a
// scale lab's run does, and returns them as the seed reads them.
func scalePusherTokens(t *testing.T, plan *labscenario.Plan) ([]lab.Pusher, map[string]lab.PusherToken) {
	t.Helper()
	minted, err := scaleLabPushers(plan, auth.NewBearerToken)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := lab.ParsePusherTokens(lab.FormatPusherTokens(minted))
	if err != nil {
		t.Fatal(err)
	}
	return minted, tokens
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

// A scale lab seeded with the induced refusal (#1698) says which device
// answers the unregistered key and through which driver, and the key is gone
// from the catalog when the seed ends; a seeded fleet's lab seed refuses the
// knob, before it touches a database.
func TestLabScaleSeedInducesAnUnregisteredKeyWhenAsked(t *testing.T) {
	ctx := context.Background()
	root := newRoot("test")
	var refusal strings.Builder
	root.SetOut(&refusal)
	root.SetErr(&refusal)
	root.SetArgs([]string{"lab", "seed", "--induce-unregistered"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "--systems") {
		t.Fatalf("lab seed --induce-unregistered without --systems = %v, want a refusal naming --systems", err)
	}

	dsn := storagetest.NewDSN(t)
	t.Setenv("OMNIGLASS_DSN", dsn)
	t.Setenv("OMNIGLASS_DATA_DIR", t.TempDir())
	var nodes []lab.SiteNode
	induced := corporateScalePlan(t, 3)
	_, pushers := scalePusherTokens(t, induced)
	for _, site := range induced.Sites {
		_, hash, _, err := auth.NewBearerToken()
		if err != nil {
			t.Fatal(err)
		}
		nodes = append(nodes, lab.SiteNode{Name: labscenario.SiteNode(site.Name), TokenHash: hex.EncodeToString(hash)})
	}
	var out bytes.Buffer
	if err := runLabScaleSeed(ctx, &out, "corporate", 3, lab.LoginUser, lab.LoginPassword, lab.FormatSiteNodeTokenHashes(nodes), pushers, true, false); err != nil {
		t.Fatalf("lab seed --systems 3 --induce-unregistered: %v", err)
	}
	res := lab.ParseSeedResult(out.String())
	if !strings.Contains(res.Summary, "refused-unregistered") || !strings.Contains(res.Summary, devseed.InducedKey) {
		t.Fatalf("seed summary %q, want the induced refusal named", res.Summary)
	}
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if _, err := gw.GetPropertyType(ctx, devseed.InducedKey); !errors.Is(err, storage.ErrPropertyTypeNotFound) {
		t.Errorf("the induced key after the seed: %v, want it unregistered", err)
	}
}
