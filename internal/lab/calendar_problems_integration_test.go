//go:build labintegration

package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/internal/labcheck"
	"github.com/hyperscaleav/omniglass/sim/calendar"
)

// docker runs the docker CLI against the lab's daemon, for the one thing a
// running lab has no API for: handing its simulator a world of the test's own.
func docker(t *testing.T, args ...string) string {
	t.Helper()
	home, _ := os.UserHomeDir()
	daemon, err := ResolveDaemon(Env{Getenv: os.Getenv, ReadFile: os.ReadFile, Home: home, GOOS: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("docker", args...)
	cmd.Env = append(os.Environ(), "DOCKER_HOST="+daemon.Host)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// A scripted problem breaks real devices on the lab calendar's clock and
// repairs them (#2769): on a corporate lab whose world scripts, for a window
// a few minutes ahead, a dead wireless-microphone battery and an unplugged
// codec in one conference room, the microphone (a real device on a real
// daemon) reads a battery of zero for the window and its own charge again
// after, and the meeting platform's device list reads the codec offline for
// the window and online after, derived from the calendar and not from the
// dark codec. Only the world file differs from a shipped lab's: the test
// writes a world whose problems it times from its own clock, into the
// running lab's data volume, and restarts the headquarters simulator, which
// reads it as it starts, so nothing waits for a real Tuesday.
func TestIntegrationScriptedProblemsBreakAndRepairRealDevices(t *testing.T) {
	ctx := context.Background()
	const systems = 20
	const system = "hq-divisible-conference-1"
	base, client := startCorporateLab(t, "problems", systems)
	project := fmt.Sprintf("omniglass-%s-it-%d", "problems", os.Getpid())

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
	// The window opens four minutes from now on the minute and lasts five:
	// time enough to restart the simulator before it, and to read inside it.
	open := time.Now().UTC().Add(4 * time.Minute).Truncate(time.Minute)
	const dur = 5 * time.Minute
	at := open.Format("15:04")
	scope := func(role string) calendar.Scope { return calendar.Scope{Rooms: []string{system}, Role: role} }
	m.Calendar.Problems = scenario.Problems{
		{ProblemSpec: calendar.ProblemSpec{Name: "test-dead-battery", Cadence: "daily", At: at, For: dur, Scope: scope("wireless-mic"),
			Impairment: calendar.Impairment{Pin: &calendar.Pin{Path: "status.mic.battery_percent", Value: "0"}}}},
		{ProblemSpec: calendar.ProblemSpec{Name: "test-codec-dark", Cadence: "daily", At: at, For: dur, Scope: scope("codec"),
			Impairment: calendar.Impairment{Unplugged: true}}},
	}
	w, err := scenario.SiteWorld(m, cat, plan, "hq")
	if err != nil {
		t.Fatal(err)
	}
	b, err := w.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "edge-hq.json")
	if err := os.WriteFile(file, b, 0o644); err != nil {
		t.Fatal(err)
	}
	docker(t, "cp", file, project+"-server-1:"+WorldFile(scenario.SiteOutpost("hq")))
	docker(t, "restart", project+"-"+scenario.SiteOutpost("hq")+"-1")

	var codecID, ucc string
	for _, s := range w.Sites {
		for _, r := range s.Rooms {
			for _, d := range r.Devices {
				if r.System == system && d.Slot == "codec" {
					codecID = d.ID
				}
			}
		}
	}
	for _, svc := range w.Services {
		if svc.Webhook && svc.Profile == "ucc-platform" {
			ucc = svc.Name
		}
	}
	if codecID == "" || ucc == "" {
		t.Fatalf("the world has no codec in %s (%q) or no meeting platform (%q)", system, codecID, ucc)
	}
	var uccID string
	for deadline := time.Now().Add(5 * time.Minute); uccID == ""; time.Sleep(5 * time.Second) {
		var comp struct {
			ID string `json:"id"`
		}
		if resp, err := client.Get(base + "/api/v1/components/" + ucc); err == nil {
			if resp.StatusCode == 200 {
				_ = json.NewDecoder(resp.Body).Decode(&comp)
			}
			resp.Body.Close()
		}
		if uccID = comp.ID; uccID == "" && time.Now().After(deadline) {
			t.Fatalf("no meeting platform component %q", ucc)
		}
	}

	battery := func() (float64, error) {
		body, err := labcheck.FireOnShowcaseBody(ctx, client, base, system, "lyra-hx-1", labcheck.Fire{Surface: "http-api", Method: "GET", Path: "/api/status"})
		if err != nil {
			return 0, err
		}
		var st struct {
			Mic struct {
				Battery float64 `json:"battery_percent"`
			} `json:"mic"`
		}
		if err := json.Unmarshal([]byte(body), &st); err != nil {
			return 0, fmt.Errorf("answered %q: %w", body, err)
		}
		return st.Mic.Battery, nil
	}
	codecStatus := func() (string, error) {
		body, err := fireOnComponent(ctx, client, base, uccID, labcheck.Fire{Surface: "http-api", Method: "GET", Path: "/api/devices?page_size=300"})
		if err != nil {
			return "", err
		}
		var doc struct {
			Devices []struct{ ID, Status string } `json:"devices"`
		}
		if err := json.Unmarshal([]byte(body), &doc); err != nil {
			return "", fmt.Errorf("answered %q: %w", body, err)
		}
		for _, d := range doc.Devices {
			if d.ID == codecID {
				return d.Status, nil
			}
		}
		return "", fmt.Errorf("the platform lists no device %s", codecID)
	}
	// until polls a reading until it is what the test wants, or a deadline.
	until := func(what string, by time.Time, ok func() (bool, string)) {
		t.Helper()
		last := ""
		for {
			good, got := ok()
			if good {
				t.Logf("%s: %s", what, got)
				return
			}
			last = got
			if time.Now().After(by) {
				t.Fatalf("%s: by %s still %s", what, by.Format("15:04:05"), last)
			}
			time.Sleep(5 * time.Second)
		}
	}

	// Before the window the pack holds a real charge and the codec is online.
	until("before the window", open.Add(-10*time.Second), func() (bool, string) {
		v, err := battery()
		s, err2 := codecStatus()
		return err == nil && err2 == nil && v > 0 && s == "online", fmt.Sprintf("battery %v (%v), codec %q (%v)", v, err, s, err2)
	})
	// Inside it: the battery reads dead and the platform reads the codec offline.
	time.Sleep(time.Until(open.Add(10 * time.Second)))
	until("inside the window", open.Add(dur-30*time.Second), func() (bool, string) {
		v, err := battery()
		s, err2 := codecStatus()
		return err == nil && err2 == nil && v == 0 && s == "offline", fmt.Sprintf("battery %v (%v), codec %q (%v)", v, err, s, err2)
	})
	// After it: the pack's own charge is back and the codec is online again.
	time.Sleep(time.Until(open.Add(dur + 10*time.Second)))
	until("after the window", open.Add(dur+3*time.Minute), func() (bool, string) {
		v, err := battery()
		s, err2 := codecStatus()
		return err == nil && err2 == nil && v > 0 && s == "online", fmt.Sprintf("battery %v (%v), codec %q (%v)", v, err, s, err2)
	})
}
