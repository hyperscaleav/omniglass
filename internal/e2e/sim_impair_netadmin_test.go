//go:build linux && netadmin

// The impairment loop end to end, in a network namespace of the test's own
// with NET_ADMIN (make test-netadmin runs it in a container): never on a
// host whose firewall the simulator would rewrite.
package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestImpairingASimulatedDeviceShowsInCollection drives the operator's
// loop: a simulated device collected by a real node, unplugged through the
// CLI (its ping and TCP checks fail and both endpoints read down),
// replugged (all recover), then slowed (the delay shows in its TCP connect
// time).
func TestImpairingASimulatedDeviceShowsInCollection(t *testing.T) {
	if os.Getenv("OMNIGLASS_NETADMIN") != "1" {
		t.Fatal("this test rewrites the network it runs in; run it through make test-netadmin")
	}
	ctx := context.Background()
	root := repoRoot(t)
	binPath := filepath.Join(t.TempDir(), "omniglass")
	build := exec.CommandContext(ctx, "go", "build", "-o", binPath, "./cmd/omniglass")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	dsn := storagetest.NewDSN(t)
	server := startServer(t, binPath, "OMNIGLASS_DSN="+dsn).URL

	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-i"}, all, all); err != nil {
		t.Fatal(err)
	}
	nodeTok := "impair-e2e-token"
	sum := sha256.Sum256([]byte(nodeTok))
	if _, err := gw.SetEnrollmentToken(ctx, "", "edge-i", hex.EncodeToString(sum[:]), all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "sim-dev"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	// It attaches snmp-generic to the product below, so the pick serves snmp.
	product, port := e2eSimulatedProduct(t, ctx, conn, "snmp")
	if _, err := conn.Exec(ctx, `update component set product_id = (select id from product where name = $1) where name = 'sim-dev'`, product); err != nil {
		t.Fatal(err)
	}
	// sim-dev is the one occupant of a role whose impairment takes its system
	// out, so the official reachability rule's alarm moves the system.
	if err := gw.UpsertStandard(ctx, storage.Standard{Name: "impair-std", Label: "Impair standard"}); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.SetSystemRole(ctx, "", "standard", "impair-std", storage.SystemRoleSpec{Name: "target", Quorum: 1, Impact: "outage"}); err != nil {
		t.Fatal(err)
	}
	std := "impair-std"
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "impair-sys", StandardID: &std}, all, all); err != nil {
		t.Fatal(err)
	}
	if err := gw.AssignRole(ctx, "", "impair-sys", "target", "sim-dev", all, all, all, all); err != nil {
		t.Fatal(err)
	}

	cli := runCLI(t, root, binPath, os.Environ())
	as := func(args ...string) string {
		t.Helper()
		out, code := cli(append(args, "--server", server, "--token", ownerTok)...)
		if code != 0 {
			t.Fatalf("omniglass %v exit %d:\n%s", args, code, out)
		}
		return out
	}
	ip := "127.62.0.10"
	as("endpoint", "create", "--component", "sim-dev", "--node", "edge-i", "--transport", "tcp",
		"--params", fmt.Sprintf(`{"target":"%s:%d"}`, ip, port))
	as("endpoint", "create", "--component", "sim-dev", "--node", "edge-i", "--transport", "icmp",
		"--params", fmt.Sprintf(`{"target":%q}`, ip))

	nodeEnv := append(os.Environ(), "OMNIGLASS_SERVER="+server, "OMNIGLASS_NODE_NAME=edge-i", "OMNIGLASS_NODE_TOKEN="+nodeTok)
	for _, args := range [][]string{
		{"sim", "run", "--impairments", "--poll", "300ms"},
		{"node", "run", "--server", server, "--name", "edge-i", "--token", nodeTok, "--heartbeat", "1s"},
	} {
		pctx, stop := context.WithCancel(ctx)
		p := exec.CommandContext(pctx, binPath, args...)
		p.Env = nodeEnv
		p.Stdout, p.Stderr = os.Stderr, os.Stderr
		if err := p.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { stop(); _ = p.Wait() })
	}

	get := func(path string, into any) bool {
		req, _ := http.NewRequest(http.MethodGet, server+"/api/v1"+path, nil)
		req.Header.Set("Authorization", "Bearer "+ownerTok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode == http.StatusOK && json.Unmarshal(b, into) == nil
	}
	// metric reads the device's latest sample of a metric, by the series
	// its probe writes (the endpoint name is the transport).
	metric := func(name string) (float64, time.Time, bool) {
		var out struct {
			Samples []struct {
				Value float64   `json:"value"`
				TS    time.Time `json:"ts"`
			} `json:"samples"`
		}
		if !get("/components/sim-dev/metrics/"+name+"/samples?limit=1", &out) || len(out.Samples) == 0 {
			return 0, time.Time{}, false
		}
		s := out.Samples[len(out.Samples)-1]
		return s.Value, s.TS, true
	}
	// reach reads each endpoint's reachability verdict: the panel an
	// operator watches.
	reach := func() map[string]string {
		var r struct {
			Endpoints []struct {
				Endpoint string `json:"endpoint"`
				Verdict  *struct {
					Value string `json:"value"`
				} `json:"verdict"`
			} `json:"endpoints"`
		}
		out := map[string]string{}
		if get("/components/sim-dev/reachability", &r) {
			for _, e := range r.Endpoints {
				if e.Verdict != nil {
					out[e.Endpoint] = e.Verdict.Value
				}
			}
		}
		return out
	}
	both := func(v string) func() bool {
		return func() bool { r := reach(); return r["tcp"] == v && r["icmp"] == v }
	}
	// after waits for a fresh sample of a metric taken after since, and
	// checks it against want.
	after := func(what, name string, since time.Time, want func(float64) bool) {
		t.Helper()
		waitFor(t, what, func() bool {
			v, ts, ok := metric(name)
			return ok && ts.After(since) && want(v)
		})
	}
	up := func(v float64) bool { return v == 1 }
	down := func(v float64) bool { return v == 0 }

	start := time.Now()
	after("TCP to answer", "tcp-open", start, up)
	after("ping to answer", "icmp-reachable", start, up)
	waitFor(t, "both endpoints to read up", both("up"))

	// ruleAlarms counts sim-dev's open alarms raised by the official
	// reachability rule; verdict reads its system's health.
	ruleAlarmIDs := func() []string {
		var out struct {
			Alarms []struct {
				ID           string `json:"id"`
				RaisedByRule string `json:"raised_by_rule"`
			} `json:"alarms"`
		}
		if !get("/components/sim-dev/alarms", &out) {
			return nil
		}
		var ids []string
		for _, a := range out.Alarms {
			if a.RaisedByRule == "reachability-lost" {
				ids = append(ids, a.ID)
			}
		}
		return ids
	}
	ruleAlarms := func() int { return len(ruleAlarmIDs()) }
	// ruleEvents maps each alarm id the official rule's events of a type
	// carry to how many carry it (#949): the edges it wrote on sim-dev.
	ruleEvents := func(key string) map[string]int {
		var out struct {
			Events []struct {
				Key           string `json:"key"`
				Origin        string `json:"origin"`
				DerivedByRule string `json:"derived_by_rule"`
				AlarmID       string `json:"alarm_id"`
			} `json:"events"`
		}
		got := map[string]int{}
		if !get("/components/sim-dev/events", &out) {
			return got
		}
		for _, e := range out.Events {
			if e.Key == key && e.Origin == "derived" && e.DerivedByRule == "reachability-lost" {
				got[e.AlarmID]++
			}
		}
		return got
	}
	verdict := func() string {
		var out struct {
			Verdict string `json:"verdict"`
		}
		if !get("/systems/impair-sys/health", &out) {
			return ""
		}
		return out.Verdict
	}
	waitFor(t, "the system to read healthy", func() bool { return verdict() == "healthy" })

	unplugged := time.Now()
	as("component", "impairment", "set", "sim-dev", "--unplugged")
	// The sustain is honored: the first down sample lands after the unplug,
	// so the rule's 15s fire sustain keeps any alarm shut for at least 15s
	// of it. Watching the first 10s leaves 5s of margin for a slow poll.
	for time.Since(unplugged) < 10*time.Second {
		if n := ruleAlarms(); n > 0 {
			t.Fatalf("the reachability rule opened %d alarms %s after the unplug, inside its 15s sustain", n, time.Since(unplugged).Round(time.Millisecond))
		}
		time.Sleep(250 * time.Millisecond)
	}
	after("TCP to fail once unplugged", "tcp-open", unplugged, down)
	after("ping to fail once unplugged", "icmp-reachable", unplugged, down)
	waitFor(t, "both endpoints to read down", both("down"))
	// The loop closes with no hand-raised alarm: once the verdict has read
	// down for the rule's sustain, the sweep opens the alarm and the system
	// leaves healthy.
	waitForWithin(t, 60*time.Second, "the reachability rule to open an alarm", func() bool { return ruleAlarms() > 0 })
	waitFor(t, "the system to leave healthy", func() bool { v := verdict(); return v != "" && v != "healthy" })
	// Each alarm the rule opened was opened by a fire event carrying it, once.
	waitFor(t, "an alarm per endpoint", func() bool { return ruleAlarms() == 2 })
	opened := ruleAlarmIDs()
	fired := ruleEvents("condition-started")
	for _, id := range opened {
		if fired[id] != 1 {
			t.Fatalf("alarm %s was opened by %d condition-started events (all: %v), want exactly 1 carrying it", id, fired[id], fired)
		}
	}

	replugged := time.Now()
	as("component", "impairment", "set", "sim-dev")
	after("TCP to recover once replugged", "tcp-open", replugged, up)
	after("ping to recover once replugged", "icmp-reachable", replugged, up)
	waitFor(t, "both endpoints to read up again", both("up"))
	waitFor(t, "the reachability alarms to clear", func() bool { return ruleAlarms() == 0 })
	// Each was resolved by a clear event carrying it, once.
	cleared := ruleEvents("condition-ended")
	for _, id := range opened {
		if cleared[id] != 1 {
			t.Fatalf("alarm %s was resolved by %d condition-ended events (all: %v), want exactly 1 carrying it", id, cleared[id], cleared)
		}
	}
	waitFor(t, "the system to read healthy again", func() bool { return verdict() == "healthy" })

	slowed := time.Now()
	as("component", "impairment", "set", "sim-dev", "--latency-ms", "300")
	after("the delay to show in the TCP connect time", "tcp-connect-time", slowed, func(ms float64) bool { return ms >= 290 })

	// snmp's rung under loss (#821): the device's v2c agent, collected with
	// snmp-generic, reads up; every datagram toward it dropped, its get goes
	// unanswered and the endpoint reads down; the loss lifted, up again.
	as("component", "impairment", "set", "sim-dev")
	as("secret", "create", "--name", "sim-community", "--secret-type", "snmp-community",
		"--owner-kind", "platform", "--fields", `{"community":"public"}`)
	as("endpoint", "create", "--component", "sim-dev", "--node", "edge-i", "--driver", "snmp-generic",
		"--inputs", fmt.Sprintf(`{"host":%q,"port":"%d","community":"sim-community"}`, ip, e2eSNMPPort(t, product)))
	snmp := func(v string) func() bool { return func() bool { return reach()["snmp"] == v } }
	waitFor(t, "the snmp endpoint to read up", snmp("up"))
	as("component", "impairment", "set", "sim-dev", "--loss-pct", "100")
	waitFor(t, "the snmp endpoint to read down under loss", snmp("down"))
	as("component", "impairment", "set", "sim-dev")
	waitFor(t, "the snmp endpoint to read up once the loss lifted", snmp("up"))
}

// e2eSNMPPort is the port the product's simulator profile serves SNMP on.
func e2eSNMPPort(t *testing.T, product string) int {
	t.Helper()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := store.Devices[product]; ok {
		for _, iface := range p.Interfaces {
			if iface.Protocol == "snmp" {
				return iface.Port
			}
		}
	}
	t.Fatalf("the simulated product %s serves no snmp face", product)
	return 0
}
