//go:build linux

package sim

import (
	"context"
	"fmt"
	"net"
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
