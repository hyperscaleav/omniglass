//go:build linux

package sim

import (
	"testing"
	"time"
)

// The room audio products' wire script (#2835): the requests, SNMP reads,
// shell sessions and behavior walks every golden transcript of the Lyra
// microphones, the Polaris DSP and the Polaris amplifier holds. The recorder
// ran it against the YAML profiles; the parity test runs it against the Go
// families.

const (
	lyraArcProduct = "lyra-arc-a2"
	lyraHXProduct  = "lyra-hx-1"
	polarisDSPProd = "polaris-dsp-16"
	polarisAmpProd = "polaris-amp-4100"
)

// audioFrozen is the note every audio family carries now its profiles are
// gone: recording a family from itself would make the hold on them vacuous.
const audioFrozen = "recorded from the YAML profile before its deletion (#2835); re-recording from the Go family would hold it to itself"

const wireAudioSource = "recorded from the YAML profile before its deletion (#2835); see sim/testdata/wire/README.md"

// ---- the Lyra microphones ----

func lyraScript() []wireStep {
	return []wireStep{
		wireGet("the status document", "/api/status"),
		{Name: "the status document with no key", Method: "GET", Path: "/api/status", NoKey: true},
		{Name: "an unknown path", Method: "GET", Path: "/nope"},
		{Name: "the root", Method: "GET", Path: "/"},
		{Name: "the wrong method on a route", Method: "GET", Path: "/api/mute"},
		{Name: "a head request on the status", Method: "HEAD", Path: "/api/status"},
		{Name: "no mute route with no key", Method: "POST", Path: "/api/mute", Body: `{"mute": true}`, NoKey: true},
		wirePost("mute", "/api/mute", `{"mute": true}`),
		wireGet("the status document muted", "/api/status"),
		wirePost("unmute", "/api/mute", `{"mute": false}`),
		wirePost("mute as a word", "/api/mute", `{"mute": "true"}`),
		wirePost("unmute as a word", "/api/mute", `{"mute":"false"}`),
		wirePost("mute refused", "/api/mute", `{"mute": "loud"}`),
		wirePost("mute a number", "/api/mute", `{"mute": 1}`),
		wirePost("mute with no field", "/api/mute", `{}`),
		wirePost("mute with no body", "/api/mute", ``),
		wirePost("mute with a null field", "/api/mute", `{"mute": null}`),
		{Name: "no reboot on a microphone", Method: "POST", Path: "/api/admin/reboot", Body: `{}`},
		wirePost("a level set by the scenario, then a mute", "/api/mute", `{"mute": true}`),
		{Name: "the status with a scenario's level", Method: "GET", Path: "/api/status", Set: map[string]any{"status.mic.level_db": -31.5}},
		{Name: "the status with a whole level", Method: "GET", Path: "/api/status", Set: map[string]any{"status.mic.level_db": -20}},
		// A body no one can read came back as a field with no value from the
		// profile; the family refuses it as unreadable (ADR-2832).
		unreadable("mute with an array", "/api/mute", `[true]`),
		unreadable("mute with null", "/api/mute", `null`),
		unreadable("mute with a truncated document", "/api/mute", `{"mute": tr`),
		unreadable("mute with a text body", "/api/mute", `mute=true`),
	}
}

// unreadable is a request whose body is not a JSON object: the profile read it
// as a document with every field missing, the family refuses it as unreadable
// and changes nothing.
func unreadable(name, path, body string) wireStep {
	return wireStep{Name: name, Method: "POST", Path: path, Body: body, Snapshot: true,
		Quirk: "the template read a body that is no JSON object as a document with no fields"}
}

func lyraWrites() []wireStep {
	return []wireStep{
		wirePost("mute", "/api/mute", `{"mute": true}`),
		{Name: "a pack drained to a fraction", Method: "GET", Path: "/api/status", Set: map[string]any{"status.mic.battery_percent": 64.5}},
	}
}

