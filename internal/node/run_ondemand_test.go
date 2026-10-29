//go:build linux

package node_test

import (
	"encoding/json"
	"fmt"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The on-demand node run over the real wire (#1163): the API, the bus, a real
// node and the simulator's digest-guarded display. A broken spec validates to
// its findings; the fixed spec previews through the node, answering the
// display's identity with the raw answer beside it and landing nothing; the
// shipped driver attached, its poll runs now and lands; a wrong password gates
// the endpoint and the next run now is refused with the gate's reason, sending
// nothing. Skipped under -short.

type runSample struct {
	Name  string   `json:"name"`
	Value *float64 `json:"value"`
	Text  string   `json:"text"`
}

type pollRun struct {
	Poll    string      `json:"poll"`
	Samples []runSample `json:"samples"`
	Faults  []string    `json:"faults"`
	Excerpt string      `json:"excerpt"`
}

func TestOnDemandRunsOverTheWire(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	prof := loginProfile(t, store, "digest", "POST /api/display/power")
	auth := faceAuth(store, prof)
	s := newHTTPStack(t, "edge-run", nil, func(st *profile.Store) []string { return []string{prof} })
	ip, port := s.ips[prof], panelPort(t, s.devices[prof], "http")
	owner := s.ownerToken()
	sec, err := s.gw.CreateSecret(s.ctx, "", storage.SecretSpec{
		Name: "display-login", SecretType: "basic-auth", OwnerKind: "platform",
		Fields: map[string]string{"username": auth.DefaultUser, "password": auth.DefaultPassword},
	}, s.all, true)
	if err != nil {
		t.Fatalf("create secret: %v", err)
	}
	shipped, err := s.gw.GetDriver(s.ctx, "boreal-edge-web")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(shipped.Spec, &spec); err != nil {
		t.Fatal(err)
	}
	inputs := map[string]string{"host": ip, "port": fmt.Sprint(port), "login": "display-login"}

	// Validate: the broken spec's findings, then the fixed one.
	broken := strings.Replace(string(shipped.Spec), `"model-number"`, `"no-such-key"`, 1)
	var checked struct {
		Valid    bool `json:"valid"`
		Findings []struct {
			Kind, Function, Message string
		} `json:"findings"`
	}
	if code := s.api(owner, http.MethodPost, "/drivers:validate", map[string]any{"spec": json.RawMessage(broken)}, &checked); code != http.StatusOK || checked.Valid || len(checked.Findings) == 0 || checked.Findings[0].Function != "identity" {
		t.Fatalf("validate broken = %d %+v", code, checked)
	}
	if code := s.api(owner, http.MethodPost, "/drivers:validate", map[string]any{"spec": spec}, &checked); code != http.StatusOK || !checked.Valid || len(checked.Findings) != 0 {
		t.Fatalf("validate fixed = %d %+v", code, checked)
	}

	s.runNode("edge-run")
	samples := func() int64 {
		conn, err := pgx.Connect(s.ctx, s.dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close(s.ctx)
		var n int64
		if err := conn.QueryRow(s.ctx, `select (select count(*) from property) + (select count(*) from `+storagetest.MetricRows+` metric)`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := samples()

	// Preview: the node answers the display's identity and lands nothing.
	var preview struct {
		Node, Target string
		Stored       bool
		Results      []pollRun `json:"results"`
	}
	body := map[string]any{"node": "edge-run", "spec": spec, "inputs": inputs}
	deadline := time.Now().Add(20 * time.Second)
	code := 0
	for time.Now().Before(deadline) {
		if code = s.api(owner, http.MethodPost, "/drivers:preview", body, &preview); code != http.StatusServiceUnavailable {
			break
		}
		time.Sleep(100 * time.Millisecond) // the node has not subscribed yet
	}
	if code != http.StatusOK || preview.Stored || preview.Node != "edge-run" || len(preview.Results) != 1 {
		t.Fatalf("preview = %d %+v", code, preview)
	}
	got := map[string]runSample{}
	for _, smp := range preview.Results[0].Samples {
		got[smp.Name] = smp
	}
	if got["model-number"].Text == "" || got["serial-number"].Text == "" || len(preview.Results[0].Faults) != 0 {
		t.Fatalf("preview read %+v", preview.Results[0])
	}
	if !strings.Contains(preview.Results[0].Excerpt, got["model-number"].Text) || strings.Contains(preview.Results[0].Excerpt, auth.DefaultPassword) {
		t.Errorf("excerpt = %q, want the raw answer and never the password", preview.Results[0].Excerpt)
	}
	time.Sleep(500 * time.Millisecond)
	if after := samples(); after != before {
		t.Errorf("a preview landed %d samples, want none", after-before)
	}

	// Run now: attach the shipped driver and run its poll.
	s.component("display-run")
	var ep struct {
		Name string `json:"name"`
	}
	if code := s.api(owner, http.MethodPost, "/endpoints", map[string]any{
		"component": "display-run", "driver": "boreal-edge-web", "node": "edge-run", "inputs": inputs,
	}, &ep); code != http.StatusCreated {
		t.Fatalf("attach = %d", code)
	}
	path := "/endpoints/display-run%2F" + ep.Name + "/polls/identity:run"
	var run struct {
		Node    string      `json:"node"`
		Stored  bool        `json:"stored"`
		Poll    string      `json:"poll"`
		Samples []runSample `json:"samples"`
		Faults  []string    `json:"faults"`
	}
	if code := s.api(owner, http.MethodPost, path, nil, &run); code != http.StatusOK || !run.Stored || run.Poll != "identity" || len(run.Samples) == 0 || len(run.Faults) != 0 {
		t.Fatalf("run now = %d %+v", code, run)
	}
	s.waitFor("display-run", ep.Name, "model-number", func(v string) bool { return v != "" }, "the run's model-number landed")
	if code := s.api(owner, http.MethodPost, "/endpoints/display-run%2F"+ep.Name+"/polls/nope:run", nil, nil); code != http.StatusNotFound {
		t.Errorf("an unknown poll = %d, want 404", code)
	}

	// A wrong password gates the endpoint; the next run now is refused with
	// the gate's reason and sends nothing.
	s.setPassword(sec, "not-the-password")
	s.waitFor("display-run", ep.Name, "endpoint-rejection", func(v string) bool { return !ungated(v) }, "a gate after the wrong password")
	var refused struct {
		Detail string `json:"detail"`
	}
	if code := s.api(owner, http.MethodPost, path, nil, &refused); code != http.StatusConflict {
		t.Fatalf("a run now on a gated endpoint = %d, want 409", code)
	}
}
