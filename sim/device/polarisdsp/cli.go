package polarisdsp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/wire"
)

// The DSP's line protocols (#2835): a CLI over SSH and Telnet, and a JSON-RPC
// control plane over raw TCP. Every persistent face serves every command (a
// device has one command set, whichever face a line arrives on), so the shell
// is one Commander and the faces differ only in framing, welcome and the line a
// subscription pushes.

// ---- the CLI verbs that read and restart ----

// A verb is a line the CLI answers, and what the console lists of it.
type verb struct {
	line, summary string
	// telnet says the Telnet face lists it too; the SSH face lists every verb.
	telnet bool
}

var cliVerbs = []verb{
	{"show dsp", "DSP health and load", true},
	{"show channels", "Active channel summary", true},
	{"system reboot", "Reboot DSP", true},
	{"system factory-reset", "Factory reset DSP", true},
}

// reads answers the verbs that read, and restarts the ones that write: a
// reboot or a factory reset is the one list the HTTP route of the same name
// writes, acknowledged as the CLI acknowledges any verb that changed something.
func reads(d spec.Device, line string) surface.Reply {
	switch line {
	case "show dsp":
		return surface.Reply{Text: showDSP(d)}
	case "show channels":
		return surface.Reply{Text: showChannels(d)}
	case "system reboot":
		return surface.Reply{Text: "+OK\n", Changes: restart(d, rebootPaths)}
	default:
		return surface.Reply{Text: "+OK\n", Changes: restart(d, factoryResetPaths)}
	}
}

func showDSP(d spec.Device) string {
	var b strings.Builder
	row := func(label string, v any) { fmt.Fprintf(&b, "%-16s : %v\n", label, v) }
	row("model", d.Read(pathModel))
	row("firmware", d.Read(pathFirmware))
	row("uptime (s)", d.Read(pathUptime))
	row("cpu load (%)", d.Read(pathCPU))
	row("active channels", fmt.Sprintf("%v / %d", d.Read(pathActive), len(base().Strips)))
	row("temperature (C)", d.Read(pathTemperature))
	row("fan (rpm)", d.Read(pathFan))
	return b.String()
}

func showChannels(d spec.Device) string {
	var b strings.Builder
	b.WriteString("idx  name             gain    mute   peak\n---  ---------------  ------  -----  ------\n")
	list, _ := d.Read(listChannels).([]any)
	for i := range list {
		fmt.Fprintf(&b, "%3v  %-15s  %3v dB  %-5t  %3v dB\n",
			d.Read(channelPath(i, "index")), spec.String(d, channelPath(i, "name")), d.Read(channelPath(i, "gain")),
			spec.Bool(d, channelPath(i, "mute")), d.Read(channelPath(i, "peak_db")))
	}
	return b.String()
}

// ---- subscriptions ----

// A subscription a CLI verb registers: the state path it watches, and the verb
// and token (the publish token a push names) it goes by.
type subscription struct {
	verb    string // "channels.0.gain", the word after subscribe
	path    string
	token   string
	every   int // the interval variant's period in milliseconds, 0 for on change
	unsubOK bool
	// telnet says the Telnet face lists it (and its unsubscribe) too.
	telnet bool
}

// The CLI's subscriptions, in the order the help lists them. Only the first
// three of each kind can be taken back, as the verbs the profile declared.
var cliSubscriptions = []subscription{
	{verb: "channels.0.gain", path: channelPath(0, "gain"), token: "gain", unsubOK: true, telnet: true},
	{verb: "channels.1.gain", path: channelPath(1, "gain"), token: "gain"},
	{verb: "channels.0.mute", path: channelPath(0, "mute"), token: "mute", unsubOK: true, telnet: true},
	{verb: "dsp.cpu_load", path: pathCPU, token: "dsp", unsubOK: true, telnet: true},
	{verb: "dsp.temperature", path: pathTemperature, token: "dsp", unsubOK: true},
	// Interval (heartbeat) variants emit on a fixed cadence regardless of
	// state change; the number after the verb is the period in milliseconds
	// (floored at the face's publish_min_interval_ms).
	{verb: "dsp.cpu_load 1000", path: pathCPU, token: "dsp", every: 1000, telnet: true},
	{verb: "dsp.temperature 2000", path: pathTemperature, token: "dsp", every: 2000},
}