var lyraOIDs = []string{
	"1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.3.0",
	"1.3.6.1.4.1.55530.1.1.0", "1.3.6.1.4.1.55530.1.2.0", "1.3.6.1.4.1.55530.2.1.0",
	"1.3.6.1.4.1.55530.5.1.0", "1.3.6.1.4.1.55530.5.2.0", "1.3.6.1.4.1.55530.9.9.0",
}

var lyraWalks = []string{"1.3.6.1.4.1.55530", "1.3.6.1.2.1.1"}

// lyraArcTicks and lyraHXTicks are the behaviors of each microphone: the ceiling
// array has no battery to drain, and its call is "active" where the wireless
// pack's is "transmitting".
func lyraArcTicks() []tickPlan {
	return []tickPlan{
		{"call_state", []string{"status.mic.active", "status.mic.mute"}},
		{"level_drift", []string{"status.mic.level_db"}},
	}
}

func lyraHXTicks() []tickPlan {
	return []tickPlan{
		{"call_state", []string{"status.mic.transmitting", "status.mic.mute"}},
		{"level_drift", []string{"status.mic.level_db"}},
		{"battery_drain", []string{"status.mic.battery_percent"}},
	}
}

// audioOperator is the parity day's operator: a hand on a path at given times.
func audioOperator(d *Device, hand map[string]map[string]any) func(string) {
	return func(hms string) {
		for path, v := range hand[hms] {
			setWirePath(nil2t{}, d, path, v)
		}
	}
}

// nil2t is a testing.TB for an operator hand that cannot fail the test it is
// not given: a bad path in the script panics, which fails it loudly.
type nil2t struct{ testing.TB }

func (nil2t) Helper() {}
func (nil2t) Fatalf(format string, args ...any) {
	panic("wire operator: " + format)
}

func lyraRoomDay(d *Device, at *time.Time) []wireChange {
	w := newWatch(d, "status.mic.active", "status.mic.transmitting", "status.mic.mute", "status.mic.level_db", "status.mic.battery_percent")
	w.sample("start")
	hand := audioOperator(d, map[string]map[string]any{
		"10:15:00": {"status.mic.mute": true},
		"11:00:00": {"status.mic.mute": false},
	})
	roomDayWalk(d, at, w, "07:00:00", "15:00:00", 30*time.Second, func(string) {
		for _, name := range []string{"call_state", "level_drift", "battery_drain"} {
			d.TickBehavior(name)
		}
	}, hand)
	return w.out
}

// ---- the Polaris DSP ----

