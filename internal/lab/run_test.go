package lab

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeEngine is an in-memory Docker: what the runner created, removed and
// ran. It is a checkpoint for the runner's logic (order, teardown, reap); the
// integration test against a real daemon closes it.
type fakeEngine struct {
	mu         sync.Mutex
	containers map[string]Resource
	states     map[string]ContainerState
	networks   map[string]Resource
	volumes    map[string]Resource
	images     map[string]map[string]string
	created    []string
	seedExit   int
	seedLog    string
	published  string
	nextID     int
}

func newFakeEngine(published string) *fakeEngine {
	return &fakeEngine{
		containers: map[string]Resource{}, states: map[string]ContainerState{},
		networks: map[string]Resource{}, volumes: map[string]Resource{},
		images: map[string]map[string]string{
			DevImage:      {VersionLabel: "abc123"},
			PostgresImage: {},
		},
		seedLog:   FormatSeedResult(SeedResult{Node: true, Summary: "the dev fleet, seeded on this lab's first run"}),
		published: published,
	}
}

func matches(l, want map[string]string) bool {
	for k, v := range want {
		if l[k] != v {
			return false
		}
	}
	return true
}

func (f *fakeEngine) Ping(context.Context) (EngineInfo, error) {
	return EngineInfo{APIVersion: "1.56"}, nil
}
func (f *fakeEngine) ImageLabels(_ context.Context, ref string) (map[string]string, bool, error) {
	l, ok := f.images[ref]
	return l, ok, nil
}
func (f *fakeEngine) PullImage(_ context.Context, ref string) error {
	f.images[ref] = map[string]string{}
	return nil
}
func (f *fakeEngine) CreateNetwork(_ context.Context, name string, labels map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.networks[name] = Resource{ID: name, Name: name, Labels: labels}
	f.created = append(f.created, "network "+name)
	return nil
}
func (f *fakeEngine) RemoveNetwork(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.networks, name)
	return nil
}
func (f *fakeEngine) list(m map[string]Resource, labels map[string]string) []Resource {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Resource
	for _, r := range m {
		if matches(r.Labels, labels) {
			out = append(out, r)
		}
	}
	return out
}
func (f *fakeEngine) Networks(_ context.Context, labels map[string]string) ([]Resource, error) {
	return f.list(f.networks, labels), nil
}
func (f *fakeEngine) CreateVolume(_ context.Context, name string, labels map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.volumes[name]; !ok {
		f.volumes[name] = Resource{ID: name, Name: name, Labels: labels}
	}
	return nil
}
func (f *fakeEngine) RemoveVolume(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.volumes, name)
	return nil
}
func (f *fakeEngine) Volumes(_ context.Context, labels map[string]string) ([]Resource, error) {
	return f.list(f.volumes, labels), nil
}
func (f *fakeEngine) RunContainer(_ context.Context, svc Service, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := fmt.Sprintf("c%d", f.nextID)
	f.containers[id] = Resource{ID: id, Name: svc.Container, Labels: svc.Labels}
	st := ContainerState{Running: true, Status: "running", Published: map[int]string{}}
	switch svc.Name {
	case "db":
		st.Health = "healthy"
	case "seed":
		st = ContainerState{Status: "exited", ExitCode: f.seedExit}
	case "server":
		st.Published[consolePort] = f.published
	}
	f.states[id] = st
	f.created = append(f.created, "container "+svc.Name)
	return id, nil
}
func (f *fakeEngine) Inspect(_ context.Context, id string) (ContainerState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.states[id]
	if !ok {
		return ContainerState{}, errors.New("no such container")
	}
	return st, nil
}
func (f *fakeEngine) Wait(_ context.Context, id string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.states[id].ExitCode, nil
}
func (f *fakeEngine) Logs(_ context.Context, id string, _ int) (string, error) {
	if f.containers[id].Labels[LabelComposeService] == "seed" {
		return f.seedLog, nil
	}
	return "", nil
}
func (f *fakeEngine) StopAndRemove(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.containers, id)
	delete(f.states, id)
	return nil
}
func (f *fakeEngine) Containers(_ context.Context, labels map[string]string) ([]Resource, error) {
	return f.list(f.containers, labels), nil
}

