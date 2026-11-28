//go:build linux

package sim

import (
	"bufio"
	"context"
	"crypto/tls"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/simtest"
)

// The VRoom family serves the faces its profile did over real sockets (#2834):
// the https feed carries the changes to the state it streams and then the named
// events a route reports, in one numbered stream, and the control socket pushes
// the change a route makes to a subscribed touch panel and answers a set.
func TestTheVRoomFamilyServesItsFeedAndControlSocket(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket test")
	}
	ip := simtest.ClaimLoopback(t)
	e := shippedEngine(t)
	e.SetPortMap(EphemeralPorts)
	t.Cleanup(e.StopAll)
	if err := e.Sync([]DeviceSnapshot{{DeviceID: "bar-1", Hostname: "bar-1", LoopbackIP: ip, ProfileName: profileOf(t, e, "kestrel-vroom"), Version: 1}}); err != nil {
		t.Fatal(err)
	}
	d := e.Devices()[0]
	d.StopBehaviors() // hold the readings still
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} //nolint:gosec // the sim's self-signed identity
	api := "https://" + faceAddr(t, d, "http-api")

	// The control socket: a subscribed panel is pushed what a route changes.
	c, _, err := dialWS(t, "wss://"+faceAddr(t, d, "ws-api")+"/api/ws", basicHeader("admin", "kestrel"))
	if err != nil {
		t.Fatal(err)
	}
	send(t, c, `{"type":"subscribe","paths":["status.call","status.audio.volume"]}`)
	if m := next(t, c); m["type"] != "subscribed" {
		t.Fatalf("ack = %v", m)
	}

	// The feed: open it, dial, read until the call's log line.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", api+"/api/events", nil)
	req.SetBasicAuth("admin", "kestrel")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("feed content type = %q", ct)
	}
	lines := make(chan string, 64)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()

	dial, _ := http.NewRequest("POST", api+"/api/call/dial", strings.NewReader(`{"id":"c9001","participants":3}`))
	dial.SetBasicAuth("admin", "kestrel")
	dial.Header.Set("Content-Type", "application/json")
	if r, err := client.Do(dial); err != nil || r.StatusCode != 200 {
		t.Fatalf("dial: %v %v", r, err)
	}
	var seen []string
	for len(seen) < 4 {
		ev := nextEvent(t, lines)
		if len(ev) == 0 {
			t.Fatal("the feed closed")
		}
		seen = append(seen, strings.TrimPrefix(ev[1], "event: ")+" "+strings.TrimPrefix(ev[2], "data: "))
	}
	if !strings.HasPrefix(seen[0], "change ") || !strings.HasPrefix(seen[1], "change ") ||
		seen[2] != `call-started {"id":"c9001","participants":3}` || !strings.HasPrefix(seen[3], `log {"severity":"INFO","facility":"kvroom.call"`) {
		t.Fatalf("the feed = %q, want the two state changes, then the call's events", seen)
	}
	// The panel saw the same change as a push.
	if m := next(t, c); m["type"] != "change" || !strings.HasPrefix(m["path"].(string), "status.call") {
		t.Fatalf("push = %v", m)
	}
	send(t, c, `{"type":"set","path":"status.audio.volume","value":61,"id":"s1"}`)
	for {
		m := next(t, c)
		if m["type"] == "result" {
			if m["id"] != "s1" || m["value"] != float64(61) {
				t.Fatalf("set = %v", m)
			}
			break
		}
	}
	if v := d.ReadPath("status.audio.volume"); v != int64(61) && v != 61 {
		t.Fatalf("volume = %v (%T)", v, v)
	}
}