func dspScript() []wireStep {
	steps := []wireStep{
		wireGet("the dashboard", "/"),
		{Name: "the dashboard with no key", Method: "GET", Path: "/", NoKey: true},
		{Name: "the status with no key", Method: "GET", Path: "/api/status", NoKey: true},
		wireGet("the status document", "/api/status"),
		wireGet("the status as xml", "/api/status.xml"),
		wireGet("the channels", "/api/channels"),
		{Name: "an unknown path", Method: "GET", Path: "/nope"},
		{Name: "the wrong method on a route", Method: "GET", Path: "/api/channels/mute"},
		{Name: "a head request on the root", Method: "HEAD", Path: "/"},
		{Name: "a post to the root", Method: "POST", Path: "/", Body: `{}`},
		wirePost("channel 0 muted", "/api/channels/mute", `{"index": 0, "mute": true}`),
		wirePost("channel 0 live", "/api/channels/mute", `{"index": 0, "mute": false}`),
		wirePost("channel 4 live", "/api/channels/mute", `{"index": 4, "mute": false}`),
		wirePost("channel 15 muted", "/api/channels/mute", `{"index": 15, "mute": true}`),
		wirePost("channel by a text index", "/api/channels/mute", `{"index": "2", "mute": true}`),
		wirePost("mute as a word", "/api/channels/mute", `{"index": 3, "mute": "true"}`),
		wirePost("a whole index written as a fraction", "/api/channels/mute", `{"index": 1.0, "mute": true}`),
		wirePost("channel 0 gain", "/api/channels/gain", `{"index": 0, "gain": -12}`),
		wirePost("channel 1 gain up", "/api/channels/gain", `{"index": 1, "gain": 12}`),
		wirePost("channel 2 gain down", "/api/channels/gain", `{"index": 2, "gain": -60}`),
		wirePost("channel 3 gain a fraction", "/api/channels/gain", `{"index": 3, "gain": -7.5}`),
		wirePost("channel 5 gain as text", "/api/channels/gain", `{"index": 5, "gain": "-9"}`),
		wirePost("gain out of the stated range", "/api/channels/gain", `{"index": 6, "gain": 99}`),
		wireGet("the channels changed", "/api/channels"),
		wirePost("an index past the last channel", "/api/channels/mute", `{"index": 16, "mute": true}`),
		wirePost("a negative index", "/api/channels/mute", `{"index": -1, "mute": true}`),
		wirePost("a far index", "/api/channels/gain", `{"index": 99, "gain": -3}`),
		wirePost("a word index", "/api/channels/mute", `{"index": "all", "mute": true}`),
		wirePost("a null index", "/api/channels/mute", `{"index": null, "mute": true}`),
		wirePost("a boolean index", "/api/channels/gain", `{"index": true, "gain": 1}`),
		wirePost("an empty text index", "/api/channels/gain", `{"index": "", "gain": 1}`),
		wirePost("no index", "/api/channels/mute", `{"mute": true}`),
		wirePost("no body", "/api/channels/mute", ``),
		wirePost("an empty document", "/api/channels/gain", `{}`),
		wirePost("mute off the list", "/api/output/mute", `{"mute": "loud"}`),
		wirePost("output muted", "/api/output/mute", `{"mute": true}`),
		wireGet("the status muted", "/api/status"),
		wirePost("output live", "/api/output/mute", `{"mute": false}`),
		wirePost("output muted as a word", "/api/output/mute", `{"mute": "true"}`),
		wirePost("output unmuted as a word", "/api/output/mute", `{"mute": "false"}`),
		wirePost("output a number", "/api/output/mute", `{"mute": 1}`),
		wirePost("output with no field", "/api/output/mute", `{}`),
		wirePost("output with no body", "/api/output/mute", ``),
		{Name: "reboot with the volatile state drifted", Method: "POST", Path: "/api/admin/reboot", Body: `{}`, Snapshot: true, Set: map[string]any{
			"status.dsp.cpu_load": 55, "status.dsp.temperature": 51, "status.dsp.fan_rpm": 4100, "status.dsp.active_channels": 3,
			"status.output.mute": true, "status.output.level_db": -9.5, "status.channels.0.gain": -20,
		}},
		wireGet("the status after a reboot", "/api/status"),
		wireGet("the channels after a reboot", "/api/channels"),
		{Name: "factory reset with the volatile state drifted", Method: "POST", Path: "/api/admin/factory-reset", Body: `{}`, Snapshot: true, Set: map[string]any{
			"status.dsp.cpu_load": 44, "status.dsp.temperature": 49, "status.dsp.fan_rpm": 4300, "status.dsp.active_channels": 2,
			"status.output.mute": true, "status.output.level_db": -30.5,
		}},
		wireGet("the status after a factory reset", "/api/status"),
		wireGet("the dashboard after a factory reset", "/"),
		wireGet("the dashboard with a drifted device", "/"),
	}
	// the last step reads a device a scenario has moved
	steps[len(steps)-1].Set = map[string]any{
		"status.dsp.cpu_load": 61.5, "status.dsp.temperature": 47.3, "status.dsp.fan_rpm": 4012, "status.dsp.active_channels": 5,
		"status.channels.0.gate": true, "status.channels.0.peak_db": -33.5, "status.channels.0.gain": 3, "status.channels.1.mute": true,
	}
	return append(steps,
		unreadable("a channel array body", "/api/channels/mute", `[0, true]`),
		unreadable("a channel text body", "/api/channels/mute", `index=0&mute=true`),
		unreadable("a gain null body", "/api/channels/gain", `null`),
		unreadable("an output array body", "/api/output/mute", `[true]`),
		unreadable("an output text body", "/api/output/mute", `mute=true`),
		quirk("a fractional index", "/api/channels/mute", `{"index": 1.5, "mute": true}`, "the path gained a key that is no channel"),
		quirk("a fractional text index", "/api/channels/gain", `{"index": "2.5", "gain": 1}`, "the path gained a key that is no channel"),
		quirk("a mute that is a word", "/api/channels/mute", `{"index": 7, "mute": "loud"}`, "the template wrote the word into state"),
		quirk("a mute that is a number", "/api/channels/mute", `{"index": 8, "mute": 7}`, "the template wrote the number into state"),
		quirk("a mute with no value", "/api/channels/mute", `{"index": 9}`, "the template wrote <no value> into state"),
		quirk("a gain that is a word", "/api/channels/gain", `{"index": 10, "gain": "loud"}`, "the template wrote the word into state"),
		quirk("a gain with no value", "/api/channels/gain", `{"index": 11}`, "the template wrote <no value> into state"),
		quirk("a gain that is null", "/api/channels/gain", `{"index": 12, "gain": null}`, "the template wrote <nil> into state"),
		quirk("a gain that is a boolean", "/api/channels/gain", `{"index": 13, "gain": true}`, "the template wrote the word into state"),
	)
}

