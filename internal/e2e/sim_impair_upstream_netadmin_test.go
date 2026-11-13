//go:build linux && netadmin

package e2e

import (
	"bytes"
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
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestImpairingAnUpstreamHoldsItsPathDependent drives a path dependency
// end to end (#1652): two simulated devices on one node, the panel reached
// through the gateway. Unplugging the gateway closes its gate, and the panel's
// gate takes the upstream reason naming the gateway in the same pass: its
// probes are held and counted (poll-held-count climbs), so its own probe
// result never flips and it raises nothing of its own. Replugged, the
// gateway opens, the panel's probe runs again, and its pass releases it.
func TestImpairingAnUpstreamHoldsItsPathDependent(t *testing.T) {
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
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-u"}, all, all); err != nil {
		t.Fatal(err)
	}
	nodeTok := "upstream-e2e-token"
	sum := sha256.Sum256([]byte(nodeTok))
	if _, err := gw.SetEnrollmentToken(ctx, "", "edge-u", hex.EncodeToString(sum[:]), all); err != nil {
		t.Fatal(err)
	}
	// The shortest report window the setting takes, so the held count lands
	// inside the test rather than a minute on.
	if _, err := gw.MergePatchSettingOverride(ctx, "", "platform", "collection", map[string]any{"report_seconds": 10}); err != nil {
		t.Fatal(err)
	}
	upstream, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "gateway-u"}, all, all, all, all)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-u"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	product, port := e2eSimulatedProduct(t, ctx, conn)
	if _, err := conn.Exec(ctx, `update component set product_id = (select id from product where name = $1) where name in ('gateway-u', 'panel-u')`, product); err != nil {
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
	ips := map[string]string{"gateway-u": "127.62.0.20", "panel-u": "127.62.0.21"}
	for comp, ip := range ips {
		as("endpoint", "create", "--component", comp, "--node", "edge-u", "--transport", "tcp",
			"--params", fmt.Sprintf(`{"target":"%s:%d"}`, ip, port))
		as("endpoint", "create", "--component", comp, "--node", "edge-u", "--transport", "icmp",
			"--params", fmt.Sprintf(`{"target":%q}`, ip))
	}
	as("component", "dependency", "create", "panel-u", "--kind", "path", "--upstream", "gateway-u")

	nodeEnv := append(os.Environ(), "OMNIGLASS_SERVER="+server, "OMNIGLASS_NODE_NAME=edge-u", "OMNIGLASS_NODE_TOKEN="+nodeTok)
	for _, args := range [][]string{
		{"sim", "run", "--impairments", "--poll", "300ms"},
		{"node", "run", "--server", server, "--name", "edge-u", "--token", nodeTok, "--heartbeat", "1s", "--probe-every", "1s", "--worklist-every", "1s"},
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

	call := func(method, path string, body, into any) bool {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, server+"/api/v1"+path, rd)
		req.Header.Set("Authorization", "Bearer "+ownerTok)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode == http.StatusOK && (into == nil || json.Unmarshal(b, into) == nil)
	}
	get := func(path string, into any) bool { return call(http.MethodGet, path, nil, into) }
	impair := func(comp string, fields map[string]any) {
		t.Helper()
		if !call(http.MethodPut, "/components/"+comp+"/impairment", fields, nil) {
			t.Fatalf("PUT /components/%s/impairment %v refused", comp, fields)
		}
	}

	type gateReason struct {
		Kind      string    `json:"kind"`
		Since     time.Time `json:"since"`
		Source    string    `json:"source"`
		Component string    `json:"component"`
	}
	type endpointRow struct {
		Verdict *struct {
			Value string `json:"value"`
		} `json:"verdict"`
		GateReasons []gateReason `json:"gate_reasons"`
	}
	reach := func(comp string) map[string]endpointRow {
		var r struct {
			Endpoints []struct {
				Endpoint string `json:"endpoint"`
				endpointRow
			} `json:"endpoints"`
		}
		out := map[string]endpointRow{}
		if get("/components/"+comp+"/reachability", &r) {
			for _, e := range r.Endpoints {
				out[e.Endpoint] = e.endpointRow
			}
		}
		return out
	}
	verdicts := func(comp, v string) func() bool {
		return func() bool {
			r := reach(comp)
			return r["tcp"].Verdict != nil && r["tcp"].Verdict.Value == v && r["icmp"].Verdict != nil && r["icmp"].Verdict.Value == v
		}
	}
	// state reads the panel's latest probe result, a change-only property:
	// its newest row is the result in force and its ts is when it flipped.
	state := func(name string) (string, time.Time) {
		var out struct {
			Samples []struct {
				Value string    `json:"value"`
				TS    time.Time `json:"ts"`
			} `json:"samples"`
		}
		if !get("/components/panel-u/properties/"+name+"/samples?limit=1", &out) || len(out.Samples) == 0 {
			return "", time.Time{}
		}
		s := out.Samples[len(out.Samples)-1]
		return s.Value, s.TS
	}
	// held sums the panel's held count over its report windows since t0.
	held := func(t0 time.Time) float64 {
		var out struct {
			Samples []struct {
				Value float64   `json:"value"`
				TS    time.Time `json:"ts"`
			} `json:"samples"`
		}
		if !get("/components/panel-u/metrics/"+probekeys.PollHeldCount+"/samples?limit=100", &out) {
			return 0
		}
		var n float64
		for _, s := range out.Samples {
			if s.TS.After(t0) {
				n += s.Value
			}
		}
		return n
	}

	waitFor(t, "the gateway to read up", verdicts("gateway-u", "up"))
	waitFor(t, "the panel to read up", verdicts("panel-u", "up"))
	openBefore, openTS := state("tcp-open")
	if openBefore != collection.PortOpen {
		t.Fatalf("the panel's tcp-open read %q before the unplug, want %q", openBefore, collection.PortOpen)
	}

	unplugged := time.Now()
	impair("gateway-u", map[string]any{"unplugged": true})
	waitFor(t, "the gateway to read down", verdicts("gateway-u", "down"))
	heldBy := func(r endpointRow) bool {
		for _, g := range r.GateReasons {
			if g.Kind == "upstream" && g.Source == upstream.ID && g.Component == "gateway-u" && !g.Since.Before(unplugged.Add(-time.Second)) {
				return true
			}
		}
		return false
	}
	waitFor(t, "the panel's gate to name upstream gateway-u", func() bool {
		r := reach("panel-u")
		return heldBy(r["tcp"]) && heldBy(r["icmp"])
	})
	// The node's own record of the gate reads the same reason.
	var rej struct {
		Samples []struct {
			Value    string `json:"value"`
			Instance string `json:"instance"`
		} `json:"samples"`
	}
	rejection := ""
	if get("/components/panel-u/properties/endpoint-rejection/samples?limit=50", &rej) {
		for _, s := range rej.Samples { // newest first
			if s.Instance == "tcp" {
				rejection = s.Value
				break
			}
		}
	}
	if !strings.HasPrefix(rejection, "upstream ") || !strings.HasSuffix(rejection, " "+upstream.ID) {
		t.Fatalf("the panel's tcp endpoint-rejection = %q; want upstream since naming %s", rejection, upstream.ID)
	}
	waitForWithin(t, 45*time.Second, "the panel's held count to climb", func() bool { return held(unplugged) >= 4 })
	first := held(unplugged)
	waitForWithin(t, 45*time.Second, "the panel's held count to climb again", func() bool { return held(unplugged) > first })
	// Held, the panel's probes never ran: its result has not flipped and its
	// verdict still reads up, so nothing of its own fired.
	if v, ts := state("tcp-open"); v != collection.PortOpen || !ts.Equal(openTS) {
		t.Fatalf("the held panel's tcp-open moved to %q at %s; want %q from %s", v, ts, collection.PortOpen, openTS)
	}
	if !verdicts("panel-u", "up")() {
		t.Fatalf("the held panel's verdict moved: %+v", reach("panel-u"))
	}

	impair("gateway-u", map[string]any{})
	waitFor(t, "the gateway to read up again", verdicts("gateway-u", "up"))
	waitFor(t, "the panel to be released after its own probe passed", func() bool {
		r := reach("panel-u")
		return len(r["tcp"].GateReasons) == 0 && len(r["icmp"].GateReasons) == 0
	})
	if !verdicts("panel-u", "up")() {
		t.Fatalf("the released panel does not read up: %+v", reach("panel-u"))
	}
}
