//go:build labintegration

package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/internal/labcheck"
)

// startCorporateLab runs `lab run --scenario corporate --systems N` on a real
// daemon under a project of its own, waits for its ready file and returns the
// console's base URL and a client signed in with the lab's token. It tears
// down what it made.
func startCorporateLab(t *testing.T, name string, systems int) (string, *http.Client) {
	t.Helper()
	if os.Getenv("OMNIGLASS_LAB_TEST") != "1" {
		t.Skip("the corporate lab runs through make test-lab (it needs the image make builds)")
	}
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
	project := fmt.Sprintf("omniglass-%s-it-%d", name, os.Getpid())
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
	args := []string{"lab", "run", "--scenario", "corporate", "--systems", fmt.Sprint(systems), "--project", project, "--port", "0", "--ready-file", file}
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
	return vars["LAB_HOST_URL"], &http.Client{Timeout: 30 * time.Second, Transport: bearer{token: vars["LAB_TOKEN"], next: http.DefaultTransport}}
}

// The simulator holds the world it simulates (#2773): on a corporate lab the
// hq conference room's video bar, a real device on a real daemon, reads its
// room's meeting off the lab calendar (the next meeting it reports is one the
// calendar the world implies holds for that room), and the {room} token in its
// profile expands to the room the plan put its system in. Neither comes from
// the server, whose device list names no room.
func TestIntegrationCorporateWorldReachesDevices(t *testing.T) {
	ctx := context.Background()
	const systems = 20
	const system = "hq-divisible-conference-1"
	base, client := startCorporateLab(t, "world", systems)

	// The expectation is the plan's own: the same scenario resolved at the
	// same size, the world of the system's site, and the calendar it implies.
	m, err := scenario.Shipped("corporate")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := scenario.ShippedCatalog()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := scenario.Resolve(m, cat, systems)
	if err != nil {
		t.Fatal(err)
	}
	w, err := scenario.SiteWorld(m, cat, plan, "hq")
	if err != nil {
		t.Fatal(err)
	}
	at, ok := w.PlaceSystem(system)
	if !ok {
		t.Fatalf("the plan's world has no system %s", system)
	}
	cal := w.Calendar()
	if cal == nil {
		t.Fatal("the corporate world holds no calendar")
	}

	type roomDoc struct {
		Organizer string `json:"meeting_organizer"`
		NextStart int64  `json:"next_start"`
		Key       string `json:"key"`
	}
	var got roomDoc
	var lo, hi time.Time
	deadline := time.Now().Add(5 * time.Minute)
	for {
		lo = time.Now().Add(-time.Minute) // the bar reads on its own tick
		body, err := labcheck.FireOnShowcaseBody(ctx, client, base, system, "kestrel-vroom", labcheck.Fire{Surface: "http-api", Method: "GET", Path: "/api/room"})
		hi = time.Now()
		if err == nil {
			if jerr := json.Unmarshal([]byte(body), &got); jerr != nil {
				t.Fatalf("GET /api/room answered %q: %v", body, jerr)
			}
			if got.Key != "" {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("after five minutes %s's video bar reads no room (%v): %+v", system, err, got)
		}
		time.Sleep(10 * time.Second)
	}
	if want := at.CalRoom().Key(); got.Key != want {
		t.Fatalf("the bar reads room %q, the plan put %s in %q", got.Key, system, want)
	}
	first, ok1 := cal.Next(at.CalRoom(), lo)
	last, ok2 := cal.Next(at.CalRoom(), hi)
	if !ok1 || !ok2 {
		t.Fatalf("the calendar holds no meeting after %s", lo)
	}
	if got.NextStart < first.Start.Unix() || got.NextStart > last.Start.Unix() {
		t.Fatalf("the bar's next meeting starts at %d; the room's calendar has it between %d and %d", got.NextStart, first.Start.Unix(), last.Start.Unix())
	}
	if now, ok := cal.Now(at.CalRoom(), hi); ok && got.Organizer != "" && got.Organizer != now.Meeting.Organizer {
		// A meeting may have started or ended since the bar's tick: only a
		// different, current organizer is a fault.
		prev, pok := cal.Now(at.CalRoom(), lo)
		if !pok || prev.Meeting.Organizer != got.Organizer {
			t.Fatalf("the bar says %q has the room, the calendar says %q", got.Organizer, now.Meeting.Organizer)
		}
	}

	// The profile's {room} token expands to the real room, never empty.
	app, err := labcheck.FireOnShowcaseBody(ctx, client, base, system, "kestrel-vroom", labcheck.Fire{Surface: "http-api", Method: "GET", Path: "/api/app"})
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("room-%s-%s-%s@", at.Site, at.Building, at.Room)
	if !strings.Contains(app, want) {
		t.Fatalf("the bar's sign-in is not for its room (want %q in): %s", want, app)
	}
	t.Logf("%s: room %s, next meeting at %d, sign-in %s", system, got.Key, got.NextStart, want)
}
