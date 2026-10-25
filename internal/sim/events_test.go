package sim

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// The server-sent events face (#1038): a simulated component's web server
// streams every change to the state paths it declares, numbers each event,
// replays from a Last-Event-ID (the last event included, as some servers
// do), sits behind the face's auth, and lets the console drop every stream
// as a restart would.

func newEventsDevice(t *testing.T, auth HTTPAuth) (*Device, string) {
	t.Helper()
	dev := newAuthDevice(t, auth)
	dev.SetEvents(profile.Events{Path: "/api/events", RetryMS: 1500, Paths: []string{"power"}, DropPath: "/api/admin/streams/drop"})
	if err := dev.StartHTTP(0, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dev.StopHTTP)
	return dev, dev.HTTPAddrs()[0].String()
}

// openEvents opens the stream, returning its lines as they come.
func openEvents(t *testing.T, addr, lastID string, basic ...string) (*http.Response, <-chan string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+addr+"/api/events", nil)
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	if len(basic) == 2 {
		req.SetBasicAuth(basic[0], basic[1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 64)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	return resp, lines
}

// nextEvent reads lines up to the next blank one, returning the event's
// field lines.
func nextEvent(t *testing.T, lines <-chan string) []string {
	t.Helper()
	var ev []string
	timeout := time.After(5 * time.Second)
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				return nil
			}
			if l == "" {
				if len(ev) > 0 && !strings.HasPrefix(ev[0], "retry:") && !strings.HasPrefix(ev[0], ":") {
					return ev
				}
				ev = nil
				continue
			}
			ev = append(ev, l)
		case <-timeout:
			t.Fatalf("no event within 5s (so far %q)", ev)
		}
	}
}

func post(t *testing.T, addr, path, body string, basic ...string) {
	t.Helper()
	req, _ := http.NewRequest("POST", "http://"+addr+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if len(basic) == 2 {
		req.SetBasicAuth(basic[0], basic[1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

func TestEventsFaceStreamsEveryChangeAndReplays(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket test")
	}
	dev, addr := newEventsDevice(t, HTTPAuth{})
	resp, lines := openEvents(t, addr, "")
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type = %q", ct)
	}
	post(t, addr, "/api/power", `{"power":"standby"}`)
	ev := nextEvent(t, lines)
	if strings.Join(ev, "|") != `id: 1|event: change|data: {"path":"power","value":"standby"}` {
		t.Fatalf("event = %q", ev)
	}
	post(t, addr, "/api/power", `{"power":"on"}`)
	if ev := nextEvent(t, lines); ev[0] != "id: 2" {
		t.Fatalf("second event = %q", ev)
	}
	// The console drops every stream: the reader sees the end.
	if res := dev.Operate(Operation{Kind: "http", Method: "POST", Path: "/api/admin/streams/drop"}); res.Status != 200 || !strings.Contains(res.Body, `"dropped":1`) {
		t.Fatalf("the console's drop = %+v", res)
	}
	for range lines {
	}
	// Changes while nobody listens are kept, and a reconnect from id 2
	// replays 2 (the overlap) and then 3.
	post(t, addr, "/api/power", `{"power":"off"}`)
	_, lines = openEvents(t, addr, "2")
	if ev := nextEvent(t, lines); ev[0] != "id: 2" {
		t.Fatalf("the replay starts at %q, want the last event again", ev)
	}
	if ev := nextEvent(t, lines); ev[0] != "id: 3" || !strings.Contains(ev[2], `"off"`) {
		t.Fatalf("the replayed change = %q", ev)
	}
	if dev.EventStreams() != 2 {
		t.Fatalf("streams opened = %d, want 2", dev.EventStreams())
	}
}

func TestEventsFaceSitsBehindTheAuthGate(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket test")
	}
	dev, addr := newEventsDevice(t, HTTPAuth{Mode: HTTPAuthBasic, User: "admin", Password: "kestrel"})
	resp, _ := openEvents(t, addr, "", "admin", "wrong")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a wrong password = %d, want 401", resp.StatusCode)
	}
	resp, lines := openEvents(t, addr, "", "admin", "kestrel")
	if resp.StatusCode != 200 {
		t.Fatalf("the right password = %d", resp.StatusCode)
	}
	post(t, addr, "/api/power", `{"power":"standby"}`, "admin", "kestrel")
	if ev := nextEvent(t, lines); ev[0] != "id: 1" {
		t.Fatalf("event = %q", ev)
	}
	if dev.EventStreams() != 1 {
		t.Fatalf("streams opened = %d, want only the authorized one", dev.EventStreams())
	}
}

func TestEventsHubFiltersAndBoundsItsReplay(t *testing.T) {
	h := newEventHub(profile.Events{Path: "/e", Paths: []string{"status.audio"}})
	h.fanout(map[string]any{"status.audio.volume": 40, "status.device.uptime": 9, "status.audio.mic_mute": true})
	if len(h.ring) != 2 || h.ring[0].id != 1 || !strings.Contains(h.ring[0].data, "mic_mute") || !strings.Contains(h.ring[1].data, `"value":40`) {
		t.Fatalf("ring = %+v, want the two audio changes, in path order", h.ring)
	}
	for i := range maxEventReplay + 10 {
		h.fanout(map[string]any{"status.audio.volume": i})
	}
	if len(h.ring) != maxEventReplay || h.ring[len(h.ring)-1].id != uint64(maxEventReplay+12) {
		t.Fatalf("ring = %d events ending %d", len(h.ring), h.ring[len(h.ring)-1].id)
	}
	// A Last-Event-ID older than the ring replays all it holds.
	if got := h.replayFrom("1"); len(got) != maxEventReplay {
		t.Fatalf("an old id replays %d", len(got))
	}
	if got := h.replayFrom("garbage"); len(got) != 0 {
		t.Fatalf("an unreadable id replays %d", len(got))
	}
}