// quirk is a request the profile answered by an accident of its templates; the
// family refuses it and changes nothing.
func quirk(name, path, body, why string) wireStep {
	return wireStep{Name: name, Method: "POST", Path: path, Body: body, Snapshot: true, Quirk: why}
}

func dspWrites() []wireStep {
	return []wireStep{
		wirePost("output muted", "/api/output/mute", `{"mute": true}`),
		{Name: "the level a scenario set", Method: "GET", Path: "/api/status", Set: map[string]any{"status.output.level_db": -23.5, "status.dsp.cpu_load": 33.5, "status.dsp.active_channels": 4, "status.dsp.temperature": 44.5, "status.dsp.fan_rpm": 3900}},
	}
}

var dspOIDs = []string{
	"1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.3.0", "1.3.6.1.2.1.1.5.0",
	"1.3.6.1.4.1.55540.2.1.0", "1.3.6.1.4.1.55540.2.2.0", "1.3.6.1.4.1.55540.2.3.0", "1.3.6.1.4.1.55540.2.4.0",
	"1.3.6.1.4.1.55540.2.5.0", "1.3.6.1.4.1.55540.2.6.0",
	"1.3.6.1.4.1.55540.5.1.0", "1.3.6.1.4.1.55540.5.2.0", "1.3.6.1.4.1.55540.5.3.0", "1.3.6.1.4.1.55540.5.4.0", "1.3.6.1.4.1.55540.5.5.0",
	"1.3.6.1.4.1.55540.9.9.0",
}

var polarisWalks = []string{"1.3.6.1.4.1.55540", "1.3.6.1.2.1.1"}

func dspTicks() []tickPlan {
	return []tickPlan{
		{"cpu_drift", []string{"status.dsp.cpu_load"}},
		{"temperature_drift", []string{"status.dsp.temperature"}},
		{"output_level_drift", []string{"status.output.level_db"}},
		{"room_signal", []string{"status.dsp.active_channels", "status.channels.0.gate", "status.channels.0.peak_db", "status.channels.3.peak_db"}},
	}
}

