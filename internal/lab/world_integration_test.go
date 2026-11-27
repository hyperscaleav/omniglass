//go:build labintegration

package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/internal/labcheck"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/calendar/caltest"
)

// startCorporateLab runs `lab run --scenario corporate --systems N` on a real
// daemon under a project of its own, waits for its ready file and returns the
// console's base URL and a client signed in with the lab's token. It tears
// down what it made.
func startCorporateLab(t *testing.T, name string, systems int) (string, *http.Client) {
	t.Helper()
	return startCorporateLabWith(t, name, systems)
}

// startCorporateLabWith is startCorporateLab with more `lab run` flags: the
// simulator's clock offset (#2800), for a test that stands the lab inside a
// chosen meeting.
func startCorporateLabWith(t *testing.T, name string, systems int, extra ...string) (string, *http.Client) {
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
	args = append(args, extra...)
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

// The simulator holds the world it simulates (#2773) and acts out the lab
// calendar's meetings (#2767): on a corporate lab, the video bars of the hq
// rooms, real devices on a real daemon, are in a call exactly when their
// room's calendar says somebody is in it (their call surface, GET /api/call),
// and the meeting platform's room list says the same of each room by the
// system it is bound to (GET /api/rooms), none of it from the server, whose
// device list names no room. The calendar the check holds them to is the
// plan's own: the same scenario resolved at the same size, the world of the
// site, and the calendar it implies. Devices tick on a minute (the bar) and
// fifteen seconds (the platform), so a reading is held to the calendar as it
// stood over the window before it. The {room} token in a bar's profile
// expands to the room the plan put its system in.
//
// The conference rooms' Relay C1 codecs are read the same way as the bars.
func TestIntegrationCorporateWorldReachesDevices(t *testing.T) {
	ctx := context.Background()
	const systems = 20
	base, client := startCorporateLab(t, "world", systems)

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
	cal := w.Calendar()
	if cal == nil {
		t.Fatal("the corporate world holds no calendar")
	}
	type bar struct {
		system, product string
		room            calendar.Room
	}
	var bars []bar
	for _, s := range w.Sites {
		for _, r := range s.Rooms {
			for _, d := range r.Devices {
				if d.Profile == "kestrel-vroom" || d.Profile == "kestrel-relay-c1" {
					bars = append(bars, bar{r.System, d.Profile, calendar.Room{Site: s.Name, Building: r.Building, Room: r.Name}})
				}
			}
		}
	}
	if len(bars) == 0 {
		t.Fatal("the hq world places no video bar")
	}
	var ucc string
	for _, svc := range w.Services {
		if svc.Webhook && svc.Profile == "ucc-platform" {
			ucc = svc.Name
		}
	}
	if ucc == "" {
		t.Fatal("the hq world has no meeting platform")
	}

	// held is what the calendar says of a room over [from, to], sampled every
	// few seconds: whether somebody could have been in it, whether the room
	// could have been empty, and the most who could have come.
	held := func(room calendar.Room, from, to time.Time) (could, couldNot bool, most int) {
		for at := from; !at.After(to); at = at.Add(5 * time.Second) {
			if e, ok := cal.Now(room, at, calendar.MeetingKinds); ok && caltest.M(e).Outcome != calendar.OutcomeNoShow {
				could = true
				if caltest.M(e).Actual > most {
					most = caltest.M(e).Actual
				}
			} else {
				couldNot = true
			}
		}
		return
	}

	// Read every bar's call and every room the platform lists, for a while
	// after the first answer, and hold each reading to the calendar.
	var uccID string
	deadline := time.Now().Add(5 * time.Minute)
	for uccID == "" {
		var comp struct {
			ID string `json:"id"`
		}
		resp, err := client.Get(base + "/api/v1/components/" + ucc)
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				_ = json.NewDecoder(resp.Body).Decode(&comp)
			}
			resp.Body.Close()
		}
		uccID = comp.ID
		if uccID == "" {
			if time.Now().After(deadline) {
				t.Fatalf("no meeting platform component %q (%v)", ucc, err)
			}
			time.Sleep(5 * time.Second)
		}
	}
	var barReads, barCalls, uccReads, uccCalls int
	first := time.Time{}
	end := time.Now().Add(8 * time.Minute)
	for time.Now().Before(end) && (first.IsZero() || time.Since(first) < 3*time.Minute) {
		for _, b := range bars {
			lo := time.Now().Add(-75 * time.Second) // the bar reads on a minute tick
			body, err := labcheck.FireOnShowcaseBody(ctx, client, base, b.system, b.product, labcheck.Fire{Surface: "http-api", Method: "GET", Path: "/api/call"})
			hi := time.Now()
			if err != nil {
				continue // the simulator may not serve it yet
			}
			var call struct {
				Active       bool `json:"active"`
				Participants int  `json:"participants"`
			}
			if jerr := json.Unmarshal([]byte(body), &call); jerr != nil {
				t.Fatalf("GET /api/call on %s answered %q: %v", b.system, body, jerr)
			}
			if first.IsZero() {
				first = hi
			}
			could, couldNot, most := held(b.room, lo, hi)
			barReads++
			switch {
			case call.Active && !could:
				t.Fatalf("%s's bar is in a call (%d) with nobody in the room on the calendar between %s and %s", b.system, call.Participants, lo.Format("15:04:05"), hi.Format("15:04:05"))
			case !call.Active && !couldNot:
				t.Fatalf("%s's bar is idle while the calendar has the room in a meeting from %s to %s", b.system, lo.Format("15:04:05"), hi.Format("15:04:05"))
			case call.Active:
				barCalls++
				if call.Participants < 1 || call.Participants > most {
					t.Fatalf("%s's bar holds %d participants, the meeting had at most %d come", b.system, call.Participants, most)
				}
			case call.Participants != 0:
				t.Fatalf("%s's bar is idle with %d participants", b.system, call.Participants)
			}
		}
		lo := time.Now().Add(-40 * time.Second) // the platform ticks every fifteen seconds
		body, err := fireOnComponent(ctx, client, base, uccID, labcheck.Fire{Surface: "http-api", Method: "GET", Path: "/api/rooms?page_size=300"})
		hi := time.Now()
		if err == nil {
			var doc struct {
				Rooms []struct {
					ID      string `json:"id"`
					Status  string `json:"status"`
					Meeting struct {
						Participants int `json:"participants"`
					} `json:"meeting"`
				} `json:"rooms"`
			}
			if jerr := json.Unmarshal([]byte(body), &doc); jerr != nil {
				t.Fatalf("GET /api/rooms answered %q: %v", body, jerr)
			}
			for _, r := range doc.Rooms {
				at, ok := w.PlaceSystem(r.ID)
				if !ok {
					continue // a room of another site, or one the platform knows alone
				}
				could, couldNot, most := held(at.CalRoom(), lo, hi)
				uccReads++
				switch {
				case r.Status == "in-meeting" && !could:
					t.Fatalf("the platform has %s in a call with nobody in it on the calendar between %s and %s", r.ID, lo.Format("15:04:05"), hi.Format("15:04:05"))
				case r.Status == "available" && !couldNot:
					t.Fatalf("the platform has %s idle while the calendar has it in a meeting from %s to %s", r.ID, lo.Format("15:04:05"), hi.Format("15:04:05"))
				case r.Status == "in-meeting":
					uccCalls++
					if r.Meeting.Participants < 1 || r.Meeting.Participants > most {
						t.Fatalf("the platform has %d in %s's meeting, at most %d came", r.Meeting.Participants, r.ID, most)
					}
				}
			}
		}
		time.Sleep(20 * time.Second)
	}
	if barReads == 0 || uccReads == 0 {
		t.Fatalf("read %d bar calls and %d platform rooms; the simulator never served them", barReads, uccReads)
	}
	t.Logf("%d bar readings (%d in a call) and %d platform room readings (%d in-call) agreed with the calendar; room %s of %d bars", barReads, barCalls, uccReads, uccCalls, bars[0].room.Key(), len(bars))

	// The profile's {room} token expands to the real room, never empty.
	var vb bar
	for _, b := range bars {
		if b.product == "kestrel-vroom" {
			vb = b
			break
		}
	}
	app, err := labcheck.FireOnShowcaseBody(ctx, client, base, vb.system, vb.product, labcheck.Fire{Surface: "http-api", Method: "GET", Path: "/api/app"})
	if err != nil {
		t.Fatal(err)
	}
	at, _ := w.PlaceSystem(vb.system)
	want := fmt.Sprintf("room-%s-%s-%s@", at.Site, at.Building, at.Room)
	if !strings.Contains(app, want) {
		t.Fatalf("the bar's sign-in is not for its room (want %q in): %s", want, app)
	}
}

// fireOnComponent fires op at the component through its simulation:fire API
// and returns what the device answered: the labcheck helper reaches a system's
// member, and the meeting platform is no system's.
func fireOnComponent(ctx context.Context, c *http.Client, base, id string, op labcheck.Fire) (string, error) {
	body, err := json.Marshal(op)
	if err != nil {
		return "", err
	}
	path := "/components/" + url.PathEscape(id) + "/simulation:fire"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1"+path, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("POST %s: %s", path, resp.Status)
	}
	var res struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
		Error  string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	if res.Error != "" || res.Status/100 != 2 {
		return "", fmt.Errorf("%s %s answered %d %s%s", op.Method, op.Path, res.Status, res.Body, res.Error)
	}
	return res.Body, nil
}
