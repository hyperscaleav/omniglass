package closedloop

import (
	"strings"
	"testing"
	"time"
)

// Unset, the server brackets each minute and assumes a node's defaults: its
// default heartbeat, and a probe on every check-in.
func TestParseSettingsDefaults(t *testing.T) {
	got, err := ParseSettings(RawSettings{}, 30*time.Second)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := Settings{Every: RingEvery, Cadence: NodeCadence{Heartbeat: 30 * time.Second}}
	if got != want {
		t.Fatalf("settings = %+v, want %+v", got, want)
	}
}

// A lab tells its server the bracket it runs at and its nodes' cadence.
func TestParseSettingsSet(t *testing.T) {
	got, err := ParseSettings(RawSettings{Bracket: "2s", Heartbeat: "2s", ProbeEvery: "10s"}, 30*time.Second)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := Settings{Every: 2 * time.Second, Cadence: NodeCadence{Heartbeat: 2 * time.Second, ProbeEvery: 10 * time.Second}}
	if got != want {
		t.Fatalf("settings = %+v, want %+v", got, want)
	}
}

// A bracket outside a second to a minute, or a figure that is not a
// positive duration, fails the boot and names the variable.
func TestParseSettingsRefuses(t *testing.T) {
	for _, x := range []struct {
		raw  RawSettings
		name string
	}{
		{RawSettings{Bracket: "500ms"}, "OMNIGLASS_CLOSED_LOOP_BRACKET"},
		{RawSettings{Bracket: "2m"}, "OMNIGLASS_CLOSED_LOOP_BRACKET"},
		{RawSettings{Bracket: "soon"}, "OMNIGLASS_CLOSED_LOOP_BRACKET"},
		{RawSettings{Heartbeat: "0s"}, "OMNIGLASS_CLOSED_LOOP_HEARTBEAT"},
		{RawSettings{Heartbeat: "x"}, "OMNIGLASS_CLOSED_LOOP_HEARTBEAT"},
		{RawSettings{ProbeEvery: "-1s"}, "OMNIGLASS_CLOSED_LOOP_PROBE_EVERY"},
	} {
		_, err := ParseSettings(x.raw, 30*time.Second)
		if err == nil || !strings.Contains(err.Error(), x.name) {
			t.Errorf("ParseSettings(%+v) = %v, want an error naming %s", x.raw, err, x.name)
		}
	}
}