func dspRoomDay(d *Device, at *time.Time) []wireChange {
	paths := []string{"status.dsp.active_channels"}
	for _, i := range []string{"0", "1", "2", "3"} {
		paths = append(paths, "status.channels."+i+".gate", "status.channels."+i+".peak_db", "status.channels."+i+".mute")
	}
	paths = append(paths, "status.output.level_db")
	w := newWatch(d, paths...)
	w.sample("start")
	hand := audioOperator(d, map[string]map[string]any{
		"10:00:00": {"status.channels.1.mute": true},
		"11:30:00": {"status.channels.1.mute": false},
	})
	roomDayWalk(d, at, w, "07:00:00", "15:00:00", 2*time.Minute, func(string) {
		d.TickBehavior("room_signal")
		d.TickBehavior("output_level_drift")
	}, hand)
	return w.out
}

// dspSessions play the shell of each persistent face: SSH and Telnet answer the
// DSP's CLI, the raw TCP face its JSON-RPC.
func dspSessions() []wireSession {
	set := func(kv ...any) map[string]any {
		m := map[string]any{}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}
	cli := func(face string) wireSession {
		return wireSession{Face: face, Steps: []wireLineStep{
			{Name: "the reads", Send: []string{"show dsp", "show channels"}},
			{Name: "help", Send: []string{"help"}},
			{Name: "an unknown command", Send: []string{"bogus", "show nothing", "SHOW DSP"}},
			{Name: "an unknown subscription", Send: []string{"subscribe channels.2.gain", "unsubscribe channels.1.gain"}},
			{Name: "subscribe to a gain", Send: []string{"subscribe channels.0.gain"}},
			{Name: "the gain moves", Do: &wireStep{Name: "gain -9", Method: "POST", Path: "/api/channels/gain", Body: `{"index": 0, "gain": -9}`}},
			{Name: "another channel's gain moves", Do: &wireStep{Name: "channel 1 gain", Method: "POST", Path: "/api/channels/gain", Body: `{"index": 1, "gain": -2.5}`}},
			{Name: "subscribe to a mute and the second gain", Send: []string{"subscribe channels.0.mute", "subscribe channels.1.gain"}},
			{Name: "the mute moves", Do: &wireStep{Name: "mute", Method: "POST", Path: "/api/channels/mute", Body: `{"index": 0, "mute": true}`}},
			{Name: "the second gain moves", Do: &wireStep{Name: "channel 1 gain again", Method: "POST", Path: "/api/channels/gain", Body: `{"index": 1, "gain": 4}`}},
			{Name: "unsubscribe the first gain", Send: []string{"unsubscribe channels.0.gain"}},
			{Name: "the first gain moves unheard", Do: &wireStep{Name: "gain again", Method: "POST", Path: "/api/channels/gain", Body: `{"index": 0, "gain": -4}`}},
			{Name: "subscribe to the load", Send: []string{"subscribe dsp.cpu_load"}},
			{Name: "a reboot moves the drifted load", Do: &wireStep{Name: "reboot", Method: "POST", Path: "/api/admin/reboot", Body: `{}`,
				Set: set("status.dsp.cpu_load", 55, "status.dsp.temperature", 51.5, "status.output.mute", true)}},
			{Name: "the reboot over the shell", Send: []string{"show dsp"}},
			{Name: "a reboot that moves nothing", Do: &wireStep{Name: "reboot again", Method: "POST", Path: "/api/admin/reboot", Body: `{}`}},
			{Name: "a factory reset moves the load again", Do: &wireStep{Name: "factory reset", Method: "POST", Path: "/api/admin/factory-reset", Body: `{}`,
				Set: set("status.dsp.cpu_load", 41, "status.dsp.temperature", 44, "status.dsp.active_channels", 6, "status.dsp.fan_rpm", 4100)}},
			{Name: "the shell sees a drift", Send: []string{"show dsp"}, Do: &wireStep{Name: "drift", Method: "GET", Path: "/api/status",
				Set: set("status.dsp.cpu_load", 63, "status.dsp.temperature", 55.5, "status.output.mute", true, "status.output.level_db", -3.5)}},
			{Name: "system reboot", Send: []string{"system reboot"}},
			{Name: "system reboot with nothing to move", Send: []string{"system reboot"}},
			{Name: "the state after the shell's reboot", Send: []string{"show dsp"}, Do: &wireStep{Name: "the status", Method: "GET", Path: "/api/status",
				Set: set("status.dsp.cpu_load", 52, "status.dsp.temperature", 50, "status.dsp.active_channels", 1, "status.dsp.fan_rpm", 3300)}},
			{Name: "system factory-reset", Send: []string{"system factory-reset"}},
			{Name: "show after the factory reset", Send: []string{"show dsp", "show channels"}},
			{Name: "unsubscribe the load", Send: []string{"unsubscribe dsp.cpu_load", "unsubscribe channels.0.mute"}},
			{Name: "subscribe to the temperature", Send: []string{"subscribe dsp.temperature"}},
			{Name: "the shell resets a drifted temperature", Send: []string{"system reboot"}, Do: &wireStep{Name: "drift", Method: "GET", Path: "/api/status",
				Set: set("status.dsp.cpu_load", 31, "status.dsp.temperature", 53.5)}},
			{Name: "the shell factory-resets a drifted temperature", Send: []string{"system factory-reset"}, Do: &wireStep{Name: "drift again", Method: "GET", Path: "/api/status",
				Set: set("status.dsp.cpu_load", 32, "status.dsp.temperature", 48.5)}},
			{Name: "unsubscribe the temperature", Send: []string{"unsubscribe dsp.temperature", "unsubscribe dsp.cpu_load"}},
			{Name: "bye", Send: []string{"bye"}},
		}}
	}
	intervals := func(face string) wireSession {
		return wireSession{Face: face, Steps: []wireLineStep{
			{Name: "interval subscriptions started and stopped", Send: []string{"subscribe dsp.cpu_load 1000", "subscribe dsp.temperature 2000", "unsubscribe dsp.cpu_load", "unsubscribe dsp.temperature"}},
			{Name: "quit", Send: []string{"quit"}},
		}}
	}
	qrc := wireSession{Face: "qrc", Steps: []wireLineStep{
		{Name: "the heartbeat", Send: []string{`{"jsonrpc":"2.0","method":"NoOp","id":0}`}},
		{Name: "the engine status", Send: []string{`{"jsonrpc":"2.0","method":"EngineStatus","id":1}`}},
		{Name: "a component read", Send: []string{`{"jsonrpc":"2.0","method":"Component.Get","params":{"Name":"Mixer1","Controls":[{"Name":"gain"}]},"id":2}`}},
		{Name: "an unknown call", Send: []string{`{"jsonrpc":"2.0","method":"Nope","id":9}`, `{"jsonrpc":"2.0","method":"NoOp","id":7}`, `bogus`}},
		{Name: "a CLI line over the raw face", Send: []string{"show dsp", "help"}},
		{Name: "a change group subscribes", Send: []string{`{"jsonrpc":"2.0","method":"ChangeGroup.AddControl","params":{"Id":"sub1","Controls":["Mixer1.gain"]},"id":3}`}},
		{Name: "the gain moves", Do: &wireStep{Name: "gain -9", Method: "POST", Path: "/api/channels/gain", Body: `{"index": 0, "gain": -9}`}},
		{Name: "the gain moves to a fraction", Do: &wireStep{Name: "gain -4.5", Method: "POST", Path: "/api/channels/gain", Body: `{"index": 0, "gain": -4.5}`}},
		{Name: "another strip moves unheard", Do: &wireStep{Name: "channel 1", Method: "POST", Path: "/api/channels/gain", Body: `{"index": 1, "gain": 1}`}},
		{Name: "the read after the moves", Send: []string{`{"jsonrpc":"2.0","method":"Component.Get","params":{"Name":"Mixer1","Controls":[{"Name":"gain"}]},"id":2}`}},
		{Name: "the change group is removed", Send: []string{`{"jsonrpc":"2.0","method":"ChangeGroup.Remove","params":{"Id":"sub1"},"id":4}`}},
		{Name: "the gain moves unheard", Do: &wireStep{Name: "gain 0", Method: "POST", Path: "/api/channels/gain", Body: `{"index": 0, "gain": 0}`}},
		{Name: "bye", Send: []string{"bye"}},
	}}
	return []wireSession{cli("ssh-console"), cli("telnet-console"), intervals("ssh-console"), intervals("telnet-console"), qrc}
}