// fakeServer is the lab's API as readiness reads it: healthy, the login
// works, the simulator serves one component, and its endpoints read up but
// the planted one.
func fakeServer(t *testing.T) *httptest.Server {
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
		fmt.Fprint(w, `{"endpoints":[{"label":"","verdict":{"value":"up"}},{"label":"Panel SNMP","verdict":{"value":"down"}},{"label":"Hooks","webhook":{"path":"/x"},"verdict":null}]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func testRunner(eng Engine, out *bytes.Buffer) *Runner {
	return &Runner{
		Engine: eng, Out: out, Host: "box",
		Alive:    func(pid int) bool { return pid == 42 },
		PortFree: func(string, int) error { return nil },
		HTTP:     &http.Client{Timeout: 2 * time.Second},
		Poll:     10 * time.Millisecond, ReadyTimeout: 2 * time.Second,
	}
}

func testConfig() Config {
	o := testOptions()
	o.Owner = Owner{Host: "box", PID: 42}
	return Config{Options: o, Image: SelectImage("abc123", ""), PlantedFaults: []string{"Panel SNMP"}}
}

func TestRunBringsTheLabUpAndReportsTheConsole(t *testing.T) {
	srv := fakeServer(t)
	eng := newFakeEngine(strings.TrimPrefix(srv.URL, "http://"))
	var out bytes.Buffer
	lab, err := testRunner(eng, &out).Run(context.Background(), testConfig())
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if lab.BaseURL != srv.URL || !strings.HasSuffix(lab.ConsoleURL, "/web") {
		t.Fatalf("base %q console %q", lab.BaseURL, lab.ConsoleURL)
	}
	want := []string{"network omniglass-lab_default", "container db", "container seed", "container server", "container node"}
	if strings.Join(eng.created, ",") != strings.Join(want, ",") {
		t.Fatalf("created %v, want %v", eng.created, want)
	}
	if len(eng.containers) != 3 {
		t.Fatalf("%d containers left running, want db, server and node (the seed is removed once it is done)", len(eng.containers))
	}
	for _, line := range []string{"1 of 1 simulated endpoints up", "1 broken on purpose", "the login dev / dev works"} {
		if !strings.Contains(out.String(), line) {
			t.Errorf("status lacks %q:\n%s", line, out.String())
		}
	}
}

// Holding, the lab stays up until its context ends; then every container
// and the network go, and the volumes stay.
func TestRunHoldsUntilStoppedThenTearsDownKeepingTheData(t *testing.T) {
	srv := fakeServer(t)
	eng := newFakeEngine(strings.TrimPrefix(srv.URL, "http://"))
	var out bytes.Buffer
	cfg := testConfig()
	cfg.Hold = true
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := testRunner(eng, &out).Run(ctx, cfg); done <- err }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		eng.mu.Lock()
		n := len(eng.containers)
		eng.mu.Unlock()
		if n == 3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("a stopped lab: %v", err)
	}
	if len(eng.containers) != 0 || len(eng.networks) != 0 {
		t.Fatalf("after Ctrl-C: containers %v networks %v, want none", eng.containers, eng.networks)
	}
	if len(eng.volumes) != 2 {
		t.Fatalf("after Ctrl-C: volumes %v, want both kept", eng.volumes)
	}
	for _, line := range []string{"Your omniglass lab is ready", "Console", "user dev, password dev", "Ctrl-C", "walkthroughs", "Stopped."} {
		if !strings.Contains(out.String(), line) {
			t.Errorf("output lacks %q:\n%s", line, out.String())
		}
	}
}

// A seed that fails takes the lab down, saying why, with nothing left
// behind but the volumes.
func TestRunTearsDownWhenTheSeedFails(t *testing.T) {
	srv := fakeServer(t)
	eng := newFakeEngine(strings.TrimPrefix(srv.URL, "http://"))
	eng.seedExit, eng.seedLog = 1, "this database holds data and has no lab mark"
	var out bytes.Buffer
	_, err := testRunner(eng, &out).Run(context.Background(), testConfig())
	if err == nil || !strings.Contains(err.Error(), "no lab mark") {
		t.Fatalf("got %v, want the seed's refusal", err)
	}
	if len(eng.containers) != 0 || len(eng.networks) != 0 {
		t.Fatalf("after a failed seed: containers %v networks %v, want none", eng.containers, eng.networks)
	}
}

// Every preflight failure comes before anything is created.
func TestPreflightFailuresCreateNothing(t *testing.T) {
	srv := fakeServer(t)
	for name, tc := range map[string]struct {
		setup func(*fakeEngine, *Runner, *Config)
		want  string
	}{
		"port taken": {func(_ *fakeEngine, r *Runner, _ *Config) {
			r.PortFree = func(string, int) error { return &PreflightError{Cause: "port 8080 is taken"} }
		}, "port 8080"},
		"image missing": {func(e *fakeEngine, _ *Runner, _ *Config) { delete(e.images, DevImage) }, "run `make`"},
		"image from another tree": {func(e *fakeEngine, _ *Runner, _ *Config) {
			e.images[DevImage] = map[string]string{VersionLabel: "0ther"}
		}, "another tree"},
		"a live lab already running": {func(e *fakeEngine, _ *Runner, c *Config) {
			e.containers["old"] = Resource{ID: "old", Labels: map[string]string{
				LabelLab: DefaultProject, LabelComposeProject: DefaultProject, LabelLabHost: "box", LabelLabPID: "42"}}
		}, "already running"},
		"a lab from another machine": {func(e *fakeEngine, _ *Runner, c *Config) {
			e.networks["old"] = Resource{ID: "old", Name: "old", Labels: map[string]string{
				LabelLab: DefaultProject, LabelComposeProject: DefaultProject, LabelLabHost: "elsewhere", LabelLabPID: "7"}}
		}, "another machine"},
	} {
		t.Run(name, func(t *testing.T) {
			eng := newFakeEngine(strings.TrimPrefix(srv.URL, "http://"))
			var out bytes.Buffer
			r := testRunner(eng, &out)
			cfg := testConfig()
			tc.setup(eng, r, &cfg)
			_, err := r.Run(context.Background(), cfg)
			var pe *PreflightError
			if !errors.As(err, &pe) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want a preflight refusal naming %q", err, tc.want)
			}
			if len(eng.created) != 0 {
				t.Fatalf("created %v before refusing, want nothing", eng.created)
			}
		})
	}
}

// A lab killed too hard to tear down (its owner gone) is reaped by the next
// run, which then comes up.
func TestRunReapsWhatAKilledLabLeft(t *testing.T) {
	srv := fakeServer(t)
	eng := newFakeEngine(strings.TrimPrefix(srv.URL, "http://"))
	dead := map[string]string{LabelLab: DefaultProject, LabelComposeProject: DefaultProject, LabelLabHost: "box", LabelLabPID: "9999"}
	eng.containers["old-db"] = Resource{ID: "old-db", Labels: dead}
	eng.networks["omniglass-lab_default"] = Resource{ID: "n", Name: "omniglass-lab_default", Labels: dead}
	var out bytes.Buffer
	if _, err := testRunner(eng, &out).Run(context.Background(), testConfig()); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if _, ok := eng.containers["old-db"]; ok {
		t.Fatal("the killed lab's container was not reaped")
	}
	if !strings.Contains(out.String(), "cleanup") {
		t.Fatalf("the reap was not reported:\n%s", out.String())
	}
}

func TestFreshRemovesTheVolumesFirst(t *testing.T) {
	srv := fakeServer(t)
	eng := newFakeEngine(strings.TrimPrefix(srv.URL, "http://"))
	old := map[string]string{LabelLab: DefaultProject, LabelComposeProject: DefaultProject, "marker": "old"}
	eng.volumes["omniglass-lab_db"] = Resource{Name: "omniglass-lab_db", Labels: old}
	cfg := testConfig()
	cfg.Fresh = true
	var out bytes.Buffer
	if _, err := testRunner(eng, &out).Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if eng.volumes["omniglass-lab_db"].Labels["marker"] == "old" {
		t.Fatal("--fresh kept the old database volume")
	}
}

// A node that never gets every endpoint up still yields a lab, saying what
// is pending, rather than taking it down.
func TestRunSaysWhatIsPendingWhenCollectionIsSlow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/healthz", func(w http.ResponseWriter, _ *http.Request) {})
	mux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /api/v1/nodes/edge-hq/simulation", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"connected":true,"devices":[{"component_id":"c-1","status":"running"}]}`)
	})
	mux.HandleFunc("GET /api/v1/components/c-1/reachability", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"endpoints":[{"verdict":{"value":"up"}},{"verdict":{"value":"down"}}]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	eng := newFakeEngine(strings.TrimPrefix(srv.URL, "http://"))
	var out bytes.Buffer
	r := testRunner(eng, &out)
	r.ReadyTimeout = 200 * time.Millisecond
	lab, err := r.Run(context.Background(), testConfig())
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !strings.Contains(lab.Pending, "1 of 2") {
		t.Fatalf("pending %q, want it to say 1 of 2 endpoints are not up", lab.Pending)
	}
}

// --image naming this binary's own build (its version label is the binary's
// build id, as the walkthrough stack's image is for the binary inside it) is
// no override worth a warning; any other image is warned about.
func TestImageOverrideWarnsOnlyForAnotherBuild(t *testing.T) {
	srv := fakeServer(t)
	for name, tc := range map[string]struct {
		label string
		warns bool
	}{
		"its own build": {"abc123", false},
		"another build": {"0ther", true},
	} {
		t.Run(name, func(t *testing.T) {
			eng := newFakeEngine(strings.TrimPrefix(srv.URL, "http://"))
			eng.images["stack:x"] = map[string]string{VersionLabel: tc.label}
			var out bytes.Buffer
			cfg := testConfig()
			cfg.Image = SelectImage("abc123", "stack:x")
			cfg.Options.Image = "stack:x"
			if _, err := testRunner(eng, &out).Run(context.Background(), cfg); err != nil {
				t.Fatalf("%v\n%s", err, out.String())
			}
			if got := strings.Contains(out.String(), "warning"); got != tc.warns {
				t.Fatalf("warned %v, want %v:\n%s", got, tc.warns, out.String())
			}
		})
	}
}
