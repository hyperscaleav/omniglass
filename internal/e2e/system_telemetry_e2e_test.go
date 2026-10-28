package e2e

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestASystemOwnsItsMetricsAndProperties drives a system's own telemetry as
// an operator and an integrator would (#422, ADR-0422), against the real
// server and its bus, through the generated CLI alone. Two producers write a
// system-owned sample: a calc rule over the system's members (the primary
// path), and a push naming the system as its owner (telemetry about the room
// as a whole, from a cloud service). Both read back on the system's own
// surfaces: its effective metrics, its properties read (the pushed property a
// row with its reconciliation, #1297), and a property's change history.
func TestASystemOwnsItsMetricsAndProperties(t *testing.T) {
	if testing.Short() {
		t.Skip("system telemetry e2e: skipped under -short (Postgres testcontainer + go build)")
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

	cli := runCLI(t, root, binPath, os.Environ())
	run := func(args ...string) string {
		t.Helper()
		out, code := cli(append(args, "--server", server, "--token", ownerTok)...)
		if code != 0 {
			t.Fatalf("%v exit %d:\n%s", args, code, out)
		}
		return out
	}
	// A huddle room staffed by two components, each a role's occupant.
	run("system", "create", "--name", "huddle-1")
	for _, role := range []string{"left", "right"} {
		run("system", "role", "update", "huddle-1", role, "--label", role, "--quorum", "1", "--impact", "none")
	}
	all := scope.Set{All: true}
	for role, comp := range map[string]string{"left": "panel-l", "right": "panel-r"} {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: comp}, all, all, all, all); err != nil {
			t.Fatal(err)
		}
		run("system", "role", "assignment", "update", "huddle-1", role, comp)
	}

	// The primary path: a system calc rule over its members writes a
	// metric and a property on the system itself.
	run("metric-type", "create", "--name", "room-temp-avg", "--data-type", "float")
	run("property-type", "create", "--name", "room-comfort", "--data-type", "string")
	run("calc-rule", "create", "--name", "huddle-temp", "--owner-kind", "system",
		"--inputs", `[{"alias":"t","key":"room-temperature"}]`, "--reduce", `{"expr":"avg(t)"}`, "--output-key", "room-temp-avg")
	run("calc-rule", "create", "--name", "huddle-comfort", "--owner-kind", "system",
		"--inputs", `[{"alias":"t","key":"room-temperature"}]`, "--reduce", `{"expr":"max(t) > 23.0 ? \"warm\" : \"fine\""}`, "--output-key", "room-comfort")
	run("telemetry", "push", "--owner", `{"kind":"component","ref":"panel-l"}`, "--metrics", `[{"name":"room-temperature","value":21}]`)
	run("telemetry", "push", "--owner", `{"kind":"component","ref":"panel-r"}`, "--metrics", `[{"name":"room-temperature","value":24}]`)

	// The push path: a cloud service reports for the room as a whole.
	pushed := run("telemetry", "push", "--owner", `{"kind":"system","ref":"huddle-1"}`, "--source", "room-booking-cloud",
		"--metrics", `[{"name":"occupancy-count","value":4}]`, "--properties", `[{"name":"video-input","value":"hdmi2"}]`)
	if !strings.Contains(pushed, `"metrics": 1`) || !strings.Contains(pushed, `"properties": 1`) {
		t.Fatalf("the system push did not accept its metric and property:\n%s", pushed)
	}

	type effective struct {
		Name       string          `json:"metric_type_name"`
		Property   string          `json:"property_type_name"`
		Value      json.RawMessage `json:"value"`
		SourceRule string          `json:"source_rule"`
	}
	read := func(args ...string) map[string]effective {
		t.Helper()
		out, code := cli(append(args, "--server", server, "--token", ownerTok)...)
		got := map[string]effective{}
		if code != 0 {
			return got
		}
		var body struct {
			Metrics    []effective `json:"metrics"`
			Properties []effective `json:"properties"`
		}
		if err := json.Unmarshal([]byte(out), &body); err != nil {
			return got
		}
		for _, m := range body.Metrics {
			got[m.Name] = m
		}
		for _, p := range body.Properties {
			got[p.Property] = p
		}
		return got
	}
	waitFor(t, "the system's metrics: the calc rule's average and the pushed count", func() bool {
		m := read("system", "metric", "list", "huddle-1")
		return string(m["room-temp-avg"].Value) == "22.5" && m["room-temp-avg"].SourceRule == "huddle-temp" &&
			string(m["occupancy-count"].Value) == "4" && m["occupancy-count"].SourceRule == ""
	})
	waitFor(t, "the system's properties: the calc rule's verdict", func() bool {
		p := read("system", "property", "list", "huddle-1")
		return string(p["room-comfort"].Value) == `"warm"` && p["room-comfort"].SourceRule == "huddle-comfort"
	})
	// The pushed property is one of the system's properties (#1297): nothing
	// declares it, so it reads want null, its observed is, and no drift; the
	// calc rule's verdict carries no reconciliation at all.
	type reconciled struct {
		Property       string `json:"property_type_name"`
		Reconciliation *struct {
			Want       json.RawMessage `json:"want"`
			Is         json.RawMessage `json:"is"`
			IsInstance string          `json:"is_instance"`
			Drift      bool            `json:"drift"`
		} `json:"reconciliation"`
	}
	var props struct {
		Properties []reconciled `json:"properties"`
	}
	if err := json.Unmarshal([]byte(run("system", "property", "list", "huddle-1")), &props); err != nil {
		t.Fatalf("decode the system's properties: %v", err)
	}
	rows := map[string]reconciled{}
	for _, p := range props.Properties {
		rows[p.Property] = p
	}
	if r := rows["video-input"].Reconciliation; r == nil || string(r.Want) != "null" || string(r.Is) != `"hdmi2"` || r.IsInstance != "" || r.Drift {
		t.Fatalf("the pushed video-input on the system's properties: want want=null is=hdmi2, owner-level, no drift; got %+v", rows["video-input"])
	}
	if r := rows["room-comfort"].Reconciliation; r != nil {
		t.Fatalf("the calc rule's room-comfort carries a reconciliation: %+v", r)
	}

	history := run("system", "property", "sample", "list", "huddle-1", "video-input")
	if !strings.Contains(history, `"value": "hdmi2"`) || !strings.Contains(history, `"provenance": "observed"`) ||
		!strings.Contains(history, `"source": "room-booking-cloud"`) {
		t.Fatalf("the pushed property's history:\n%s", history)
	}

	// A system owns no log lines: the CLI exits non-zero with the reason.
	out, code := cli("telemetry", "push", "--owner", `{"kind":"system","ref":"huddle-1"}`, "--logs", `[{"message":"sync ok"}]`,
		"--server", server, "--token", ownerTok)
	if code == 0 || !strings.Contains(out, "log line is component-owned") {
		t.Fatalf("a system push with a log line exit %d:\n%s", code, out)
	}
}