// ---- the Polaris amplifier ----

func ampScript() []wireStep {
	return []wireStep{
		wireGet("the status document", "/api/status"),
		{Name: "the status with no key", Method: "GET", Path: "/api/status", NoKey: true},
		{Name: "an unknown path", Method: "GET", Path: "/nope"},
		{Name: "the root", Method: "GET", Path: "/"},
		{Name: "the wrong method on a route", Method: "GET", Path: "/api/power"},
		wirePost("standby", "/api/power", `{"power": "standby"}`),
		wireGet("the status in standby", "/api/status"),
		wirePost("wake", "/api/power", `{"power": "on"}`),
		wirePost("power refused", "/api/power", `{"power": "off"}`),
		wirePost("power with no field", "/api/power", `{}`),
		wirePost("power with no body", "/api/power", ``),
		wirePost("power with a boolean", "/api/power", `{"power": true}`),
		{Name: "protect reset while normal", Method: "POST", Path: "/api/protect/reset", Body: `{}`, Snapshot: true},
		{Name: "protect reset while latched", Method: "POST", Path: "/api/protect/reset", Body: `{}`, Snapshot: true, Set: map[string]any{"status.amp.protect": "protect"}},
		{Name: "the status with a latched protect and a clip", Method: "GET", Path: "/api/status", Set: map[string]any{
			"status.amp.protect": "protect", "status.amp.signal_present": true, "status.amp.temperature": 48.5, "status.amp.fan_rpm": 3100,
			"status.channels.1.clip": true, "status.channels.1.output_db": -12.5, "status.channels.2.load_ohms": 4.5,
		}},
		{Name: "protect reset with no body", Method: "POST", Path: "/api/protect/reset", Body: ``, Snapshot: true, Set: map[string]any{"status.amp.protect": "overheat"}},
		{Name: "reboot with the volatile state drifted", Method: "POST", Path: "/api/admin/reboot", Body: `{}`, Snapshot: true, Set: map[string]any{
			"status.amp.protect": "protect", "status.amp.temperature": 50.5, "status.amp.fan_rpm": 3300, "status.amp.signal_present": true,
		}},
		wireGet("the status after a reboot", "/api/status"),
		{Name: "no factory reset on an amplifier", Method: "POST", Path: "/api/admin/factory-reset", Body: `{}`},
		unreadable("power with an array", "/api/power", `["on"]`),
		unreadable("power with a text body", "/api/power", `power=on`),
	}
}

