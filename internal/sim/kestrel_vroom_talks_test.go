package sim

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
)

// loadVRoom loads the shipped kestrel-vroom as the simulator does, with its
// live feed in place and its behaviors compiled but not running, so a test
// ticks them by hand.
func loadVRoom(t *testing.T, id string) *Device {
	t.Helper()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(store, profiles.Templates())
	if err := e.LoadDevices([]DeviceSnapshot{{DeviceID: id, Hostname: id, LoopbackIP: "127.0.0.1", ProfileName: "kestrel-vroom", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	dev := e.Devices()[0]
	for _, iface := range store.Devices["kestrel-vroom"].Interfaces {
		if iface.Events.Path != "" {
			dev.SetEvents(iface.Events)
		}
	}
	return dev
}

// tickByName fires one tick of the named behavior.
func tickByName(t *testing.T, d *Device, name string) {
	t.Helper()
	for i := range d.behaviors {
		if d.behaviors[i].Name == name {
			d.tickBehavior(&d.behaviors[i])
			return
		}
	}
	t.Fatalf("kestrel-vroom has no behavior %q", name)
}

// The VRoom talks on its own (#2754): a call about every 90 s, started and
// ended as named events on its live feed with participants joining and
// leaving in between, each occurrence also a log line in the codec's idiom
// (severity, facility, text). Two full cycles of the call cycle's 15 s tick
// (six ticks a call) bracket two calls.
func TestTheVRoomsCallCycleReportsACallAndItsParticipants(t *testing.T) {
	d := loadVRoom(t, "hq-conf-1-videobar-1")
	for range 12 {
		tickByName(t, d, "call_cycle")
	}
	var names []string
	var logs []map[string]any
	started := map[string]bool{}
	for _, ev := range d.events.replayFrom("1") {
		switch ev.name {
		case "change":
			continue
		case "log":
			var line map[string]any
			if err := json.Unmarshal([]byte(ev.data), &line); err != nil {
				t.Fatalf("log line %q is not JSON: %v", ev.data, err)
			}
			logs = append(logs, line)
			continue
		case "call-started":
			var body struct {
				ID           string `json:"id"`
				Participants int    `json:"participants"`
			}
			if err := json.Unmarshal([]byte(ev.data), &body); err != nil || body.ID == "" || body.Participants < 1 {
				t.Fatalf("call-started %q, want an id and a participant count (%v)", ev.data, err)
			}
			if started[body.ID] {
				t.Fatalf("call %s started twice; each call takes a new id", body.ID)
			}
			started[body.ID] = true
		case "participant-joined", "participant-left":
			var body struct {
				CallID       string `json:"call_id"`
				Participants int    `json:"participants"`
			}
			if err := json.Unmarshal([]byte(ev.data), &body); err != nil || !started[body.CallID] {
				t.Fatalf("%s %q names no call in progress (%v)", ev.name, ev.data, err)
			}
		}
		names = append(names, ev.name)
	}
	// The order: each call opens with call-started, any participant moves
	// sit inside it, and call-ended closes it.
	inCall, calls, moves := false, 0, 0
	for _, n := range names {
		switch n {
		case "call-started":
			if inCall {
				t.Fatalf("a call started inside a call: %v", names)
			}
			inCall, calls = true, calls+1
		case "call-ended":
			if !inCall {
				t.Fatalf("a call ended with none in progress: %v", names)
			}
			inCall = false
		default:
			if !inCall {
				t.Fatalf("%s outside a call: %v", n, names)
			}
			moves++
		}
	}
	if calls != 2 || inCall {
		t.Fatalf("12 ticks of 15 s carried %d calls (still in one: %v), want two whole calls: %v", calls, inCall, names)
	}
	if moves == 0 {
		t.Fatalf("no participant joined or left across two calls: %v", names)
	}
	if len(logs) < len(names) {
		t.Fatalf("%d log lines for %d occurrences, want one each", len(logs), len(names))
	}
	for _, l := range logs {
		sev, _ := l["severity"].(string)
		fac, _ := l["facility"].(string)
		text, _ := l["text"].(string)
		if (sev != "INFO" && sev != "WARN") || !strings.HasPrefix(fac, "kvroom.") || text == "" {
			t.Fatalf("log line %v, want a severity, a kvroom facility and its text", l)
		}
	}
	if v := d.ReadPath("status.call.active"); v != false {
		t.Fatalf("after two whole cycles the call is %v, want idle", v)
	}
}

// The call stats drift while a call is up and rest at zero when the room is
// idle (#2754): packet loss a ratio (0 to 1), jitter in milliseconds, the
// bitrate in kbps, the frame rate, the speaker level 0 to 100. The codec's
// own load and temperature drift whether or not it is in a call.
func TestTheVRoomsCallStatsDriftOnlyInACall(t *testing.T) {
	d := loadVRoom(t, "hq-conf-1-videobar-1")
	num := func(path string) float64 {
		t.Helper()
		f, _, err := toFloat(d.ReadPath(path))
		if err != nil {
			t.Fatalf("%s = %v: %v", path, d.ReadPath(path), err)
		}
		return f
	}
	tickByName(t, d, "media_stats")
	for _, p := range []string{"status.media.packet_loss", "status.media.jitter_ms", "status.media.bitrate_kbps", "status.media.frame_rate", "status.audio.speaker_level"} {
		if v := num(p); v != 0 {
			t.Errorf("idle %s = %v, want 0", p, v)
		}
	}
	if v := num("status.device.cpu_load"); v <= 0 || v > 100 {
		t.Errorf("idle cpu_load = %v, want a percentage", v)
	}
	tickByName(t, d, "call_cycle") // the call starts
	for range 4 {
		tickByName(t, d, "media_stats")
		if v := num("status.media.packet_loss"); v < 0 || v > 1 {
			t.Errorf("packet_loss = %v, want a ratio", v)
		}
		if v := num("status.media.bitrate_kbps"); v < 500 || v > 8000 {
			t.Errorf("bitrate_kbps = %v, want a call's", v)
		}
		if v := num("status.media.frame_rate"); v < 15 || v > 60 {
			t.Errorf("frame_rate = %v, want a call's", v)
		}
		if v := num("status.media.jitter_ms"); v <= 0 || v > 200 {
			t.Errorf("jitter_ms = %v, want a call's", v)
		}
		if v := num("status.audio.speaker_level"); v <= 0 || v > 100 {
			t.Errorf("speaker_level = %v, want a call's", v)
		}
	}
	// A lossy stretch is a WARN line on the network facility.
	if err := SetPath(d.state, "status.media.packet_loss", 0.05); err != nil {
		t.Fatal(err)
	}
	mark := d.events.seq
	tickByName(t, d, "media_stats")
	var warned bool
	for _, ev := range d.events.replayFrom(strconv.FormatUint(mark+1, 10)) {
		warned = warned || (ev.name == "log" && strings.Contains(ev.data, `"severity":"WARN","facility":"kvroom.net"`) && strings.Contains(ev.data, "packet loss"))
	}
	if !warned {
		t.Fatalf("a 5%% loss wrote no WARN line: %+v", d.events.replayFrom(strconv.FormatUint(mark+1, 10)))
	}
}

// Every VRoom behavior tick renders clean (#2754): across two whole cycles,
// idle and in a call, no mutation or event fails, so the bar's own log is
// not a stream of template warnings.
func TestTheVRoomsTicksRenderClean(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	d := loadVRoom(t, "hq-conf-1-videobar-1")
	for range 12 {
		tickByName(t, d, "call_cycle")
		tickByName(t, d, "media_stats")
	}
	if buf.Len() > 0 {
		t.Fatalf("the ticks warned:\n%s", buf.String())
	}
}

// No VRoom behavior ticks faster than 15 s (#1360's cost rule): richness
// comes from more kinds of signal, not more samples.
func TestNoVRoomBehaviorTicksFasterThan15s(t *testing.T) {
	d := loadVRoom(t, "bar")
	if len(d.behaviors) == 0 {
		t.Fatal("kestrel-vroom has no behaviors")
	}
	for _, b := range d.behaviors {
		if b.Interval < 15*time.Second {
			t.Errorf("behavior %s ticks every %s, want 15 s or slower", b.Name, b.Interval)
		}
	}
}
