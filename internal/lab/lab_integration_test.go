//go:build labintegration

// The lab against a real Docker daemon: the integration test that closes the
// runner's capability gate (the fake-engine tests are its checkpoint). It
// needs the image `make` builds (omniglass:dev) and runs through make test-lab.
// Everything it creates carries a project name of its own, so it never meets
// a person's lab on the same daemon, and it removes only that project.
package lab

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
	"strings"
	"testing"
	"time"
)

func TestIntegrationLabOnARealDaemon(t *testing.T) {
	if os.Getenv("OMNIGLASS_LAB_TEST") != "1" {
		t.Skip("the lab's integration test runs through make test-lab (it needs the image make builds)")
	}
	ctx := context.Background()
	home, _ := os.UserHomeDir()
	daemon, err := ResolveDaemon(Env{Getenv: os.Getenv, ReadFile: os.ReadFile, Home: home, GOOS: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	eng, err := NewDockerEngine(daemon.Host)
	if err != nil {
		t.Fatal(err)
	}
	labels, found, err := eng.ImageLabels(ctx, DevImage)
	if err != nil || !found {
		t.Fatalf("the image %s (run make first): found %v, %v", DevImage, found, err)
	}
	host, _ := os.Hostname()
	project := fmt.Sprintf("omniglass-lab-it-%d", os.Getpid())
	t.Cleanup(func() {
		var out bytes.Buffer
		r := runner(eng, &out, host)
		r.TearDown(Spec{Project: project, Network: project + "_default"})
		vols, _ := eng.Volumes(context.Background(), ProjectLabels(project))
		for _, v := range vols {
			_ = eng.RemoveVolume(context.Background(), v.Name)
		}
	})
	cfg := func(pid int) Config {
		return Config{
			Options: Options{Project: project, Listen: DefaultListen, Port: 0, Image: DevImage, Scenario: DefaultScenario,
				NodeToken: "lab-it-node-token", NodeTokenHash: tokenHash("lab-it-node-token"),
				PushToken: "lab-it-push-token", PushTokenHash: tokenHash("lab-it-push-token"), PushTokenPrefix: "labitpsh",
				Owner: Owner{Host: host, PID: pid, PIDNS: CurrentPIDNS()}},
			Image:         SelectImage(labels[VersionLabel], ""),
			PlantedFaults: []string{"Panel SNMP", "Panel SSH", "Panel console"},
		}
	}
	up := func(c Config) (*Lab, string) {
		t.Helper()
		var out bytes.Buffer
		start := time.Now()
		l, err := runner(eng, &out, host).Run(ctx, c)
		if err != nil {
			t.Fatalf("the lab did not come up: %v\n%s", err, out.String())
		}
		t.Logf("lab up in %s:\n%s", time.Since(start).Round(time.Second), out.String())
		if l.Pending != "" {
			t.Fatalf("the lab came up with work pending: %s", l.Pending)
		}
		return l, out.String()
	}

	lab, _ := up(cfg(os.Getpid()))
	client, err := runner(eng, io.Discard, host).signIn(ctx, lab.BaseURL)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("compose sees one project of db, server and node", func(t *testing.T) {
		if _, err := exec.LookPath("docker"); err != nil {
			t.Skip("no docker CLI to ask compose")
		}
		out, err := exec.Command("docker", "compose", "-p", project, "ps", "--format", "{{.Service}}").CombinedOutput()
		if err != nil {
			t.Fatalf("docker compose ps: %v\n%s", err, out)
		}
		got := strings.Fields(string(out))
		for _, want := range []string{"db", "server", "node"} {
			if !strings.Contains(" "+strings.Join(got, " ")+" ", " "+want+" ") {
				t.Errorf("docker compose -p %s ps lists %v, want %s among them", project, got, want)
			}
		}
		ls, _ := exec.Command("docker", "compose", "ls").CombinedOutput()
		if !strings.Contains(string(ls), project) {
			t.Errorf("docker compose ls does not list %s:\n%s", project, ls)
		}
		logs, _ := exec.Command("docker", "compose", "-p", project, "logs", "server").CombinedOutput()
		if !strings.Contains(string(logs), "http api listening") {
			t.Errorf("docker compose logs server lacks the server's log:\n%s", logs)
		}
	})

	t.Run("one published port, on loopback, and NET_ADMIN on the node alone", func(t *testing.T) {
		published := 0
		for _, svc := range []string{"db", "server", "node"} {
			st, err := eng.Inspect(ctx, lab.ID(svc))
			if err != nil {
				t.Fatal(err)
			}
			for port, addr := range st.Published {
				published++
				if svc != "server" || port != consolePort || !strings.HasPrefix(addr, "127.0.0.1:") {
					t.Errorf("%s publishes %d on %s, want only the server's console on 127.0.0.1", svc, port, addr)
				}
			}
			hasNetAdmin := strings.Contains(strings.Join(st.CapAdd, ","), "NET_ADMIN")
			if hasNetAdmin != (svc == "node") {
				t.Errorf("%s caps %v: NET_ADMIN belongs to the node alone", svc, st.CapAdd)
			}
		}
		if published != 1 {
			t.Errorf("%d published ports across the lab, want exactly 1", published)
		}
	})

	t.Run("the server says it is a lab", func(t *testing.T) {
		var st struct {
			Lab bool `json:"lab"`
		}
		if err := getJSON(ctx, client, lab.BaseURL+"/api/v1/auth/status", &st); err != nil || !st.Lab {
			t.Fatalf("auth/status lab = %v (%v), want true", st.Lab, err)
		}
	})

	// Every built collection method runs healthy (#1370): after the
	// warm-up (a simulated service's first webhook post can take two
	// minutes), each driver and transport is up on a component that is no
	// planted fault, a driver with values of its own landed, and a system
	// holds the booking service's push.
	t.Run("every collection method runs healthy on both tiers", func(t *testing.T) {
		deadline := time.Now().Add(4 * time.Minute)
		for {
			missing, err := methodsExercised(ctx, client, lab.BaseURL, cfg(os.Getpid()).PlantedFaults)
			if err == nil && len(missing) == 0 {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("after the warm-up, not exercised: %v (%v)", missing, err)
			}
			time.Sleep(10 * time.Second)
		}
	})

	// An impairment set through the API takes a simulated component's
	// reachability down in the lab; an empty one brings it back.
	t.Run("an unplugged component goes down and comes back", func(t *testing.T) {
		var sim struct {
			Devices []struct {
				ComponentID string `json:"component_id"`
				Status      string `json:"status"`
			} `json:"devices"`
		}
		if err := getJSON(ctx, client, lab.BaseURL+"/api/v1/nodes/"+NodeName+"/simulation", &sim); err != nil {
			t.Fatal(err)
		}
		var target string
		for _, d := range sim.Devices {
			if d.Status == "running" && verdicts(t, client, lab.BaseURL, d.ComponentID) == "up" {
				target = d.ComponentID
				break
			}
		}
		if target == "" {
			t.Fatal("no simulated component reads up to unplug")
		}
		put(t, client, lab.BaseURL+"/api/v1/components/"+target+"/impairment", `{"unplugged":true}`)
		waitUntil(t, 60*time.Second, "the unplugged component to read down", func() bool {
			return verdicts(t, client, lab.BaseURL, target) == "down"
		})
		put(t, client, lab.BaseURL+"/api/v1/components/"+target+"/impairment", `{}`)
		waitUntil(t, 60*time.Second, "the replugged component to read up", func() bool {
			return verdicts(t, client, lab.BaseURL, target) == "up"
		})
	})

	// A component the learner adds survives a stop and a re-run.
	post(t, client, lab.BaseURL+"/api/v1/components", `{"name":"lab-it-added","product":"generic-device"}`)

	var out bytes.Buffer
	runner(eng, &out, host).TearDown(lab.Spec)
	if cs, _ := eng.Containers(ctx, ProjectLabels(project)); len(cs) != 0 {
		t.Fatalf("after teardown %d containers remain", len(cs))
	}
	if ns, _ := eng.Networks(ctx, ProjectLabels(project)); len(ns) != 0 {
		t.Fatalf("after teardown %d networks remain", len(ns))
	}
	if vs, _ := eng.Volumes(ctx, ProjectLabels(project)); len(vs) != 2 {
		t.Fatalf("after teardown %d volumes remain, want both kept", len(vs))
	}

	// A lab killed too hard to tear down: its owner is a process that is
	// gone. The next run reaps it and comes up, keeping the estate, and its
	// secrets still decrypt (the SNMP endpoints read up through them).
	gone := exec.Command("true")
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	up(cfg(gone.Process.Pid))
	lab, outText := up(cfg(os.Getpid()))
	if !strings.Contains(outText, "cleanup") {
		t.Fatalf("the second run did not reap the killed one:\n%s", outText)
	}
	client, err = runner(eng, io.Discard, host).signIn(ctx, lab.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if code := status(t, client, lab.BaseURL+"/api/v1/components/lab-it-added"); code != http.StatusOK {
		t.Fatalf("the added component after a re-run: %d, want it kept", code)
	}
	runner(eng, &out, host).TearDown(lab.Spec)

	// --fresh starts from the seed: the added component is gone.
	fresh := cfg(os.Getpid())
	fresh.Fresh = true
	lab, _ = up(fresh)
	client, err = runner(eng, io.Discard, host).signIn(ctx, lab.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if code := status(t, client, lab.BaseURL+"/api/v1/components/lab-it-added"); code != http.StatusNotFound {
		t.Fatalf("the added component after --fresh: %d, want it gone", code)
	}
}

func runner(eng Engine, out io.Writer, host string) *Runner {
	return &Runner{Engine: eng, Out: out, Host: host, Alive: ProcessAlive, PIDNS: CurrentPIDNS(), PortFree: PortFree,
		HTTP: &http.Client{Timeout: 10 * time.Second}, Poll: time.Second, ReadyTimeout: 4 * time.Minute}
}

// verdicts reads a component's endpoints: "up" when every dialed endpoint
// reads up, "down" when every one reads down, else "mixed".
func verdicts(t *testing.T, c *http.Client, base, id string) string {
	t.Helper()
	var r struct {
		Endpoints []struct {
			Verdict *struct {
				Value string `json:"value"`
			} `json:"verdict"`
		} `json:"endpoints"`
	}
	if err := getJSON(context.Background(), c, base+"/api/v1/components/"+id+"/reachability", &r); err != nil {
		return ""
	}
	seen := map[string]int{}
	n := 0
	for _, e := range r.Endpoints {
		if e.Verdict == nil {
			continue
		}
		n++
		seen[e.Verdict.Value]++
	}
	switch {
	case n > 0 && seen["up"] == n:
		return "up"
	case n > 0 && seen["down"] == n:
		return "down"
	}
	return "mixed"
}

func send(t *testing.T, c *http.Client, method, url, body string) int {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 && method != http.MethodGet {
		t.Fatalf("%s %s: %s %s", method, url, resp.Status, b)
	}
	_ = json.Valid(b)
	return resp.StatusCode
}

func put(t *testing.T, c *http.Client, url, body string)  { send(t, c, http.MethodPut, url, body) }
func post(t *testing.T, c *http.Client, url, body string) { send(t, c, http.MethodPost, url, body) }
func status(t *testing.T, c *http.Client, url string) int { return send(t, c, http.MethodGet, url, "") }

func waitUntil(t *testing.T, within time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func tokenHash(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// A throwaway lab held for a process (the demo pipeline's way, #1197): once
// ready it writes the ready file, whose token reaches the API; once the
// process is gone it tears itself down, its volumes with it.
func TestIntegrationThrowawayLabHeldForAProcess(t *testing.T) {
	if os.Getenv("OMNIGLASS_LAB_TEST") != "1" {
		t.Skip("the lab's integration test runs through make test-lab (it needs the image make builds)")
	}
	ctx := context.Background()
	home, _ := os.UserHomeDir()
	daemon, err := ResolveDaemon(Env{Getenv: os.Getenv, ReadFile: os.ReadFile, Home: home, GOOS: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	eng, err := NewDockerEngine(daemon.Host)
	if err != nil {
		t.Fatal(err)
	}
	labels, found, err := eng.ImageLabels(ctx, DevImage)
	if err != nil || !found {
		t.Fatalf("the image %s (run make first): found %v, %v", DevImage, found, err)
	}
	host, _ := os.Hostname()
	project := fmt.Sprintf("omniglass-lab-it-throwaway-%d", os.Getpid())
	t.Cleanup(func() {
		r := runner(eng, io.Discard, host)
		r.TearDown(Spec{Project: project, Network: project + "_default"})
		vols, _ := eng.Volumes(context.Background(), ProjectLabels(project))
		for _, v := range vols {
			_ = eng.RemoveVolume(context.Background(), v.Name)
		}
	})
	holder := exec.Command("sleep", "600")
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holder.Process.Kill(); _ = holder.Wait() })
	file := t.TempDir() + "/lab.env"
	cfg := Config{
		Options: Options{Project: project, Listen: DefaultListen, Port: 0, Image: DevImage, Scenario: "empty",
			NodeToken: "lab-it-node-token", NodeTokenHash: tokenHash("lab-it-node-token"),
			Owner: Owner{Host: host, PID: os.Getpid(), PIDNS: CurrentPIDNS()}, Ephemeral: true},
		Image: SelectImage(labels[VersionLabel], ""), Hold: true, ReadyFile: file, WhilePID: holder.Process.Pid,
	}
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() { _, err := runner(eng, &out, host).Run(ctx, cfg); done <- err }()
	waitUntil(t, 4*time.Minute, "the ready file", func() bool { _, err := os.Stat(file); return err == nil })
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		k, v, _ := strings.Cut(line, "=")
		vars[k] = strings.Trim(v, "'")
	}
	req, _ := http.NewRequest(http.MethodGet, vars["LAB_HOST_URL"]+"/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+vars["LAB_TOKEN"])
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the ready file's token on /auth/me: %s, want 200", resp.Status)
	}
	if vols, _ := eng.Volumes(ctx, ProjectLabels(project)); len(vols) != 2 {
		t.Fatalf("a held throwaway lab has %d volumes, want 2", len(vols))
	}

	_ = holder.Process.Kill()
	_ = holder.Wait()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%v\n%s", err, out.String())
		}
	case <-time.After(2 * time.Minute):
		t.Fatalf("the lab kept holding after its process was gone:\n%s", out.String())
	}
	cs, _ := eng.Containers(ctx, ProjectLabels(project))
	ns, _ := eng.Networks(ctx, ProjectLabels(project))
	vs, _ := eng.Volumes(ctx, ProjectLabels(project))
	if len(cs)+len(ns)+len(vs) != 0 {
		t.Fatalf("after its process went: %d containers, %d networks, %d volumes remain, want none\n%s", len(cs), len(ns), len(vs), out.String())
	}
}