func ampWrites() []wireStep {
	return []wireStep{
		wirePost("standby", "/api/power", `{"power": "standby"}`),
		{Name: "values a scenario set", Method: "GET", Path: "/api/status", Set: map[string]any{
			"status.amp.protect": "protect", "status.amp.temperature": 49.5, "status.amp.fan_rpm": 3000,
			"status.channels.0.output_db": -13.5, "status.channels.1.clip": true, "status.channels.3.load_ohms": 16,
		}},
	}
}

var ampOIDs = []string{
	"1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.3.0",
	"1.3.6.1.4.1.55540.3.1.0", "1.3.6.1.4.1.55540.3.2.0", "1.3.6.1.4.1.55540.3.3.0", "1.3.6.1.4.1.55540.3.4.0",
	"1.3.6.1.4.1.55540.3.10.1.2.1", "1.3.6.1.4.1.55540.3.10.1.2.2", "1.3.6.1.4.1.55540.3.10.1.2.3", "1.3.6.1.4.1.55540.3.10.1.2.4",
	"1.3.6.1.4.1.55540.3.10.1.3.1", "1.3.6.1.4.1.55540.3.10.1.3.2", "1.3.6.1.4.1.55540.3.10.1.3.3", "1.3.6.1.4.1.55540.3.10.1.3.4",
	"1.3.6.1.4.1.55540.3.10.1.4.1", "1.3.6.1.4.1.55540.3.10.1.4.2", "1.3.6.1.4.1.55540.3.10.1.4.3", "1.3.6.1.4.1.55540.3.10.1.4.4",
	"1.3.6.1.4.1.55540.5.1.0", "1.3.6.1.4.1.55540.5.2.0", "1.3.6.1.4.1.55540.3.10.1.2.5", "1.3.6.1.4.1.55540.9.9.0",
}

