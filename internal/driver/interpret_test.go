package driver_test

import (
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The poll interpreter (#814): locate each emit in the fetched payload, apply
// its declared transform, and type it by its baked lane. The contract worth
// pinning hardest: a metric-lane emit that cannot become a number is a fault,
// never a wrong-lane sample; a missing emit is a fault, never a silent drop.

func bakedSNMP() *driver.BakedFunction {
	return &driver.BakedFunction{
		Driver: "snmp-generic", Function: "scalars",
		Request: &driver.Request{Get: []string{"1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.3.0"}},
		Emits: []driver.BakedEmit{
			{Name: "model-number", Lane: "property", Extract: driver.Extract{OID: "1.3.6.1.2.1.1.1.0"}},
			{Name: "uptime", Lane: "metric", Extract: driver.Extract{OID: "1.3.6.1.2.1.1.3.0"}, Transform: &driver.Transform{Scale: 0.01}},
		},
	}
}

func TestInterpretSNMPScalars(t *testing.T) {
	ts := time.Now()
	payload := driver.Payload{Values: map[string]string{
		"1.3.6.1.2.1.1.1.0": "Boreal AirWall 3", "1.3.6.1.2.1.1.3.0": "8123456",
	}}
	emitted, faults := bakedSNMP().Interpret(payload, ts)
	if len(faults) != 0 {
		t.Fatalf("faults: %v", faults)
	}
	byName := map[string]driver.Emitted{}
	for _, e := range emitted {
		byName[e.Name] = e
	}
	model := byName["model-number"]
	if !model.IsText || model.Text != "Boreal AirWall 3" || model.Lane != "property" {
		t.Fatalf("model-number = %+v, want the sysDescr text on the property lane", model)
	}
	up := byName["uptime"]
	if up.IsText || up.Lane != "metric" || up.Value != 81234.56 {
		t.Fatalf("uptime = %+v, want timeticks scaled to 81234.56 on the metric lane", up)
	}
	if !up.TS.Equal(ts) {
		t.Fatalf("uptime ts = %v, want the collection time", up.TS)
	}
}

func TestInterpretRegexCapture(t *testing.T) {
	fn := &driver.BakedFunction{
		Driver: "newtron-nvp", Function: "status",
		Emits: []driver.BakedEmit{
			{Name: "video-input", Lane: "property", Extract: driver.Extract{Regex: `^INPUT (\S+)$`}},
		},
	}
	emitted, faults := fn.Interpret(driver.Payload{Text: "INPUT hdmi-2"}, time.Now())
	if len(faults) != 0 || len(emitted) != 1 {
		t.Fatalf("emitted %v faults %v", emitted, faults)
	}
	if emitted[0].Text != "hdmi-2" {
		t.Fatalf("capture = %q, want hdmi-2", emitted[0].Text)
	}
}

func TestInterpretKeyAndJSONPath(t *testing.T) {
	fn := &driver.BakedFunction{
		Driver: "kv", Function: "kv",
		Emits: []driver.BakedEmit{
			{Name: "video-input", Lane: "property", Extract: driver.Extract{Key: "input"}},
			{Name: "uptime", Lane: "metric", Extract: driver.Extract{JSONPath: "status.uptime"}},
		},
	}
	payload := driver.Payload{
		Values: map[string]string{"input": "sdi-1"},
		JSON:   []byte(`{"status": {"uptime": 42.5, "power": "on"}}`),
	}
	emitted, faults := fn.Interpret(payload, time.Now())
	if len(faults) != 0 || len(emitted) != 2 {
		t.Fatalf("emitted %v faults %v", emitted, faults)
	}
	byName := map[string]driver.Emitted{}
	for _, e := range emitted {
		byName[e.Name] = e
	}
	if byName["video-input"].Text != "sdi-1" {
		t.Fatalf("key extract = %+v", byName["video-input"])
	}
	if byName["uptime"].Value != 42.5 {
		t.Fatalf("jsonpath extract = %+v", byName["uptime"])
	}
}

func TestInterpretEnumMapAndCast(t *testing.T) {
	fn := &driver.BakedFunction{
		Driver: "kv", Function: "kv",
		Emits: []driver.BakedEmit{
			{Name: "video-input", Lane: "property", Extract: driver.Extract{Key: "in"},
				Transform: &driver.Transform{Map: map[string]string{"1": "hdmi-1", "2": "hdmi-2"}}},
		},
	}
	emitted, faults := fn.Interpret(driver.Payload{Values: map[string]string{"in": "2"}}, time.Now())
	if len(faults) != 0 || emitted[0].Text != "hdmi-2" {
		t.Fatalf("enum map: emitted %v faults %v", emitted, faults)
	}

	// A mapped value with no entry is a fault: projecting the raw through
	// would ship a value the menu never declared.
	_, faults = fn.Interpret(driver.Payload{Values: map[string]string{"in": "9"}}, time.Now())
	if len(faults) != 1 || !strings.Contains(faults[0].Error(), "video-input") {
		t.Fatalf("unmapped enum: faults %v, want one naming the emit", faults)
	}
}

func TestInterpretFaults(t *testing.T) {
	ts := time.Now()

	t.Run("a missing emit is a fault, the rest still land", func(t *testing.T) {
		payload := driver.Payload{Values: map[string]string{"1.3.6.1.2.1.1.1.0": "Boreal AirWall 3"}}
		emitted, faults := bakedSNMP().Interpret(payload, ts)
		if len(emitted) != 1 || emitted[0].Name != "model-number" {
			t.Fatalf("emitted %v, want just model-number", emitted)
		}
		if len(faults) != 1 || !strings.Contains(faults[0].Error(), "uptime") {
			t.Fatalf("faults %v, want one naming uptime", faults)
		}
	})

	t.Run("a metric-lane value that is not numeric is a fault, never a wrong-lane sample", func(t *testing.T) {
		payload := driver.Payload{Values: map[string]string{
			"1.3.6.1.2.1.1.1.0": "Boreal AirWall 3", "1.3.6.1.2.1.1.3.0": "up since tuesday",
		}}
		emitted, faults := bakedSNMP().Interpret(payload, ts)
		for _, e := range emitted {
			if e.Name == "uptime" {
				t.Fatalf("a non-numeric uptime landed as %+v", e)
			}
		}
		if len(faults) != 1 || !strings.Contains(faults[0].Error(), "uptime") {
			t.Fatalf("faults %v, want one naming uptime", faults)
		}
	})

	t.Run("a regex that does not match is a fault", func(t *testing.T) {
		fn := &driver.BakedFunction{
			Driver: "newtron-nvp", Function: "status",
			Emits: []driver.BakedEmit{
				{Name: "video-input", Lane: "property", Extract: driver.Extract{Regex: `^INPUT (\S+)$`}},
			},
		}
		emitted, faults := fn.Interpret(driver.Payload{Text: "ERR unknown command"}, ts)
		if len(emitted) != 0 || len(faults) != 1 {
			t.Fatalf("emitted %v faults %v", emitted, faults)
		}
	})
}

func TestParseBaked(t *testing.T) {
	raw := []byte(`{"driver": "snmp-generic", "version": "1.0.0", "function": "scalars",
		"schedule": {"every": "60s"},
		"request": {"get": ["1.3.6.1.2.1.1.3.0"]},
		"emits": [{"name": "uptime", "lane": "metric", "extract": {"oid": "1.3.6.1.2.1.1.3.0"}, "transform": {"scale": 0.01}}]}`)
	fn, err := driver.ParseBaked(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if fn.Driver != "snmp-generic" || len(fn.Emits) != 1 || fn.Emits[0].Lane != "metric" {
		t.Fatalf("baked = %+v", fn)
	}
	// The reachability task's bare "{}" spec is NOT a driver task.
	if driver.IsBaked([]byte(`{}`)) {
		t.Fatal("{} read as a driver task")
	}
	if !driver.IsBaked(raw) {
		t.Fatal("a baked function not recognized")
	}
}

func TestRenderRequest(t *testing.T) {
	req := driver.Request{Line: "SET INPUT ${value}"}
	got, err := driver.RenderRequest(req, "hdmi-2", nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got.Line != "SET INPUT hdmi-2" {
		t.Fatalf("line = %q", got.Line)
	}

	got, err = driver.RenderRequest(driver.Request{Line: "SET LEVEL ${arg.zone} ${value}"}, "-20", map[string]any{"zone": 3})
	if err != nil || got.Line != "SET LEVEL 3 -20" {
		t.Fatalf("line = %q err %v", got.Line, err)
	}

	// A reference nothing supplies refuses: a half-rendered line never
	// reaches a device.
	if _, err := driver.RenderRequest(driver.Request{Line: "SET INPUT ${arg.input}"}, "", nil); err == nil {
		t.Fatal("missing arg rendered")
	}
	if _, err := driver.RenderRequest(driver.Request{Line: "SET INPUT ${value}"}, "", nil); err == nil {
		t.Fatal("missing value rendered")
	}
	if _, err := driver.RenderRequest(driver.Request{Line: "X ${warp.factor}"}, "v", nil); err == nil {
		t.Fatal("unknown template field rendered")
	}
}
