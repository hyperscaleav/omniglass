package lab

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The options only the demo pipeline's scripts use (#1197): a throwaway lab
// of a per-run project, held for a process and torn down with its data, and a
// ready file a script reads to reach it.

// A throwaway lab labels everything it creates as such, so the reaper can
// tell it from a learner's lab, whose data it never removes.
func TestPlanEphemeralLabelsEveryResource(t *testing.T) {
	o := testOptions()
	o.Project, o.Ephemeral = "ogwalk-1-ab", true
	s, err := Plan(o)
	if err != nil {
		t.Fatal(err)
	}
	check := func(what string, l map[string]string) {
		if l[LabelLabEphemeral] != "true" {
			t.Errorf("%s lacks %s=true: %v", what, LabelLabEphemeral, l)
		}
	}
	for _, svc := range s.Services() {
		check("service "+svc.Name, svc.Labels)
	}
	check("the network", s.NetworkLabels)
	for _, v := range s.Volumes {
		check("volume "+v.Key, v.Labels)
	}
	check("the owned labels", OwnedLabels(s))

	plain, err := Plan(testOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, svc := range plain.Services() {
		if _, ok := svc.Labels[LabelLabEphemeral]; ok {
			t.Errorf("a learner's lab marks %s ephemeral", svc.Name)
		}
	}
}

// fakeServerWithTokens is fakeServer plus the token route the ready file's
// token is minted through, counting the mints.
func fakeServerWithTokens(t *testing.T, mints *atomic.Int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "og_session", Value: "s", Path: "/"})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/nodes/edge-hq/simulation", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"connected":true,"devices":[{"component_id":"c-1","status":"running"}]}`)
	})
	mux.HandleFunc("GET /api/v1/components/c-1/reachability", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"endpoints":[{"label":"","verdict":{"value":"up"}}]}`)
	})
	mux.HandleFunc("POST /api/v1/auth/me/tokens", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("og_session"); err != nil || c.Value != "s" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mints.Add(1)
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"token":"ogp_minted","prefix":"ogp_m","description":"x","expires_at":"2099-01-01T00:00:00Z"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// eventually polls until cond holds or the test's patience runs out.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Held with a ready file, the lab writes, once it is ready, everything a
// script needs to reach it from a container on its network: the network, the
// server's address there, an owner token minted for the run, the login, the
// containers and the labels a helper container carries to be torn down with
// the lab.
func TestReadyFileSaysHowToReachTheLab(t *testing.T) {
	var mints atomic.Int32
	srv := fakeServerWithTokens(t, &mints)
	eng := newFakeEngine(strings.TrimPrefix(srv.URL, "http://"))
	file := filepath.Join(t.TempDir(), "lab.env")
	cfg := testConfig()
	cfg.Project, cfg.Ephemeral = "ogwalk-1-ab", true
	cfg.Hold, cfg.ReadyFile = true, file
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var out bytes.Buffer
	go func() { _, err := testRunner(eng, &out).Run(ctx, cfg); done <- err }()
	eventually(t, "the ready file", func() bool { _, err := os.Stat(file); return err == nil })
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	env := string(b)
	for _, want := range []string{
		"LAB_PROJECT='ogwalk-1-ab'",
		"LAB_NETWORK='ogwalk-1-ab_default'",
		"LAB_SERVER_URL='http://server:8080'",
		"LAB_HOST_URL='" + srv.URL + "'",
		"LAB_TOKEN='ogp_minted'",
		"LAB_USER='dev'",
		"LAB_PASSWORD='dev'",
		"LAB_IMAGE='omniglass:dev'",
		"LAB_DSN='postgres://omniglass:omniglass@db:5432/omniglass?sslmode=disable'",
		"LAB_DB='ogwalk-1-ab-db-1'",
		"LAB_SERVER='ogwalk-1-ab-server-1'",
		"LAB_NODE='ogwalk-1-ab-node-1'",
		"LAB_OWNER_PID='42'",
	} {
		if !strings.Contains(env, want+"\n") {
			t.Errorf("the ready file lacks %s:\n%s", want, env)
		}
	}
	for _, want := range []string{
		LabelComposeProject + "=ogwalk-1-ab", LabelLab + "=ogwalk-1-ab", LabelLabHost + "=box",
		LabelLabPID + "=42", LabelLabEphemeral + "=true",
	} {
		if !strings.Contains(env, want) {
			t.Errorf("the ready file's labels lack %s:\n%s", want, env)
		}
	}
	if n := mints.Load(); n != 1 {
		t.Errorf("minted %d tokens, want one per run", n)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("a stopped lab: %v", err)
	}
}

// A throwaway lab's data goes with it: teardown removes its volumes too.
func TestEphemeralTearDownRemovesTheVolumes(t *testing.T) {
	srv := fakeServer(t)
	eng := newFakeEngine(strings.TrimPrefix(srv.URL, "http://"))
	cfg := testConfig()
	cfg.Project, cfg.Ephemeral, cfg.Hold = "ogwalk-1-ab", true, true
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var out bytes.Buffer
	go func() { _, err := testRunner(eng, &out).Run(ctx, cfg); done <- err }()
	eventually(t, "the lab", func() bool { eng.mu.Lock(); defer eng.mu.Unlock(); return len(eng.containers) == 3 })
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if len(eng.containers) != 0 || len(eng.networks) != 0 || len(eng.volumes) != 0 {
		t.Fatalf("after teardown: containers %v networks %v volumes %v, want none", eng.containers, eng.networks, eng.volumes)
	}
}

// Held for a process, the lab tears itself down once that process is gone:
// a script killed too hard to run its trap leaves no lab behind.
func TestHoldEndsWhenTheProcessItHoldsForIsGone(t *testing.T) {
	srv := fakeServer(t)
	eng := newFakeEngine(strings.TrimPrefix(srv.URL, "http://"))
	var gone atomic.Bool
	var out bytes.Buffer
	r := testRunner(eng, &out)
	r.Alive = func(pid int) bool { return pid == 42 || (pid == 77 && !gone.Load()) }
	cfg := testConfig()
	cfg.Hold, cfg.WhilePID = true, 77
	cfg.Project, cfg.Ephemeral = "ogwalk-1-ab", true
	done := make(chan error, 1)
	go func() { _, err := r.Run(context.Background(), cfg); done <- err }()
	eventually(t, "the lab", func() bool { eng.mu.Lock(); defer eng.mu.Unlock(); return len(eng.containers) == 3 })
	gone.Store(true)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%v\n%s", err, out.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the lab kept holding after the process it held for was gone")
	}
	if len(eng.containers) != 0 || len(eng.networks) != 0 {
		t.Fatalf("containers %v networks %v left, want none", eng.containers, eng.networks)
	}
	if !strings.Contains(out.String(), "process 77") {
		t.Fatalf("the output does not say why the lab stopped:\n%s", out.String())
	}
}

// A throwaway lab of any project whose owner on this host is gone (both the
// script and the lab killed too hard to clean up) is reaped, data and all,
// by the next run; a live one, one owned elsewhere, and a learner's lab of
// another project are left alone.
func TestReapRemovesAbandonedThrowawayLabsOfAnyProject(t *testing.T) {
	srv := fakeServer(t)
	eng := newFakeEngine(strings.TrimPrefix(srv.URL, "http://"))
	lbl := func(project, host, pid string, ephemeral bool) map[string]string {
		l := map[string]string{LabelLab: project, LabelComposeProject: project, LabelLabHost: host, LabelLabPID: pid}
		if ephemeral {
			l[LabelLabEphemeral] = "true"
		}
		return l
	}
	dead := lbl("ogwalk-9-aa", "box", "9999", true)
	eng.containers["dead-c"] = Resource{ID: "dead-c", Labels: dead}
	eng.networks["ogwalk-9-aa_default"] = Resource{ID: "n1", Name: "ogwalk-9-aa_default", Labels: dead}
	eng.volumes["ogwalk-9-aa_db"] = Resource{Name: "ogwalk-9-aa_db", Labels: dead}
	eng.containers["live-c"] = Resource{ID: "live-c", Labels: lbl("ogwalk-42-bb", "box", "42", true)}
	eng.volumes["ogwalk-42-bb_db"] = Resource{Name: "ogwalk-42-bb_db", Labels: lbl("ogwalk-42-bb", "box", "42", true)}
	eng.containers["far-c"] = Resource{ID: "far-c", Labels: lbl("ogwalk-7-cc", "elsewhere", "7", true)}
	eng.volumes["learner_db"] = Resource{Name: "learner_db", Labels: lbl("learner", "box", "9999", false)}

	var out bytes.Buffer
	if err := testRunner(eng, &out).Reap(context.Background(), "ogwalk-1-new"); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if _, ok := eng.containers["dead-c"]; ok {
		t.Error("an abandoned throwaway lab's container was not reaped")
	}
	if _, ok := eng.networks["ogwalk-9-aa_default"]; ok {
		t.Error("an abandoned throwaway lab's network was not reaped")
	}
	if _, ok := eng.volumes["ogwalk-9-aa_db"]; ok {
		t.Error("an abandoned throwaway lab's volume was not reaped")
	}
	for _, keep := range []string{"live-c", "far-c"} {
		if _, ok := eng.containers[keep]; !ok {
			t.Errorf("the reaper removed %s, which it does not own or whose owner is alive", keep)
		}
	}
	for _, keep := range []string{"ogwalk-42-bb_db", "learner_db"} {
		if _, ok := eng.volumes[keep]; !ok {
			t.Errorf("the reaper removed the volume %s", keep)
		}
	}
}

// The ready file is shell: a value is single-quoted, and a quote inside it
// cannot end the quoting.
func TestFormatReadyEnvQuotesEveryValue(t *testing.T) {
	got := FormatReadyEnv([][2]string{{"A", "plain"}, {"B", "it's"}, {"C", ""}})
	want := "A='plain'\nB='it'\\''s'\nC=''\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A throwaway lab removes its data at teardown, so it never runs as the
// learner's own project, whose data a re-run keeps.
func TestPlanRefusesAThrowawayLabOfTheDefaultProject(t *testing.T) {
	o := testOptions()
	o.Ephemeral = true
	o.Project = ""
	if _, err := Plan(o); err == nil || !strings.Contains(err.Error(), "--project") {
		t.Fatalf("got %v, want a refusal naming --project", err)
	}
	o.Project = DefaultProject
	if _, err := Plan(o); err == nil {
		t.Fatal("a throwaway lab of the default project was planned")
	}
}