func ampTicks() []tickPlan {
	return []tickPlan{
		{"temperature_drift", []string{"status.amp.temperature"}},
		{"fan_drift", []string{"status.amp.fan_rpm"}},
		{"output_drift", []string{"status.amp.signal_present", "status.channels.0.output_db", "status.channels.1.output_db", "status.channels.2.output_db", "status.channels.3.output_db"}},
	}
}

func ampRoomDay(d *Device, at *time.Time) []wireChange {
	paths := []string{"status.amp.power", "status.amp.signal_present", "status.amp.temperature", "status.amp.fan_rpm"}
	for _, i := range []string{"0", "1", "2", "3"} {
		paths = append(paths, "status.channels."+i+".output_db")
	}
	w := newWatch(d, paths...)
	w.sample("start")
	hand := audioOperator(d, map[string]map[string]any{
		"10:30:00": {"status.amp.power": "standby"},
		"11:30:00": {"status.amp.power": "on"},
	})
	roomDayWalk(d, at, w, "07:00:00", "15:00:00", 2*time.Minute, func(string) {
		for _, name := range []string{"output_drift", "temperature_drift", "fan_drift"} {
			d.TickBehavior(name)
		}
	}, hand)
	return w.out
}

// The audio families, as the harness records and replays them.
var (
	lyraArcWire = wireFamily{
		Name: "lyra-arc", Products: []string{lyraArcProduct}, Engine: shippedEngineOf, Source: wireAudioSource, Frozen: audioFrozen,
		Script: lyraScript(), Writes: lyraWrites(), OIDs: lyraOIDs, Walks: lyraWalks, Ticks: lyraArcTicks(), RoomDay: lyraRoomDay, Faces: true,
	}
	lyraHXWire = wireFamily{
		Name: "lyra-hx", Products: []string{lyraHXProduct}, Engine: shippedEngineOf, Source: wireAudioSource, Frozen: audioFrozen,
		Script: lyraScript(), Writes: lyraWrites(), OIDs: lyraOIDs, Walks: lyraWalks, Ticks: lyraHXTicks(), RoomDay: lyraRoomDay, Faces: true,
	}
	polarisDSPWire = wireFamily{
		Name: "polaris-dsp", Products: []string{polarisDSPProd}, Engine: shippedEngineOf, Source: wireAudioSource, Frozen: audioFrozen,
		Script: dspScript(), Writes: dspWrites(), OIDs: dspOIDs, Walks: polarisWalks, Ticks: dspTicks(), RoomDay: dspRoomDay,
		Faces: true, Sessions: dspSessions(), Echo: []string{"udp-echo"},
	}
	polarisAmpWire = wireFamily{
		Name: "polaris-amp", Products: []string{polarisAmpProd}, Engine: shippedEngineOf, Source: wireAudioSource, Frozen: audioFrozen,
		Script: ampScript(), Writes: ampWrites(), OIDs: ampOIDs, Walks: polarisWalks, Ticks: ampTicks(), RoomDay: ampRoomDay, Faces: true,
	}
)

func init() {
	wireFamilies = append(wireFamilies, lyraArcWire, lyraHXWire, polarisDSPWire, polarisAmpWire)
}
