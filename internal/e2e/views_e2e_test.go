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

// TestViewBindingsReadEveryPrimitive drives a view binding (#1830, ADR-1830)
// as a script or an agent would, against the real server through the
// generated CLI alone: telemetry of all four primitives pushed onto a
// component reads back through one `view run`, a binding per primitive, and
// `view watch` over a bounded duration carries a row that lands while it
// streams.
func TestViewBindingsReadEveryPrimitive(t *testing.T) {
	if testing.Short() {
		t.Skip("views e2e: skipped under -short (Postgres testcontainer + go build)")
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "lobby-codec"}, all, all, all, all); err != nil {
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
	owner := `{"kind":"component","ref":"lobby-codec"}`
	run("telemetry", "push", "--owner", owner, "--source", "views-e2e",
		"--metrics", `[{"name":"room-temperature","value":21.5}]`,
		"--properties", `[{"name":"video-input","value":"hdmi1"}]`,
		"--events", `[{"name":"call-started","message":"call up"}]`,
		"--logs", `[{"message":"codec rebooted","severity":"warning"}]`)

	bindings := `[
		{"primitive":"metric","kind":"component","refs":["lobby-codec"],"keys":["room-temperature"],"window_seconds":3600},
		{"primitive":"property","kind":"component","refs":["lobby-codec"],"keys":["video-input"],"window_seconds":3600},
		{"primitive":"event","kind":"component","refs":["lobby-codec"],"keys":["call-started"],"window_seconds":3600},
		{"primitive":"log","kind":"component","refs":["lobby-codec"],"window_seconds":3600}
	]`
	type result struct {
		Cursor string `json:"cursor"`
		Metric []struct {
			Points []struct {
				Value float64 `json:"value"`
			} `json:"points"`
		} `json:"metric"`
		Property []struct {
			Points []struct {
				Value string `json:"value"`
			} `json:"points"`
		} `json:"property"`
		Events []struct {
			Message string `json:"message"`
		} `json:"events"`
		Logs []struct {
			Message string `json:"message"`
		} `json:"logs"`
	}
	var got struct {
		Cursor  string   `json:"cursor"`
		Results []result `json:"results"`
	}
	waitFor(t, "every primitive reads back through view run", func() bool {
		if err := json.Unmarshal([]byte(run("view", "run", "--bindings", bindings)), &got); err != nil || len(got.Results) != 4 {
			return false
		}
		r := got.Results
		return len(r[0].Metric) == 1 && len(r[1].Property) == 1 && len(r[2].Events) == 1 && len(r[3].Logs) == 1
	})
	r := got.Results
	if r[0].Metric[0].Points[0].Value != 21.5 || r[1].Property[0].Points[0].Value != "hdmi1" ||
		r[2].Events[0].Message != "call up" || r[3].Logs[0].Message != "codec rebooted" {
		t.Fatalf("view run read back %+v", r)
	}
	if got.Cursor == "" || r[0].Cursor != got.Cursor {
		t.Fatalf("every result carries the snapshot's cursor: run %q, result %q", got.Cursor, r[0].Cursor)
	}

	// A bounded watch from the run's cursor carries the sample pushed while
	// it streams, and not the one the history already holds.
	watching := `[{"primitive":"metric","kind":"component","refs":["lobby-codec"],"keys":["room-temperature"],"window_seconds":3600,"cursor":"` + got.Cursor + `"}]`
	done := make(chan string, 1)
	go func() {
		out, _ := cli("view", "watch", "--bindings", watching, "--duration-seconds", "4", "--server", server, "--token", ownerTok)
		done <- out
	}()
	run("telemetry", "push", "--owner", owner, "--metrics", `[{"name":"room-temperature","value":23.25}]`)
	out := <-done
	if !strings.Contains(out, "event: rows") || !strings.Contains(out, `"value":23.25`) {
		t.Fatalf("view watch did not carry the live sample:\n%s", out)
	}
	if strings.Contains(out, `"value":21.5`) {
		t.Fatalf("view watch repeated a sample the history holds:\n%s", out)
	}
}
