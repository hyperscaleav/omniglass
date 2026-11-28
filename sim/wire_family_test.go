//go:build linux

package sim

import (
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/surface/httpapi"
	"github.com/hyperscaleav/omniglass/sim/surface/snmp"
	"github.com/hyperscaleav/omniglass/sim/wire"
)

// The generic wire harness proven by a family of its own (#2845): a small lamp
// served over HTTP and SNMP that drifts, follows its room and reports an
// uptime, recorded and replayed through wireFamily with no Boreal code
// involved. It is the worked example of what a family's wire script is: a
// wireFamily value and a list of steps, nothing more.

const (
	lampName   = "wiretest-lamp"
	lampPower  = "status.lamp.power"
	lampLevel  = "status.lamp.level"
	lampTemp   = "status.lamp.temperature"
	lampBusy   = "status.lamp.busy"
	lampUp     = "status.device.uptime"
	lampBoot   = "status.device.boot_epoch"
	lampArc    = "1.3.6.1.4.1.99999."
	lampDescr  = "configuration.device.model"
	lampOIDPow = lampArc + "1.1.0"
)

type lampRow struct {
	Product, Model string
	Watts          int
}

var lampRows = []lampRow{{"wt-lamp-1", "Lamp 1", 40}, {"wt-lamp-2", "Lamp 2", 60}}

// lampSpec is the test family; drifted is a copy that answers a power change
// differently, to prove the replay notices.
func lampSpec(drifted bool) spec.Spec {
	ack := func(d spec.Device) any {
		if drifted {
			return map[string]any{"ok": true, "lamp": spec.String(d, lampPower)}
		}
		return map[string]any{"ok": true, "power": spec.String(d, lampPower)}
	}
	return spec.Spec{
		Name:     lampName,
		Identity: spec.Identity{Manufacturer: "Wiretest", Model: "Lamp", DeviceType: "display", Summary: "A lamp the wire harness is proven on"},
		Models: spec.Table(lampRows, func(r lampRow) spec.Model {
			return spec.Model{Product: r.Product, Model: r.Model, State: map[string]any{"configuration": map[string]any{"device": map[string]any{"model": r.Model}}}}
		}),
		Interfaces: []wire.Interface{
			{Name: "api", Protocol: "http", Port: 8080},
			{Name: "snmp", Protocol: "snmp", Port: 161, Auth: wire.Auth{Community: "public"}},
		},
		State: map[string]any{
			"status": map[string]any{
				"device": map[string]any{"boot_epoch": 0},
				"lamp":   map[string]any{"power": "on", "level": 50, "temperature": 40.0, "busy": false},
			},
			"configuration": map[string]any{"device": map[string]any{"model": "Lamp"}},
		},
		Elapsed: []spec.Elapsed{{Path: lampUp, Since: lampBoot}},
		HTTP: []spec.HTTPRoute{
			{Match: "GET /api/status", Handle: func(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
				return spec.JSON(map[string]any{"power": spec.String(d, lampPower), "level": spec.Int(d, lampLevel), "uptime": spec.Int64(d, lampUp), "temperature": spec.Float(d, lampTemp)})
			}},
			httpapi.Control{
				Match: "POST /api/power", Fields: []httpapi.Field{{Path: lampPower, Name: "power", Kind: httpapi.Choice, Values: []string{"on", "off"}}}, Ack: ack,
			}.Route(),
			httpapi.Control{
				Match: "POST /api/level", Fields: []httpapi.Field{{Path: lampLevel, Name: "level", Kind: httpapi.Number}}, Ack: ack,
			}.Route(),
			httpapi.Action{
				Match: "POST /api/admin/reboot",
				Writes: func(d spec.Device) []surface.Change {
					return []surface.Change{{Path: lampBoot, Value: d.Now().Unix()}}
				},
				Ack: map[string]any{"ok": true},
			}.Route(),
		},
		Ticks: []spec.Tick{
			{Name: "temperature_drift", Interval: time.Minute, Writes: []string{lampTemp}, Run: func(d spec.Device, c spec.TickContext) []surface.Change {
				return []surface.Change{{Path: lampTemp, Value: behave.Drift(spec.Float(d, lampTemp), behave.Band{Lo: 30, Hi: 50, Step: 1}, c.Rand, behave.Places(1))}}
			}},
			{Name: "room_busy", Interval: 30 * time.Second, OnStart: true, Writes: []string{lampBusy}, Run: func(_ spec.Device, c spec.TickContext) []surface.Change {
				return []surface.Change{{Path: lampBusy, Value: c.Room.Scheduled}}
			}},
		},
		Surface: func(d spec.Device) spec.Surface {
			oids := snmp.System{
				Descr: func() string { return spec.String(d, lampDescr) }, UptimeSeconds: func() int64 { return spec.Int64(d, lampUp) },
				DescrPath: lampDescr, UptimePath: lampUp,
			}.OIDs()
			return spec.Surface{OIDs: append(oids, surface.Text(lampOIDPow, func() string { return spec.String(d, lampPower) }).At(lampPower))}
		},
	}
}

func lampEngine(drifted bool) func(testing.TB) *Engine {
	return func(t testing.TB) *Engine {
		return NewEngineOver(shippedCatalog(t, lampSpec(drifted)))
	}
}

