//go:build labintegration

package lab

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// A kept scale lab (#1633) on a real daemon, driven as its operator drives
// it: `lab run --scenario corporate --systems 3 --keep`, stopped with
// SIGTERM (as a service manager stops it), keeps its two volumes; run again
// the same way, it re-attaches them, keeps the fleet without reseeding (a
// component added on the first run is still there, and the seed says it
// kept the fleet), and its site nodes come up enrolled with the new run's
// tokens. It runs under a project of its own, so it never meets a person's
// scale lab on the same daemon. OMNIGLASS_LAB_TEST_BINARY and
// OMNIGLASS_LAB_TEST_IMAGE run another build than make's.
func TestIntegrationKeptScaleLabSurvivesAStop(t *testing.T) {
	if os.Getenv("OMNIGLASS_LAB_TEST") != "1" {
		t.Skip("the kept scale lab runs through make test-lab (it needs the image make builds)")
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
	binary := envOr("OMNIGLASS_LAB_TEST_BINARY", "../../bin/omniglass")
	image := envOr("OMNIGLASS_LAB_TEST_IMAGE", DevImage)
	project := fmt.Sprintf("omniglass-scale-it-%d", os.Getpid())
	host, _ := os.Hostname()
	t.Cleanup(func() {
		r := runner(eng, &bytes.Buffer{}, host)
		r.TearDown(Spec{Project: project, Network: project + "_default"})
		vols, _ := eng.Volumes(context.Background(), ProjectLabels(project))
		for _, v := range vols {
			_ = eng.RemoveVolume(context.Background(), v.Name)
		}
	})

	// run starts the kept lab, waits for its ready file, and returns how to
	// reach it and how to stop it (SIGTERM, then its output once it exits).
	run := func(name string) (base, token string, stop func() string) {
		t.Helper()
		file := t.TempDir() + "/lab.env"
		var out lockedBuffer
		cmd := exec.Command(binary, "lab", "run", "--scenario", "corporate", "--systems", "3", "--keep",
			"--project", project, "--port", "0", "--image", image, "--ready-file", file)
		cmd.Env = append(os.Environ(), "DOCKER_HOST="+daemon.Host)
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
		stop = func() string {
			t.Helper()
			_ = cmd.Process.Signal(syscall.SIGTERM)
			select {
			case err := <-exited:
				if err != nil {
					t.Fatalf("%s: the stopped lab exited %v:\n%s", name, err, out.String())
				}
			case <-time.After(3 * time.Minute):
				_ = cmd.Process.Kill()
				t.Fatalf("%s: the lab did not stop on SIGTERM:\n%s", name, out.String())
			}
			return out.String()
		}
		deadline := time.Now().Add(6 * time.Minute)
		for {
			if _, err := os.Stat(file); err == nil {
				break
			}
			select {
			case err := <-exited:
				t.Fatalf("%s: the lab exited before it was ready (%v):\n%s", name, err, out.String())
			default:
			}
			if time.Now().After(deadline) {
				stop()
				t.Fatalf("%s: no ready file:\n%s", name, out.String())
			}
			time.Sleep(time.Second)
		}
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		vars := map[string]string{}
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			k, v, _ := strings.Cut(line, "=")
			vars[k] = strings.Trim(v, "'")
		}
		return vars["LAB_HOST_URL"], vars["LAB_TOKEN"], stop
	}
	call := func(method, url, token, body string) int {
		t.Helper()
		req, _ := http.NewRequest(method, url, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	base, token, stop := run("the first run")
	if code := call(http.MethodPost, base+"/api/v1/components", token, `{"name":"scale-it-added","product":"generic-device"}`); code/100 != 2 {
		t.Fatalf("add a component on the first run: %d", code)
	}
	first := stop()
	restart := "omniglass lab run --scenario corporate --systems 3 --project " + project + " --keep"
	if !strings.Contains(first, "so `"+restart+"` picks up where you left off") {
		t.Errorf("the first run's stop line does not name its restart:\n%s", first)
	}
	if vs, _ := eng.Volumes(ctx, ProjectLabels(project)); len(vs) != 2 {
		t.Fatalf("after a kept scale lab's stop: %d volumes, want both kept\n%s", len(vs), first)
	}

	base, token, stop = run("the second run")
	if code := call(http.MethodGet, base+"/api/v1/components/scale-it-added", token, ""); code != http.StatusOK {
		t.Errorf("the component added on the first run, after a restart: %d, want it kept", code)
	}
	second := stop()
	if !strings.Contains(second, "kept this scale lab's fleet") {
		t.Errorf("the second run's seed did not keep the fleet:\n%s", second)
	}
	if !strings.Contains(second, "sites up") {
		t.Errorf("the second run's site nodes did not come up:\n%s", second)
	}
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// lockedBuffer is a buffer a process's output and the test share.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}
