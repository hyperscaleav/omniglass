//go:build linux

package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

const protocolsPortShift = 25000

// simulatedProductOn picks a seeded product on driverID that a simulator
// profile serves, derived from both catalogs rather than named.
func simulatedProductOn(t *testing.T, driverID string) string {
	t.Helper()
	raw, err := seed.FactsJSON()
	if err != nil {
		t.Fatal(err)
	}
	var facts struct {
		Products []struct {
			ID       string `json:"id"`
			DriverID string `json:"driver_id"`
		} `json:"products"`
	}
	if err := json.Unmarshal(raw, &facts); err != nil {
		t.Fatal(err)
	}
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range facts.Products {
		if _, _, ok := store.ForProduct(p.ID); ok && p.DriverID == driverID {
			names = append(names, p.ID)
		}
	}
	if len(names) == 0 {
		t.Fatalf("no simulated product on driver %s", driverID)
	}
	sort.Strings(names)
	return names[0]
}

// TestOperatorMonitorsAndConfiguresSimulatedDevices is what the simulator is
// for, driven as an operator would: endpoints attached through the CLI with
// the shipped drivers, the real collector polling simulated devices over
// SNMP and a line protocol, the observations landing on the components, and
// a command issued through the CLI changing the device.
func TestOperatorMonitorsAndConfiguresSimulatedDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("simulator e2e: skipped under -short (Postgres testcontainer + go build)")
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
	addr := "127.0.0.1:" + freePort(t)
	natsPort := freePort(t)
	srvCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	srv := exec.CommandContext(srvCtx, binPath, "server")
	srv.Env = append(os.Environ(),
		"OMNIGLASS_DSN="+dsn, "OMNIGLASS_ADDR="+addr,
		"OMNIGLASS_NATS_ADDR=127.0.0.1:"+natsPort, "OMNIGLASS_NATS_URL=nats://127.0.0.1:"+natsPort,
		"OMNIGLASS_NATS_STORE_DIR="+t.TempDir(),
	)
	srv.Stdout, srv.Stderr = os.Stderr, os.Stderr
	if err := srv.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { cancel(); _ = srv.Wait() })
	server := "http://" + addr
	pollHealthz(t, server+"/api/v1/healthz")

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
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-p"}, all, all); err != nil {
		t.Fatal(err)
	}
	nodeTok := "protocols-e2e-token"
	sum := sha256.Sum256([]byte(nodeTok))
	if _, err := gw.SetEnrollmentToken(ctx, "", "edge-p", hex.EncodeToString(sum[:]), all); err != nil {
		t.Fatal(err)
	}
	snmpProduct, nvpProduct := simulatedProductOn(t, "snmp-generic"), simulatedProductOn(t, "newtron-nvp")
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	for name, product := range map[string]string{"sim-display": snmpProduct, "sim-panel": nvpProduct} {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name}, all, all, all, all); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx, `update component set product_id = (select id from product where name = $1) where name = $2`, product, name); err != nil {
			t.Fatal(err)
		}
	}

	// The operator's configuration, through the CLI: a community secret and
	// two driver attaches placed on the node.
	cli := runCLI(t, root, binPath, os.Environ())
	as := func(args ...string) string {
		t.Helper()
		out, code := cli(append(args, "--server", server, "--token", ownerTok)...)
		if code != 0 {
			t.Fatalf("omniglass %v exit %d:\n%s", args, code, out)
		}
		return out
	}
	as("secret", "create", "--name", "sim-community", "--secret-type", "snmp-community",
		"--owner-kind", "platform", "--fields", `{"community":"public"}`)
	displayIP, panelIP := "127.34.0.10", "127.34.0.11"
	as("endpoint", "create", "--component", "sim-display", "--node", "edge-p", "--driver", "snmp-generic",
		"--inputs", fmt.Sprintf(`{"host":%q,"port":"%d","community":"sim-community"}`, displayIP, 161+protocolsPortShift))
	as("endpoint", "create", "--component", "sim-panel", "--node", "edge-p", "--driver", "newtron-nvp",
		"--inputs", fmt.Sprintf(`{"host":%q}`, panelIP))

	// The simulator and the collector beside each other, on the node's identity.
	nodeEnv := append(os.Environ(), "OMNIGLASS_SERVER="+server, "OMNIGLASS_NODE_NAME=edge-p", "OMNIGLASS_NODE_TOKEN="+nodeTok)
	for _, args := range [][]string{
		{"sim", "run", "--port-offset", fmt.Sprint(protocolsPortShift), "--poll", "300ms"},
		{"node", "run", "--server", server, "--name", "edge-p", "--token", nodeTok, "--heartbeat", "1s"},
	} {
		pctx, stop := context.WithCancel(ctx)
		p := exec.CommandContext(pctx, binPath, args...)
		p.Env = nodeEnv
		p.Stdout, p.Stderr = os.Stderr, os.Stderr
		if err := p.Start(); err != nil {
			t.Fatalf("start %v: %v", args, err)
		}
		t.Cleanup(func() { stop(); _ = p.Wait() })
	}

	// Monitoring: each driver's first poll lands observed values.
	observed := func(component, property string) string {
		out, code := cli("component", "property", "sample", "list", component, property, "--server", server, "--token", ownerTok)
		if code != 0 {
			return ""
		}
		var series struct {
			Samples []struct {
				Value      json.RawMessage `json:"value"`
				Provenance string          `json:"provenance"`
			} `json:"samples"`
		}
		if json.Unmarshal([]byte(out), &series) != nil {
			return ""
		}
		for _, s := range series.Samples {
			if s.Provenance == "observed" {
				var v string
				_ = json.Unmarshal(s.Value, &v)
				return v
			}
		}
		return ""
	}
	waitFor(t, "an SNMP-observed model number on the display", func() bool { return observed("sim-display", "model-number") != "" })
	waitFor(t, "an NVP-observed video input on the panel", func() bool { return observed("sim-panel", "video-input") == "hdmi1" })

	// Configuration: a command issued through the CLI changes the device.
	as("component", "command", "issue", "sim-panel", "--command-type", "set-input", "--value", "hdmi3")
	lx := collection.NewLineExchanger()
	waitFor(t, "the panel to report the commanded input", func() bool {
		got, err := lx.Exchange(ctx, net.JoinHostPort(panelIP, "51325"), "GET INPUT", time.Second)
		return err == nil && got == "INPUT hdmi3"
	})
	// The device's reply settles the command (ADR-0141), well inside its
	// window and without waiting for the next poll.
	waitFor(t, "the command to settle from the panel's reply", func() bool {
		var status string
		err := conn.QueryRow(ctx, `select status from command order by id desc limit 1`).Scan(&status)
		return err == nil && status == "settled"
	})
}