// cliSubjects are what the CLI faces may push: a change to a channel's gain or
// mute, or to a DSP reading, as `! "publishToken":... "subject":... "value":...`.
func cliSubjects() []wire.Subject {
	line := func(subject, token string, value any) string {
		return fmt.Sprintf(`! "publishToken":%s "subject":%s "value":%s`, quote(token), quote(subject), jsonOf(value))
	}
	return []wire.Subject{
		{Match: "status.channels.*.gain", PublishToken: "gain", PushLine: line},
		{Match: "status.channels.*.mute", PublishToken: "mute", PushLine: line},
		{Match: "status.dsp.*", PublishToken: "dsp", PushLine: line},
	}
}

// listed is what the console lists of a face's commands: the lines it was
// documented with. The shell answers every line on every face, as the device's
// one command set; these are the documentation of what each face is for.
func listed(telnet bool) []wire.Command {
	var out []wire.Command
	for _, v := range cliVerbs {
		if telnet && !v.telnet {
			continue
		}
		c := wire.Command{Match: v.line}
		if !telnet {
			c.Doc.Summary = v.summary
		}
		out = append(out, c)
	}
	for _, sub := range cliSubscriptions {
		if !telnet || sub.telnet {
			out = append(out, wire.Command{Match: "subscribe " + sub.verb, Action: "subscribe"})
		}
	}
	for _, sub := range cliSubscriptions {
		if sub.unsubOK && (!telnet || sub.telnet) {
			out = append(out, wire.Command{Match: "unsubscribe " + sub.verb, Action: "unsubscribe"})
		}
	}
	return out
}

func quote(s string) string { return `"` + s + `"` }

