//go:build labintegration

package lab

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/labcheck"
)

// bearer signs every request a client sends with a lab's token.
type bearer struct {
	token string
	next  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(r)
}

// The corporate lab's conference video bar talks (#2754): on a real daemon,
// `lab run --scenario corporate --systems 20` (the smallest corporate lab
// with a divisible conference room), the hq conference room's video bar
// reaches the showcase floor within the warm-up: 20 metrics, 8 properties,
// an event it caught itself and a log line, over its https API and its
// websocket control socket, once a call is dialed on it (its own schedule
// may put no meeting in the warm-up). The DSP beside it is logged, for the footprint
// reading. It runs under a project of its own and tears down what it made.
func TestIntegrationCorporateVideoBarTalks(t *testing.T) {
	if os.Getenv("OMNIGLASS_LAB_TEST") != "1" {
		t.Skip("the corporate lab runs through make test-lab (it needs the image make builds)")
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
	project := fmt.Sprintf("omniglass-videobar-it-%d", os.Getpid())
	host, _ := os.Hostname()
	t.Cleanup(func() {
		var out strings.Builder
		r := runner(eng, &out, host)
		r.TearDown(Spec{Project: project, Network: project + "_default"})
		vols, _ := eng.Volumes(context.Background(), ProjectLabels(project))
		for _, v := range vols {
			_ = eng.RemoveVolume(context.Background(), v.Name)
		}
	})

	file := t.TempDir() + "/lab.env"
	var out lockedBuffer
	args := []string{"lab", "run", "--scenario", "corporate", "--systems", "20", "--project", project, "--port", "0", "--ready-file", file}
	if image := os.Getenv("OMNIGLASS_LAB_TEST_IMAGE"); image != "" {
		args = append(args, "--image", image)
	}
	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(), "DOCKER_HOST="+daemon.Host)
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(3 * time.Minute):
			_ = cmd.Process.Kill()
		}
	})
	deadline := time.Now().Add(8 * time.Minute)
	for {
		if _, err := os.Stat(file); err == nil {
			break
		}
		select {
		case err := <-exited:
			t.Fatalf("the lab exited before it was ready (%v):\n%s", err, out.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("no ready file:\n%s", out.String())
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
	base := vars["LAB_HOST_URL"]
	client := &http.Client{Timeout: 30 * time.Second, Transport: bearer{token: vars["LAB_TOKEN"], next: http.DefaultTransport}}

	// The room keeps a meeting schedule, which may put no meeting in the
	// warm-up, so the gate dials a call on the bar first, as a demo would:
	// the call starts at once and runs half an hour. A fire can meet the
	// simulator before it serves the bar, so it is retried for a while.
	const system = "hq-divisible-conference-1"
	dial := labcheck.Fire{Surface: "http-api", Method: "POST", Path: "/api/call/dial", Body: `{"id":"c-showcase","participants":4}`}
	for fireBy := time.Now().Add(2 * time.Minute); ; {
		err := labcheck.FireOnShowcase(ctx, client, base, system, "kestrel-vroom", dial)
		if err == nil {
			break
		}
		if time.Now().After(fireBy) {
			t.Fatalf("could not dial a call on %s's video bar: %v", system, err)
		}
		time.Sleep(5 * time.Second)
	}

	// The warm-up: the status poll runs every 30 s and the dialed call's
	// events and log lines arrive at once, so five minutes holds them all.
	warm := time.Now().Add(5 * time.Minute)
	for {
		bar, err := labcheck.ReadShowcase(ctx, client, base, system, "kestrel-vroom")
		short := labcheck.VideoBarFloor.Short(bar)
		if err == nil && len(short) == 0 {
			t.Logf("%s's video bar: %+v", system, bar)
			if dsp, err := labcheck.ReadShowcase(ctx, client, base, system, "polaris-dsp-16"); err == nil {
				t.Logf("%s's DSP: %+v", system, dsp)
			}
			return
		}
		if time.Now().After(warm) {
			t.Fatalf("after the warm-up, %s's video bar falls short (%v): %v", system, err, short)
		}
		time.Sleep(10 * time.Second)
	}
}
