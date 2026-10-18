//go:build linux

package sim

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// The touch panel speaks the newtron-nvp driver's line protocol on 51325,
// as the collector's own line client asks it: GET INPUT reports the video
// input, SET INPUT selects one of the inputs the video-input property
// allows, and the next GET sees it.
func TestPanelSpeaksTheNVPLineProtocol(t *testing.T) {
	e := newCatalogEngine(t)
	ip := "127.88.9.1"
	if err := e.Sync([]DeviceSnapshot{{DeviceID: "panel", Hostname: "panel", LoopbackIP: ip,
		ProfileName: "newtron-panel-7", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	target := net.JoinHostPort(ip, fmt.Sprint(51325)) // not privileged, so not shifted
	lx := collection.NewLineExchanger()
	ask := func(line string) string {
		t.Helper()
		got, err := lx.Exchange(context.Background(), target, line, 2*time.Second)
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		return got
	}
	if got := ask("GET INPUT"); got != "INPUT hdmi1" {
		t.Fatalf("GET INPUT = %q, want the default input", got)
	}
	for _, in := range []string{"hdmi2", "hdmi3", "dp1", "usbc", "hdmi1"} {
		if got := ask("SET INPUT " + in); got != "INPUT "+in {
			t.Fatalf("SET INPUT %s = %q", in, got)
		}
		if got := ask("GET INPUT"); got != "INPUT "+in {
			t.Fatalf("after SET INPUT %s, GET INPUT = %q", in, got)
		}
	}
}

// A subscribed session hears the input change as the driver's listener
// expects it (EVT INPUT <input>, unquoted), and on the session that sent the
// command, the command's reply comes before the push it caused.
func TestPanelPushesInputChangesAfterTheReply(t *testing.T) {
	e := newCatalogEngine(t)
	ip := "127.88.9.2"
	if err := e.Sync([]DeviceSnapshot{{DeviceID: "panel", Hostname: "panel", LoopbackIP: ip,
		ProfileName: "newtron-panel-7", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	target := net.JoinHostPort(ip, "51325")
	dial := func() (net.Conn, *bufio.Reader) {
		t.Helper()
		c, err := net.DialTimeout("tcp", target, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		return c, bufio.NewReader(c)
	}
	send := func(c net.Conn, line string) {
		t.Helper()
		if _, err := fmt.Fprintf(c, "%s\r\n", line); err != nil {
			t.Fatal(err)
		}
	}
	read := func(r *bufio.Reader) string {
		t.Helper()
		l, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		return strings.TrimRight(l, "\r\n")
	}

	c, r := dial()
	send(c, "SUBSCRIBE INPUT")
	if got := read(r); got != "OK" {
		t.Fatalf("SUBSCRIBE INPUT = %q", got)
	}
	send(c, "SET INPUT hdmi2")
	if got := read(r); got != "INPUT hdmi2" {
		t.Fatalf("first line after SET = %q, want the command's reply before any push", got)
	}
	if got := read(r); got != "EVT INPUT hdmi2" {
		t.Fatalf("push = %q, want EVT INPUT hdmi2", got)
	}

	// A change made on another session reaches the subscriber too.
	other, otherR := dial()
	send(other, "SET INPUT dp1")
	read(otherR)
	if got := read(r); got != "EVT INPUT dp1" {
		t.Fatalf("push from another session = %q", got)
	}
}
