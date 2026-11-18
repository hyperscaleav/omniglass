package seed

import (
	"encoding/json"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// TestVolumeIsTheCanonicalRatio pins #1429: volume is a dimensionless
// quantity, so it stores the ratio (0 to 1) like battery-charge and
// storage-utilization, and a rule written `volume > 0.9` means 90%.
func TestVolumeIsTheCanonicalRatio(t *testing.T) {
	var doc metricTypesDoc
	if err := yaml.Unmarshal(metricTypesYAML, &doc); err != nil {
		t.Fatal(err)
	}
	for _, m := range doc.MetricTypes {
		if m.Name != "volume" {
			continue
		}
		if m.Unit != "ratio" {
			t.Errorf("volume unit = %q, want ratio", m.Unit)
		}
		if m.Precision == nil || *m.Precision != 3 {
			t.Errorf("volume precision = %v, want 3", m.Precision)
		}
		return
	}
	t.Fatal("volume is not a seeded metric type")
}

// volumeBaked bakes one shipped function of a Kestrel driver, its emits on
// the metric lane as attach does.
func volumeBaked(t *testing.T, id, fn string) *driver.BakedFunction {
	t.Helper()
	sp := shippedSpec(t, id)
	var emits []driver.Emit
	for _, p := range sp.Polls {
		if p.Name == fn {
			emits = p.Emits
		}
	}
	for _, l := range sp.Listeners {
		if l.Name == fn {
			emits = l.Emits
		}
	}
	b := &driver.BakedFunction{Driver: id, Function: fn}
	for _, e := range emits {
		lane := driver.LaneMetric
		if e.Name == "mute" {
			lane = driver.LaneProperty
		}
		b.Emits = append(b.Emits, driver.BakedEmit{Name: e.Name, Lane: lane, Extract: e.Extract, Transform: e.Transform})
	}
	return b
}

func volumeOf(t *testing.T, fn *driver.BakedFunction, body string) float64 {
	t.Helper()
	got, faults := fn.Interpret(driver.Payload{Text: body, JSON: []byte(body)}, time.Now())
	if len(faults) > 0 {
		t.Fatalf("%s faulted: %v", fn.Function, faults)
	}
	for _, e := range got {
		if e.Name == "volume" {
			return e.Value
		}
	}
	t.Fatalf("%s emitted no volume: %+v", fn.Function, got)
	return 0
}

// TestKestrelDriversReadVolumeAsARatio: the bar reports 0 to 100, the driver
// applies the canon, so every inbound path lands 41 as 0.41 exactly.
func TestKestrelDriversReadVolumeAsARatio(t *testing.T) {
	for _, tc := range []struct{ id, fn, body string }{
		{"kestrel-api", "audio", `{"volume": 41, "mic_mute": false}`},
		{"kestrel-api", "volume-change", `{"event":"change","path":"status.audio.volume","value":41}`},
		{"kestrel-ws", "volume", `{"id":"1","type":"reply","value":41}`},
		{"kestrel-ws", "volume-change", `{"type":"change","path":"status.audio.volume","value":41}`},
	} {
		if got := volumeOf(t, volumeBaked(t, tc.id, tc.fn), tc.body); got != 0.41 {
			t.Errorf("%s %s: volume = %v, want 0.41", tc.id, tc.fn, got)
		}
	}
}

// TestKestrelSetVolumeActuatesTheDeviceScale: the intended ratio goes out as
// the 0 to 100 number the bar wants, typed, and the bar's answer settles
// against the intended ratio.
func TestKestrelSetVolumeActuatesTheDeviceScale(t *testing.T) {
	for _, tc := range []struct{ id, body, reply string }{
		{"kestrel-api", `{"volume":41}`, ""},
		{"kestrel-ws", `{"path":"status.audio.volume","type":"set","value":41}`, `{"id":"1","type":"reply","value":41}`},
	} {
		var b driver.CommandBinding
		for _, c := range shippedSpec(t, tc.id).Commands {
			if c.CommandType == "set-volume" {
				b = c
			}
		}
		req, err := driver.RenderCommand(b, "0.41", json.RawMessage(`0.41`), nil)
		if err != nil {
			t.Fatalf("%s: %v", tc.id, err)
		}
		body := string(req.Body)
		if req.Body == nil {
			body = req.Line
		}
		if !jsonEqual(body, tc.body) {
			t.Errorf("%s: request body %s, want %s", tc.id, body, tc.body)
		}
		if _, err := driver.RenderCommand(b, "loud", json.RawMessage(`"loud"`), nil); err == nil {
			t.Errorf("%s: a non-numeric intended value rendered", tc.id)
		}
		if _, err := driver.RenderCommand(b, "", nil, nil); err == nil {
			t.Errorf("%s: a missing intended value rendered", tc.id)
		}
		if tc.reply != "" {
			if v := b.JudgeReply(tc.reply); !v.Succeeded || v.Value != "0.41" {
				t.Errorf("%s: the reply reads %+v, want the ratio 0.41", tc.id, v)
			}
		}
	}
}

func jsonEqual(a, b string) bool {
	var x, y any
	return json.Unmarshal([]byte(a), &x) == nil && json.Unmarshal([]byte(b), &y) == nil && jsonString(x) == jsonString(y)
}

func jsonString(v any) string { b, _ := json.Marshal(v); return string(b) }