func lampScript() []wireStep {
	return []wireStep{
		wireGet("the status document", "/api/status"),
		{Name: "an unknown path", Method: "GET", Path: "/nope"},
		wirePost("power off", "/api/power", `{"power": "off"}`),
		wirePost("power refused", "/api/power", `{"power": "dim"}`),
		wirePost("level 80", "/api/level", `{"level": 80}`),
		{Name: "power with a body no one can read", Method: "POST", Path: "/api/power", Body: `power=on`},
		wirePost("reboot", "/api/admin/reboot", `{}`),
		wireGet("the status document after a reboot", "/api/status"),
		// A request the recorded profile answered by accident goes last.
		{Name: "level a word", Method: "POST", Path: "/api/level", Body: `{"level": "loud"}`, Snapshot: true, Quirk: "the template wrote the word into state"},
	}
}

func lampRoomDay(d *Device, at *time.Time) []wireChange {
	w := newWatch(d, lampBusy)
	w.sample("start")
	roomDayWalk(d, at, w, "07:00:00", "15:00:00", 30*time.Second, func(string) { d.TickBehavior("room_busy") }, func(string) {})
	return w.out
}

func lampFamily(dir string, drifted bool) wireFamily {
	return wireFamily{
		Name: lampName, Products: []string{"wt-lamp-1", "wt-lamp-2"}, Engine: lampEngine(drifted), Dir: dir,
		Source:  "recorded from the test-only lamp family; see sim/testdata/wire/README.md",
		Script:  lampScript(),
		Writes:  []wireStep{wirePost("power off", "/api/power", `{"power": "off"}`)},
		OIDs:    []string{"1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.3.0", lampOIDPow},
		Walks:   []string{"1.3.6.1.2.1.1"},
		Ticks:   []tickPlan{{"temperature_drift", []string{lampTemp}}},
		RoomDay: lampRoomDay,
	}
}

func init() { wireFamilies = append(wireFamilies, lampFamily("", false)) }

// A family with nothing of Boreal's records its transcript through the
// generic harness and replays it from disk: the HTTP steps, the SNMP reads and
// walks, the drift sequence and both room days are all gathered, written and
// held.
func TestWireHarnessRecordsAndReplaysATestOnlyFamily(t *testing.T) {
	fam := lampFamily(t.TempDir(), false)
	for _, product := range fam.Products {
		tr := collectWire(t, fam, product)
		if len(tr.HTTP) != len(fam.Script) || tr.SNMP["rest"] == nil || tr.SNMP["written"] == nil || len(tr.Walks) != 2 || len(tr.Ticks["temperature_drift"]) != tickRuns || len(tr.RoomDays) != 2 || len(tr.RoomDays["calendar"]) < 2 {
			t.Fatalf("%s: the transcript is missing a part: %d http, snmp %v, walks %d, ticks %d, room days %d", product, len(tr.HTTP), tr.SNMP != nil, len(tr.Walks), len(tr.Ticks["temperature_drift"]), len(tr.RoomDays))
		}
		writeGolden(t, fam, tr)
		replayWire(t, fam, product)
	}
	// The uptime reads an hour, the pinned anchor, through Elapsed.
	if got := collectWire(t, fam, "wt-lamp-1").HTTP[0].Response; !strings.Contains(got, `"uptime":3600`) {
		t.Errorf("the status document reads %s, want an uptime of 3600", got)
	}
}

// The committed transcripts of the test family hold: the harness replays what
// the recorder wrote into the repository.
func TestWireGoldenOfTheTestFamilyReplays(t *testing.T) {
	fam := lampFamily("", false)
	for _, product := range fam.Products {
		t.Run(product, func(t *testing.T) { replayWire(t, fam, product) })
	}
}

// A replay that could not fail would prove nothing: a family that answers one
// request differently from its recorded transcript is reported, and the report
// names the step.
func TestWireHarnessReportsAFamilyThatAnswersDifferently(t *testing.T) {
	dir := t.TempDir()
	recorded := lampFamily(dir, false)
	writeGolden(t, recorded, collectWire(t, recorded, "wt-lamp-1"))

	drifted := lampFamily(dir, true)
	diffs := diffWire(t, drifted, "wt-lamp-1", collectWire(t, drifted, "wt-lamp-1"), readGolden(t, drifted, "wt-lamp-1"))
	if len(diffs) == 0 {
		t.Fatal("a family answering a power change differently replayed clean")
	}
	if !strings.Contains(strings.Join(diffs, "\n"), "power off: body differs") {
		t.Errorf("the report does not name the step:\n%s", strings.Join(diffs, "\n"))
	}
}

// A Quirk step is held to a refusal that changes nothing, not to its bytes: a
// family that accepts the request the profile answered by accident is caught.
func TestWireHarnessHoldsAQuirkStepToARefusal(t *testing.T) {
	fam := lampFamily(t.TempDir(), false)
	want := collectWire(t, fam, "wt-lamp-1")
	got := collectWire(t, fam, "wt-lamp-1")
	last := len(got.HTTP) - 1
	got.HTTP[last].Status, got.HTTP[last].StateChange = 200, true
	if diffs := diffWire(t, fam, "wt-lamp-1", got, want); len(diffs) != 1 || !strings.Contains(diffs[0], "level a word") {
		t.Errorf("a quirk step answered 200 and changed state: %v", diffs)
	}
}

// A family whose goldens came from a profile that is gone cannot be recorded
// again, however it is asked for.
func TestTheRecorderRefusesAFrozenFamily(t *testing.T) {
	for _, fam := range []wireFamily{borealEdgeWire, borealBeamWire, borealCastWire} {
		if fam.Frozen == "" {
			t.Errorf("%s is recordable, and its goldens came from a deleted profile", fam.Name)
		}
	}
	if err := recordable(lampFamily("", false)); err != nil {
		t.Errorf("the test family is refused: %v", err)
	}
	if err := recordable(borealCastWire); err == nil || !strings.Contains(err.Error(), "frozen") {
		t.Errorf("a frozen family is recordable: %v", err)
	}
}