func jsonOf(v any) string {
	if v == nil {
		return "null"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// ---- the JSON-RPC control plane ----

// Control-plane messages are JSON-RPC 2.0 objects framed by a single NUL byte.
// A fictional addition to the DSP to teach the raw-TCP transport and the
// wire-quirks schema: a real Polaris would not speak it. A request is matched
// whole, as written, so an id other than the documented one is no command.

const designName = "polaris-dsp-16"

type (
	call struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
		ID      int    `json:"id"`
	}
	reply struct {
		JSONRPC string `json:"jsonrpc"`
		Result  any    `json:"result"`
		ID      int    `json:"id"`
	}
	engineStatus struct {
		State      string `json:"State"`
		DesignName string `json:"DesignName"`
	}
	notice struct {
		JSONRPC string       `json:"jsonrpc"`
		Method  string       `json:"method"`
		Params  engineStatus `json:"params"`
	}
	controlName struct {
		Name string `json:"Name"`
	}
	componentGet struct {
		Name     string        `json:"Name"`
		Controls []controlName `json:"Controls"`
	}
	controlValue struct {
		Name   string `json:"Name"`
		String string `json:"String"`
		Value  any    `json:"Value"`
	}
	componentValues struct {
		Name     string         `json:"Name"`
		Controls []controlValue `json:"Controls"`
	}
	changeGroupAdd struct {
		ID       string   `json:"Id"`
		Controls []string `json:"Controls"`
	}
	changeGroupRemove struct {
		ID string `json:"Id"`
	}
	poll struct {
		JSONRPC string     `json:"jsonrpc"`
		Method  string     `json:"method"`
		Params  pollParams `json:"params"`
	}
	pollParams struct {
		ID      string       `json:"Id"`
		Changes []pollChange `json:"Changes"`
	}
	pollChange struct {
		Component string `json:"Component"`
		Name      string `json:"Name"`
		Value     any    `json:"Value"`
	}
)

// The control the change group watches, and the id the one subscription goes by.
const (
	component = "Mixer1"
	group     = "sub1"
)

func text(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func activeEngine() engineStatus { return engineStatus{State: "Active", DesignName: designName} }

// qrcFace is the raw-TCP control plane: no auth dialog, no negotiation, every
// connect speaks the wire protocol at once.
func qrcFace() wire.Interface {
	return wire.Interface{
		Name: "qrc", Protocol: "tcp", Port: 1710, APISurface: "polaris-qrc",
		Doc:        wire.Doc{Summary: "JSON-RPC control over raw TCP (NUL-terminated)", Tags: []string{"control", "persistent-session", "raw-tcp"}},
		Persistent: true, LineEnding: "\x00", Commands: qrcListing(),
		Welcome: text(notice{JSONRPC: "2.0", Method: "EngineStatus", Params: activeEngine()}),
		Limits:  wire.SessionLimits{MaxSubsPerSession: 64, PublishMinIntervalMs: 50, MaxSessionsPerDevice: 16, OutBuffer: 256},
		Subjects: []wire.Subject{{
			Match: "status.channels.*.gain", PublishToken: component + ".gain",
			PushLine: func(_, token string, value any) string {
				return text(poll{JSONRPC: "2.0", Method: "ChangeGroup.Poll", Params: pollParams{ID: token, Changes: []pollChange{{Component: component, Name: "gain", Value: value}}}})
			},
		}},
	}
}

// qrcListing is what the console lists of the control plane: the calls it was
// documented with, the heartbeat first.
func qrcListing() []wire.Command {
	out := []wire.Command{{Match: text(call{JSONRPC: "2.0", Method: "NoOp", ID: 0})}}
	out[0].Doc.Summary = "Heartbeat"
	for _, c := range []struct {
		call   call
		action string
	}{
		{call{JSONRPC: "2.0", Method: "EngineStatus", ID: 1}, ""},
		{call{JSONRPC: "2.0", Method: "Component.Get", Params: componentGet{component, []controlName{{"gain"}}}, ID: 2}, ""},
		{call{JSONRPC: "2.0", Method: "ChangeGroup.AddControl", Params: changeGroupAdd{group, []string{component + ".gain"}}, ID: 3}, "subscribe"},
		{call{JSONRPC: "2.0", Method: "ChangeGroup.Remove", Params: changeGroupRemove{group}, ID: 4}, "unsubscribe"},
	} {
		out = append(out, wire.Command{Match: text(c.call), Action: c.action})
	}
	return out
}

// ---- the shell ----

// shell answers the subscription verbs and the control plane's calls for one
// device.
type shell struct {
	d     spec.Device
	lines []string
	by    map[string]func() surface.Reply
}

func newShell(d spec.Device) *shell {
	s := &shell{d: d, by: map[string]func() surface.Reply{}}
	add := func(line string, answer func() surface.Reply) {
		s.lines = append(s.lines, line)
		s.by[line] = answer
	}
	ok := func(r surface.Reply) surface.Reply { r.Text = "+OK"; return r }
	for _, v := range cliVerbs {
		add(v.line, func() surface.Reply { return reads(d, v.line) })
	}
	for _, sub := range cliSubscriptions {
		add("subscribe "+sub.verb, func() surface.Reply {
			return ok(surface.Reply{Action: surface.ActionSubscribe, Subject: sub.path, IntervalMs: sub.every})
		})
	}
	for _, sub := range cliSubscriptions {
		if sub.unsubOK {
			add("unsubscribe "+sub.verb, func() surface.Reply { return ok(surface.Reply{Action: surface.ActionUnsubscribe, Subject: sub.path}) })
		}
	}
	gain := channelPath(0, "gain")
	rpc := func(c call, answer func() surface.Reply) { add(text(c), answer) }
	rpc(call{JSONRPC: "2.0", Method: "NoOp", ID: 0}, func() surface.Reply {
		return surface.Reply{Text: text(reply{"2.0", true, 0})}
	})
	rpc(call{JSONRPC: "2.0", Method: "EngineStatus", ID: 1}, func() surface.Reply {
		return surface.Reply{Text: text(reply{"2.0", activeEngine(), 1})}
	})
	rpc(call{JSONRPC: "2.0", Method: "Component.Get", Params: componentGet{component, []controlName{{"gain"}}}, ID: 2}, func() surface.Reply {
		v := controlValue{Name: "gain", String: spec.String(d, gain), Value: d.Read(gain)}
		return surface.Reply{Text: text(reply{"2.0", componentValues{component, []controlValue{v}}, 2})}
	})
	rpc(call{JSONRPC: "2.0", Method: "ChangeGroup.AddControl", Params: changeGroupAdd{group, []string{component + ".gain"}}, ID: 3}, func() surface.Reply {
		return surface.Reply{Text: text(reply{"2.0", true, 3}), Action: surface.ActionSubscribe, Subject: gain, Token: group}
	})
	rpc(call{JSONRPC: "2.0", Method: "ChangeGroup.Remove", Params: changeGroupRemove{group}, ID: 4}, func() surface.Reply {
		return surface.Reply{Text: text(reply{"2.0", true, 4}), Action: surface.ActionUnsubscribe, Subject: gain}
	})
	return s
}

// Commands lists every line the shell answers, in the order help shows them.
func (s *shell) Commands() []string { return s.lines }

// Handle answers one line.
func (s *shell) Handle(line string) (surface.Reply, bool) {
	answer, ok := s.by[line]
	if !ok {
		return surface.Reply{}, false
	}
	return answer(), true
}
