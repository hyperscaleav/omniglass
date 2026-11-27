package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/lab"
	labscenario "github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// The lab's seed with no outpost (#1267), against a real database: the dev
// fleet goes down without the simulated fleet's wiring, no outpost is enrolled
// (so it needs no outpost token), and the report tells the lab to run none. The
// same seed with its outpost wires the fleet and reports the outpost present.
func TestLabSeedWithNoOutpostWiresNothingAndEnrollsNoOutpost(t *testing.T) {
	seedInto := func(noOutpost bool, tokenHash string) (lab.SeedResult, int) {
		t.Helper()
		ctx := context.Background()
		dsn := storagetest.NewDSN(t)
		t.Setenv("OMNIGLASS_DSN", dsn)
		t.Setenv("OMNIGLASS_DATA_DIR", t.TempDir())
		var out bytes.Buffer
		if err := runLabSeed(ctx, &out, "dev", lab.OutpostName, lab.LoginUser, lab.LoginPassword, tokenHash, noOutpost, nil, 0, false, false); err != nil {
			t.Fatalf("lab seed (no outpost %v): %v", noOutpost, err)
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
	if still.Outpost {
		t.Fatalf("no outpost: the seed reports an outpost present (%+v), so the lab would run one", still)
	}
	if !strings.Contains(still.Summary, "dev") {
		t.Fatalf("no outpost: summary %q does not name the fleet", still.Summary)
	}

	_, hash, _, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	live, liveEndpoints := seedInto(false, hex.EncodeToString(hash))
	if !live.Outpost {
		t.Fatalf("with its outpost: the seed reports none (%+v)", live)
	}
	if stillEndpoints >= liveEndpoints {
		t.Fatalf("no outpost: %d endpoints, with its outpost %d; want the simulated wiring left out", stillEndpoints, liveEndpoints)
	}
}

// Each lab run mints the dev fleet's declared pusher a token (#1370, #1743),
// and the seed makes it the pusher's only one, as it re-enrolls the outpost: the run's token
// authenticates the booking service, and the last run's no longer does.
func TestLabSeedGivesThePusherThisRunsToken(t *testing.T) {
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	t.Setenv("OMNIGLASS_DSN", dsn)
	t.Setenv("OMNIGLASS_DATA_DIR", t.TempDir())
	_, outpostHash, _, err := auth.NewBearerToken()
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
		if err := runLabSeed(ctx, &out, "dev", lab.OutpostName, lab.LoginUser, lab.LoginPassword, hex.EncodeToString(outpostHash), false,
			map[string]lab.PusherToken{"sim-booking-service": {Hash: hex.EncodeToString(hash), Prefix: prefix}}, 0, false, false); err != nil {
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
	if err != nil || pr.Machine == nil || pr.Machine.Name != "sim-booking-service" {
		t.Fatalf("this run's token = %+v, %v; want the booking service", pr, err)
	}
	if _, err := gw.AuthenticateBearer(ctx, first); !errors.Is(err, storage.ErrCredentialNotFound) {
		t.Fatalf("the last run's token still authenticates: %v", err)
	}
}

// A lab kept from a build that did not declare the pusher yet holds no user
// for it (#2194). The next run must seed it, not skip it: the emulator on the
// outpost pushes with the token minted for this run whatever the fleet holds,
// and a token no stored credential matches is a 401 on every push. After the
// kept seed the run's token authenticates the booking service, which holds the
// pusher role on its one system and nothing else.
func TestKeptLabSeedsThePusherItsFleetLacks(t *testing.T) {
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	t.Setenv("OMNIGLASS_DSN", dsn)
	t.Setenv("OMNIGLASS_DATA_DIR", t.TempDir())
	_, outpostHash, _, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	seed := func() ([]byte, string) {
		t.Helper()
		_, hash, prefix, err := auth.NewBearerToken()
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := runLabSeed(ctx, &out, "dev", lab.OutpostName, lab.LoginUser, lab.LoginPassword, hex.EncodeToString(outpostHash), false,
			map[string]lab.PusherToken{"sim-booking-service": {Hash: hex.EncodeToString(hash), Prefix: prefix}}, 0, false, false); err != nil {
			t.Fatalf("lab seed: %v", err)
		}
		return hash, out.String()
	}
	seed()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `delete from users where id = (select user_id from machine where name = 'sim-booking-service')`); err != nil {
		t.Fatalf("make the fleet predate the pusher: %v", err)
	}

	hash, out := seed()
	if !strings.Contains(out, "kept this lab's fleet") {
		t.Fatalf("the second seed is not a kept lab's: %q", out)
	}
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	pr, err := gw.AuthenticateBearer(ctx, hash)
	if err != nil || pr.Machine == nil || pr.Machine.Name != "sim-booking-service" {
		t.Fatalf("the kept lab's run token = %+v, %v; want the booking service to authenticate with it", pr, err)
	}
	var grants []string
	rows, err := conn.Query(ctx, `
		select r.name || ' ' || r.permissions::text || ' ' || g.scope_kind || ' ' || coalesce(l.name, '')
		from machine s join user_grant g on g.user_id = s.user_id join role r on r.id = g.role_id
		left join system y on y.id::text = g.scope_id::text left join location l on l.id = y.location_id
		where s.name = 'sim-booking-service'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			t.Fatal(err)
		}
		grants = append(grants, g)
	}
	if want := devseed.PusherRole + " {telemetry:push} system boardroom-a"; len(grants) != 1 || grants[0] != want {
		t.Fatalf("the seeded pusher's grants = %v, want [%s]", grants, want)
	}
}

// A walkthrough lab's seed sets the platform's collection report window
// (#1646), so the outpost and the server, which both read it at start, report
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
		if err := runLabSeed(ctx, &out, "dev", lab.OutpostName, lab.LoginUser, lab.LoginPassword, hex.EncodeToString(hash), false, nil, seconds, false, false); err != nil {
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
// server, with no outpost to collect from; the same seed not asked for it lands
// none, so a learner's lab reads only what its own server measures.
func TestLabSeedLandsPlatformHistoryOnlyWhenAsked(t *testing.T) {
	seeded := func(platformHistory bool) (got, want int) {
		t.Helper()
		ctx := context.Background()
		dsn := storagetest.NewDSN(t)
		t.Setenv("OMNIGLASS_DSN", dsn)
		t.Setenv("OMNIGLASS_DATA_DIR", t.TempDir())
		var out bytes.Buffer
		if err := runLabSeed(ctx, &out, "dev", lab.OutpostName, lab.LoginUser, lab.LoginPassword, "", true, nil, 0, platformHistory, false); err != nil {
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

// A walkthrough lab's seed with --sso (#2362) configures single sign-on
// against the lab's Dex: the platform client secret Dex knows the server by,
// the issuer and offline_access, Dex's host on the egress allow list, and the
// demo person, a viewer, linked to the identity Dex vouches for them by. A
// second run keeps it all, once; a seed without --sso configures none of it.
func TestLabSeedConfiguresSingleSignOnOnlyWhenAsked(t *testing.T) {
	seed := func(t *testing.T, dsn string, sso bool) *storage.PG {
		t.Helper()
		t.Setenv("OMNIGLASS_DSN", dsn)
		var out bytes.Buffer
		if err := runLabSeed(context.Background(), &out, "dev", lab.OutpostName, lab.LoginUser, lab.LoginPassword, "", true, nil, 0, false, sso); err != nil {
			t.Fatalf("lab seed (sso %v): %v", sso, err)
		}
		// Opened as the seed opens it, so the client secret unseals.
		gw, _, err := openLabDB(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(gw.Close)
		return gw
	}
	ctx := context.Background()
	t.Setenv("OMNIGLASS_DATA_DIR", t.TempDir())

	off := seed(t, storagetest.NewDSN(t), false)
	if p, err := off.SSOProvider(ctx, storage.SSOProviderDefault); err != nil || p != nil {
		t.Fatalf("seeded without --sso: provider %+v, %v; want single sign-on off", p, err)
	}
	if _, err := off.ResolveUsername(ctx, lab.SSODemoUser); err == nil {
		t.Fatalf("seeded without --sso: %s exists", lab.SSODemoUser)
	}

	dsn := storagetest.NewDSN(t)
	seed(t, dsn, true)
	gw := seed(t, dsn, true)
	p, err := gw.SSOProvider(ctx, storage.SSOProviderDefault)
	if err != nil || p == nil {
		t.Fatalf("seeded with --sso: provider %+v, %v", p, err)
	}
	if p.Issuer != lab.DexIssuer || p.ClientID != lab.DexClientID || p.ClientSecret != lab.DexClientSecret || !slices.Contains(p.Scopes, "offline_access") {
		t.Fatalf("seeded with --sso: provider %+v, want the lab's Dex client with offline_access", p)
	}
	bindings, err := gw.GetSettingBindings(ctx, "platform")
	if err != nil {
		t.Fatal(err)
	}
	var allow []any
	for _, b := range bindings {
		if b.Namespace == "notification" {
			allow, _ = b.Doc["egress_allow"].([]any)
		}
	}
	if !reflect.DeepEqual(allow, []any{lab.DexService}) {
		t.Fatalf("seeded twice with --sso: egress_allow = %v, want Dex's host once", allow)
	}
	id, err := gw.ResolveUsername(ctx, lab.SSODemoUser)
	if err != nil {
		t.Fatalf("seeded with --sso: %s: %v", lab.SSODemoUser, err)
	}
	u, err := gw.GetUser(ctx, id, scope.Set{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Grants) != 1 || u.Grants[0].Role != lab.SSODemoRole || u.Grants[0].ScopeKind != "all" {
		t.Fatalf("the demo person's grants = %+v, want one %s grant at all", u.Grants, lab.SSODemoRole)
	}
	links, err := gw.ListIdentityLinks(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].Identifier != lab.SSODemoIdentity() {
		t.Fatalf("the demo person's links = %+v, want %s", links, lab.SSODemoIdentity())
	}
}

// A scale lab's seed (#1554), against a real database: the scenario's plan
// goes down with an outpost per site, each enrolled with the token the lab
// minted for it, each declared pusher (#1743) holding the token minted for
// it, each site's meeting platform (#1747) simulated on its outpost with a
// webhook to post to and every system of its site to name, and the report
// tells the lab its outposts are present. A missing site
// hash or pusher token is refused before the database is touched, and a
// database that already holds data is refused.
func TestLabScaleSeedEnrollsEverySiteOutpost(t *testing.T) {
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	t.Setenv("OMNIGLASS_DSN", dsn)
	t.Setenv("OMNIGLASS_DATA_DIR", t.TempDir())
	// A known key, so this test reads back the webhook secrets the seed sealed.
	kek := bytes.Repeat([]byte{0x7}, 32)
	t.Setenv("OMNIGLASS_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(kek))
	plan := corporateScalePlan(t, 3)
	tokens := map[string][]byte{}
	var outposts []lab.SiteOutpost
	for _, site := range plan.Sites {
		_, hash, _, err := auth.NewBearerToken()
		if err != nil {
			t.Fatal(err)
		}
		name := labscenario.SiteOutpost(site.Name)
		tokens[name] = hash
		outposts = append(outposts, lab.SiteOutpost{Name: name, TokenHash: hex.EncodeToString(hash)})
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
		lab.FormatSiteOutpostTokenHashes(outposts[1:]), pushers, false, false, false); err == nil || !strings.Contains(err.Error(), outposts[0].Name) {
		t.Fatalf("a missing site hash: %v, want a refusal naming %s", err, outposts[0].Name)
	}
	short, _ := lab.ParsePusherTokens(lab.FormatPusherTokens(minted[1:]))
	if err := runLabScaleSeed(ctx, &bytes.Buffer{}, "corporate", 3, lab.LoginUser, lab.LoginPassword,
		lab.FormatSiteOutpostTokenHashes(outposts), short, false, false, false); err == nil || !strings.Contains(err.Error(), minted[0].Name) {
		t.Fatalf("a missing pusher token: %v, want a refusal naming %s", err, minted[0].Name)
	}

	var out bytes.Buffer
	if err := runLabScaleSeed(ctx, &out, "corporate", 3, lab.LoginUser, lab.LoginPassword, lab.FormatSiteOutpostTokenHashes(outposts), pushers, false, false, false); err != nil {
		t.Fatalf("lab seed --systems 3: %v", err)
	}
	// The meeting platform is one account-wide component (#2790), so the plan
	// declares it once, not once per site.
	senders := map[string]int{} // site -> its platforms
	total := 0
	for _, site := range plan.Sites {
		for _, p := range site.Pushers {
			if p.Webhook {
				senders[site.Name]++
				total++
			}
		}
	}
	if total != 1 {
		t.Fatalf("corporate at 3 declares %d platforms, want the one account-wide platform", total)
	}
	if res := lab.ParseSeedResult(out.String()); !res.Outpost || !strings.Contains(res.Summary, "3 systems") ||
		!strings.Contains(res.Summary, fmt.Sprintf("%d webhook senders", total)) {
		t.Fatalf("seed result %+v, want the outposts present, the size and the webhook senders named", res)
	}
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(kek)))
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	for name, hash := range tokens {
		pr, err := gw.AuthenticateBearer(ctx, hash)
		if err != nil || pr.Outpost == nil || pr.Outpost.Name != name {
			t.Errorf("%s's token = %+v, %v; want it to authenticate %s", name, pr, err, name)
		}
	}
	for _, p := range minted {
		hash, _ := hex.DecodeString(p.TokenHash)
		pr, err := gw.AuthenticateBearer(ctx, hash)
		if err != nil || pr.Machine == nil || pr.Machine.Name != p.Name {
			t.Errorf("pusher %s's token = %+v, %v; want it to authenticate the pusher", p.Name, pr, err)
		}
	}
	// One outpost per site, at its site, simulating exactly that site's
	// components, each at a loopback address no other component has.
	all := scope.Set{All: true}
	listed, err := gw.ListOutposts(ctx, all)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != len(plan.Sites) {
		t.Errorf("%d outposts for %d sites", len(listed), len(plan.Sites))
	}
	addresses := map[string]string{}
	var fleet []string
	for _, site := range plan.Sites {
		for _, s := range site.Systems {
			fleet = append(fleet, s.Name)
		}
	}
	for _, site := range plan.Sites {
		name := labscenario.SiteOutpost(site.Name)
		outpost, err := gw.GetOutpost(ctx, name, all)
		if err != nil {
			t.Fatal(err)
		}
		if outpost.LocationName == nil || *outpost.LocationName != site.Name {
			t.Errorf("outpost %s is at %v, want the site %s", name, outpost.LocationName, site.Name)
		}
		devices, err := gw.OutpostSimDevices(ctx, name, all)
		if err != nil {
			t.Fatal(err)
		}
		if want := site.Simulated(); len(devices) != want {
			t.Errorf("outpost %s simulates %d components, want the site's %d, its platform and any polled service", name, len(devices), want)
		}
		hooks, err := gw.OutpostSimWebhooks(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		var rooms []string
		for _, s := range site.Systems {
			rooms = append(rooms, s.Name)
		}
		sort.Strings(rooms)
		if len(hooks) != senders[site.Name] {
			t.Errorf("outpost %s holds %d webhooks to post, want its site's %d platforms", name, len(hooks), senders[site.Name])
		}
		for comp, h := range hooks {
			// The account-wide platform speaks for every system of the fleet
			// and for the devices it lists (#2790), so it names the whole
			// fleet's rooms, not just its site's.
			named := map[string]bool{}
			for _, id := range h.Identities {
				named[id] = true
			}
			for _, s := range fleet {
				if !named[s] {
					t.Errorf("platform %s names %v, want it to name system %s", comp, h.Identities, s)
				}
			}
			// A device id is no room: the account lists its rooms, so the
			// simulator is handed room ids alone, never the device ids bound
			// on the same conduit (#2757).
			for _, id := range h.Identities {
				if strings.HasPrefix(id, "dev-") {
					t.Errorf("platform %s is handed %q to add as a room, want room ids only", comp, id)
				}
			}
			if h.Path == "" || h.Secret == "" {
				t.Errorf("platform %s posts to %q (secret set %v), want a path and a secret", comp, h.Path, h.Secret != "")
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
	if want := plan.Simulated(); len(addresses) != want {
		t.Errorf("%d addresses for %d components and platforms", len(addresses), want)
	}
	if err := runLabScaleSeed(ctx, &bytes.Buffer{}, "corporate", 3, lab.LoginUser, lab.LoginPassword,
		lab.FormatSiteOutpostTokenHashes(outposts), pushers, false, false, false); err == nil || !strings.Contains(err.Error(), "holds data") {
		t.Fatalf("a second seed: %v, want a refusal of a database holding data", err)
	}
}

// A kept scale lab's seed (#1633), against a real database: a second seed
// with keep, on the database the first one seeded and with this run's new
// token hashes, seeds nothing (the component and outpost counts hold), enrolls
// every site outpost with its new hash (the last run's no longer authenticates)
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
		var outposts []lab.SiteOutpost
		for _, site := range plan.Sites {
			_, hash, _, err := auth.NewBearerToken()
			if err != nil {
				t.Fatal(err)
			}
			name := labscenario.SiteOutpost(site.Name)
			tokens[name] = hash
			outposts = append(outposts, lab.SiteOutpost{Name: name, TokenHash: hex.EncodeToString(hash)})
		}
		return tokens, lab.FormatSiteOutpostTokenHashes(outposts)
	}
	firstTokens, firstHashes := mint()
	firstPushers, firstPusherTokens := scalePusherTokens(t, plan)
	if err := runLabScaleSeed(ctx, &bytes.Buffer{}, "corporate", 3, lab.LoginUser, lab.LoginPassword, firstHashes, firstPusherTokens, false, true, false); err != nil {
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
		ns, err := gw.ListOutposts(ctx, all)
		if err != nil {
			t.Fatal(err)
		}
		return len(cs), len(ns)
	}
	components, outposts := counts()

	secondTokens, secondHashes := mint()
	secondPushers, secondPusherTokens := scalePusherTokens(t, plan)
	var out bytes.Buffer
	if err := runLabScaleSeed(ctx, &out, "corporate", 3, lab.LoginUser, lab.LoginPassword, secondHashes, secondPusherTokens, false, true, false); err != nil {
		t.Fatalf("the second seed, kept: %v", err)
	}
	res := lab.ParseSeedResult(out.String())
	if !res.Outpost || !strings.Contains(res.Summary, "kept") {
		t.Fatalf("seed result %+v, want the outposts present and the fleet kept", res)
	}
	if c, n := counts(); c != components || n != outposts {
		t.Fatalf("after a kept seed: %d components and %d outposts, want the first run's %d and %d", c, n, components, outposts)
	}
	for name, hash := range secondTokens {
		pr, err := gw.AuthenticateBearer(ctx, hash)
		if err != nil || pr.Outpost == nil || pr.Outpost.Name != name {
			t.Errorf("%s's new token = %+v, %v; want it to authenticate %s", name, pr, err, name)
		}
		if _, err := gw.AuthenticateBearer(ctx, firstTokens[name]); err == nil {
			t.Errorf("%s's last-run token still authenticates", name)
		}
	}
	// Each pusher (#1743) holds this run's token alone, as each outpost does.
	for i, p := range secondPushers {
		hash, _ := hex.DecodeString(p.TokenHash)
		pr, err := gw.AuthenticateBearer(ctx, hash)
		if err != nil || pr.Machine == nil || pr.Machine.Name != p.Name {
			t.Errorf("pusher %s's new token = %+v, %v; want it to authenticate the pusher", p.Name, pr, err)
		}
		old, _ := hex.DecodeString(firstPushers[i].TokenHash)
		if _, err := gw.AuthenticateBearer(ctx, old); err == nil {
			t.Errorf("pusher %s's last-run token still authenticates", p.Name)
		}
	}

	if err := runLabScaleSeed(ctx, &bytes.Buffer{}, "corporate", 3, lab.LoginUser, lab.LoginPassword, secondHashes, secondPusherTokens, false, false, false); err == nil ||
		!strings.Contains(err.Error(), "holds data") || !strings.Contains(err.Error(), "--fresh") {
		// The kept volumes outlive a throwaway run, so starting it again
		// throwaway never clears them: the refusal names --fresh.
		t.Fatalf("a throwaway seed on the kept database: %v, want the refusal of a database holding data, naming --fresh", err)
	}
	other := corporateScalePlan(t, 20)
	var otherOutposts []lab.SiteOutpost
	for _, site := range other.Sites {
		otherOutposts = append(otherOutposts, lab.SiteOutpost{Name: labscenario.SiteOutpost(site.Name), TokenHash: "00"})
	}
	_, otherPushers := scalePusherTokens(t, other)
	if err := runLabScaleSeed(ctx, &bytes.Buffer{}, "corporate", 20, lab.LoginUser, lab.LoginPassword,
		lab.FormatSiteOutpostTokenHashes(otherOutposts), otherPushers, false, true, false); err == nil || !strings.Contains(err.Error(), "--fresh") {
		t.Fatalf("a kept seed at another size: %v, want a refusal naming --fresh", err)
	}
	// A first run stopped after the fleet was seeded and before its outposts
	// were placed leaves an outpost at no site: not this plan's fleet.
	unplaced, nowhere := labscenario.SiteOutpost(plan.Sites[0].Name), ""
	if _, err := gw.UpdateOutpost(ctx, "", unplaced, storage.OutpostPatch{LocationName: &nowhere}, all, all, all); err != nil {
		t.Fatal(err)
	}
	if err := runLabScaleSeed(ctx, &bytes.Buffer{}, "corporate", 3, lab.LoginUser, lab.LoginPassword, secondHashes, secondPusherTokens, false, true, false); err == nil ||
		!strings.Contains(err.Error(), "--fresh") || !strings.Contains(err.Error(), unplaced) {
		t.Fatalf("a kept seed with %s at no site: %v, want a refusal naming the outpost and --fresh", unplaced, err)
	}
	if c, n := counts(); c != components || n != outposts {
		t.Fatalf("after the refusals: %d components and %d outposts, want %d and %d", c, n, components, outposts)
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
	root := newRoot("test", "")
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
	var outposts []lab.SiteOutpost
	induced := corporateScalePlan(t, 3)
	_, pushers := scalePusherTokens(t, induced)
	for _, site := range induced.Sites {
		_, hash, _, err := auth.NewBearerToken()
		if err != nil {
			t.Fatal(err)
		}
		outposts = append(outposts, lab.SiteOutpost{Name: labscenario.SiteOutpost(site.Name), TokenHash: hex.EncodeToString(hash)})
	}
	var out bytes.Buffer
	if err := runLabScaleSeed(ctx, &out, "corporate", 3, lab.LoginUser, lab.LoginPassword, lab.FormatSiteOutpostTokenHashes(outposts), pushers, true, false, false); err != nil {
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

// The lab's model key reaches the server as an env secret now (#2223), so a
// lab seeded before that sealed it: the seed drops that stored lab key and the
// assistant settings naming it, so the env secret of the same name does not
// fail the server's boot and the env layer shows through. An operator's own
// key_secret is left alone.
func TestLabSeedDropsTheStoredLabKey(t *testing.T) {
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	t.Setenv("OMNIGLASS_DSN", dsn)
	t.Setenv("OMNIGLASS_DATA_DIR", t.TempDir())
	seed := func() {
		t.Helper()
		var out bytes.Buffer
		if err := runLabSeed(ctx, &out, "dev", lab.OutpostName, lab.LoginUser, lab.LoginPassword, "", true, nil, 0, false, false); err != nil {
			t.Fatalf("lab seed: %v", err)
		}
	}
	seed()
	gw, _, err := openLabDB(ctx)
	if err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	// What a lab seeded before #2223 left behind.
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{Name: lab.OmnibotKeySecret, SecretType: "api-key", OwnerKind: "platform",
		Fields: map[string]string{"key": "old-key"}}, all, true); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.MergePatchSettingBinding(ctx, "", "platform", "assistant", map[string]any{"provider": "openai", "key_secret": lab.OmnibotKeySecret}); err != nil {
		t.Fatal(err)
	}
	gw.Close()

	seed()
	gw, _, err = openLabDB(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if m, err := gw.OmnibotModel(ctx); err != nil || m != nil {
		t.Fatalf("after the seed: model = %+v, %v, want none, so the env layer or the recording answers", m, err)
	}
	secrets, err := gw.ListSecrets(ctx, all, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range secrets {
		if s.Name == lab.OmnibotKeySecret {
			t.Errorf("the stored lab key %s is still there, want it gone", s.Name)
		}
	}
	// An operator's own key is theirs.
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{Name: "my-key", SecretType: "api-key", OwnerKind: "platform",
		Fields: map[string]string{"key": "mine"}}, all, true); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.MergePatchSettingBinding(ctx, "", "platform", "assistant", map[string]any{"provider": "openai", "key_secret": "my-key"}); err != nil {
		t.Fatal(err)
	}
	gw.Close()
	seed()
	gw, _, err = openLabDB(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if m, err := gw.OmnibotModel(ctx); err != nil || m == nil || m.Key != "mine" {
		t.Fatalf("an operator's own key after the seed: model = %+v, %v", m, err)
	}
}

// A health storyline that names rooms a lab of this size has not is refused
// before the database is touched (#2728): a small corporate lab has no
// headquarters boardroom for the storyline to land on.
func TestLabScaleSeedRefusesAStorylineItsPlanCannotHold(t *testing.T) {
	err := runLabScaleSeed(context.Background(), &bytes.Buffer{}, "corporate", 3, lab.LoginUser, lab.LoginPassword, "", nil, false, false, true)
	if err == nil || !strings.Contains(err.Error(), "hq-mr86-1") {
		t.Fatalf("a 3-system lab with --health-history: %v, want a refusal naming the storyline's missing rooms", err)
	}
}

// A scale lab's seed leaves each site outpost's world file for its simulator
// (#2773): resolved from the scenario at the lab's size, one per site, each
// the world the pure plan-to-world function gives, loadable by the simulator.
func TestLabScaleSeedWritesEachSiteOutpostsWorldFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "world")
	if err := writeLabWorlds("corporate", 20, dir); err != nil {
		t.Fatal(err)
	}
	plan := corporateScalePlan(t, 20)
	m, err := labscenario.Shipped("corporate")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := labscenario.ShippedCatalog()
	if err != nil {
		t.Fatal(err)
	}
	want, err := labscenario.SiteWorlds(m, cat, plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, site := range plan.Sites {
		outpost := labscenario.SiteOutpost(site.Name)
		got, err := world.Load(filepath.Join(dir, outpost+".json"))
		if err != nil {
			t.Fatal(err)
		}
		a, _ := got.Marshal()
		b, _ := want[outpost].Marshal()
		if string(a) != string(b) {
			t.Errorf("%s's world file is not the plan's world", outpost)
		}
	}
	if err := writeLabWorlds("no-such-scenario", 20, dir); err == nil {
		t.Error("an unknown scenario wrote worlds")
	}
}
