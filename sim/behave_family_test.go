package sim

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// Parity of sim/behave with the families that use it (#2830; the YAML profiles
// it was first proven against are gone, #2837). Each test drives a family
// through the engine on a pinned clock, one
// tick at a time over the pinned room day (the calendar package's hand-checked
// day), and before every tick reads the typed state the device holds, runs the
// matching primitive from it with a twin of the device's own random source,
// and compares what the primitive owns with what the family wrote. A tick
// that drew a different number of times than the primitive desyncs the twin
// and fails the very next comparison.

var parityRoom = calendar.Room{Site: "hq", Building: "tower", Room: "tower-1-01"}

var parityDay = time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)

func parityKnobs() calendar.Knobs {
	return calendar.Knobs{BookingRate: 0.5, SlotMix: map[int]float64{15: 1, 30: 2, 60: 2}, NoShowRate: 0.2, EndEarlyRate: 0.2, EndLateRate: 0.15,
		Attendance: calendar.Range{Min: 2, Max: 6}, WalkInRate: 0.25, Hours: calendar.Hours{Start: 8, End: 14}, RecordingRate: 0.5}
}

// parityCal is the pinned room day with the two scripted problems.
func parityCal() *calendar.Calendar {
	k := parityKnobs()
	c := calendar.New(24, func(calendar.Room) (calendar.Knobs, bool) { return k, true })
	return c.WithProblems(func(calendar.Room) []calendar.ProblemSpec {
		pin := &calendar.Pin{Path: "status.device.power", Value: "off"}
		return []calendar.ProblemSpec{
			{Name: "morning", Cadence: "daily", At: "08:40", For: 30 * time.Minute, Scope: calendar.Scope{Role: "main-display"}, Impairment: calendar.Impairment{Pin: pin}},
			{Name: "afternoon", Cadence: "daily", At: "13:30", For: 90 * time.Minute, Scope: calendar.Scope{Role: "main-display"}, Impairment: calendar.Impairment{Pin: pin}},
		}
	})
}

func parityAt(hms string) time.Time {
	tm, err := time.Parse("15:04:05", hms)
	if err != nil {
		panic(err)
	}
	return parityDay.Add(time.Duration(tm.Hour())*time.Hour + time.Duration(tm.Minute())*time.Minute + time.Duration(tm.Second())*time.Second)
}

// parityRun is one device of a profile on the pinned clock beside the twin
// source the primitive draws from.
type parityRun struct {
	t   *testing.T
	d   *Device
	cal *calendar.Calendar
	at  *time.Time
	rng *behave.DeviceSource
	// off says the device's room has no calendar.
	off bool
}

func newParity(t *testing.T, profileName, id string, cal *calendar.Calendar, w *world.World, room calendar.Room) *parityRun {
	t.Helper()
	at := parityAt("07:00:00")
	d := calDevice(t, profileName, id, room, cal, w, &at)
	return &parityRun{t: t, d: d, cal: cal, at: &at, rng: behave.NewSource(id)}
}

// noCalendar is the same device with its calendar taken away: the fallback
// each primitive carries.
func (p *parityRun) noCalendar() *parityRun {
	p.d.calSrc = func() *calendar.Calendar { return nil }
	p.off = true
	return p
}

// room is the room's state as the device reads it.
func (p *parityRun) room() calendar.RoomState {
	if p.off {
		return calendar.RoomState{Phase: calendar.PhaseIdle, Occupancy: calendar.PhaseIdle}
	}
	return p.cal.StateAt(p.d.Room(), *p.at)
}

// walk calls fn at every step from..to on the pinned day, clock set.
func (p *parityRun) walk(from, to string, step time.Duration, fn func(hms string)) {
	p.t.Helper()
	for tm := parityAt(from); !tm.After(parityAt(to)); tm = tm.Add(step) {
		*p.at = tm
		fn(tm.Format("15:04:05"))
	}
}

func (p *parityRun) tick(name string) {
	p.t.Helper()
	if !p.d.TickBehavior(name) {
		p.t.Fatalf("no behavior %q", name)
	}
}

// set is an operator's write to a path, a list element by its index.
func (p *parityRun) set(path string, v any) {
	p.t.Helper()
	state := p.d.State()
	var cur any = state
	parts := splitPath(path)
	for _, part := range parts[:len(parts)-1] {
		switch c := cur.(type) {
		case map[string]any:
			cur = c[part]
		case []any:
			idx, err := strconv.Atoi(part)
			if err != nil {
				p.t.Fatalf("%s: %q is not an index", path, part)
			}
			cur = c[idx]
		}
	}
	last := parts[len(parts)-1]
	switch c := cur.(type) {
	case map[string]any:
		c[last] = v
	case []any:
		idx, err := strconv.Atoi(last)
		if err != nil {
			p.t.Fatalf("%s: %q is not an index", path, last)
		}
		c[idx] = v
	default:
		p.t.Fatalf("%s: cannot set under %T", path, cur)
	}
	p.d.SetState(state, 99)
}

// norm reads a value the way a comparison does: numbers as floats, the strings
// "true" and "false" as booleans.
func norm(v any) any {
	switch x := v.(type) {
	case int:
		return float64(x)
	case int32:
		return float64(x)
	case int64:
		return float64(x)
	case float32:
		return float64(x)
	case string:
		if b, err := strconv.ParseBool(x); err == nil && (x == "true" || x == "false") {
			return b
		}
		return x
	case nil:
		return ""
	}
	return v
}

func (p *parityRun) get(path string) any { return norm(p.d.ReadPath(path)) }
func (p *parityRun) f(path string) float64 {
	p.t.Helper()
	v, ok := p.get(path).(float64)
	if !ok {
		p.t.Fatalf("%s = %v (%T), want a number", path, p.d.ReadPath(path), p.d.ReadPath(path))
	}
	return v
}
func (p *parityRun) i(path string) int64 { p.t.Helper(); return int64(p.f(path)) }
func (p *parityRun) n(path string) int   { p.t.Helper(); return int(p.f(path)) }
func (p *parityRun) b(path string) bool {
	p.t.Helper()
	v, ok := p.get(path).(bool)
	if !ok {
		p.t.Fatalf("%s = %v (%T), want a bool", path, p.d.ReadPath(path), p.d.ReadPath(path))
	}
	return v
}
func (p *parityRun) s(path string) string {
	p.t.Helper()
	if v := p.d.ReadPath(path); v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

// same fails when the profile wrote something other than the primitive.
func (p *parityRun) same(hms, path string, want any) {
	p.t.Helper()
	if got, w := p.get(path), norm(want); got != w {
		p.t.Fatalf("%s: %s is %v (%T) in the family, %v (%T) from sim/behave", hms, path, got, got, w, w)
	}
}

// The engine and sim/behave seed a device's source from its id the same way.
func TestBehaveSourceDrawsTheEnginesStream(t *testing.T) {
	for _, id := range []string{"d-1", "cam-7", "rec-1"} {
		eng, mine := newDeviceRand(id), behave.NewSource(id)
		for i := 0; i < 200; i++ {
			if a, b := eng.uniform(-3, 5), mine.Uniform(-3, 5); a != b {
				t.Fatalf("%s uniform %d: engine %v, behave %v", id, i, a, b)
			}
			if a, b := eng.intn(2, 9), mine.IntN(2, 9); a != b {
				t.Fatalf("%s int %d: engine %d, behave %d", id, i, a, b)
			}
			if a, b := eng.uniform(0, 1) < 0.3, mine.Chance(0.3); a != b {
				t.Fatalf("%s chance %d: engine %v, behave %v", id, i, a, b)
			}
		}
	}
	for _, v := range []float64{0, 2.5, -2.5, 1234.567, 0.0123456, -36.4567} {
		for n := -1; n <= 3; n++ {
			if a, _ := roundN(n, v); a != behave.Round(n, v) {
				t.Fatalf("round %d %v: %v vs %v", n, v, a, behave.Round(n, v))
			}
			if a, _ := sigfigN(n, v); a != behave.SigFig(n, v) {
				t.Fatalf("sigfig %d %v: %v vs %v", n, v, a, behave.SigFig(n, v))
			}
		}
		if a, _ := clamp(-1, 2, v); a != behave.Clamp(-1, 2, v) {
			t.Fatalf("clamp %v", v)
		}
	}
}

// ---- power follows the room, input follows the meeting ----

func TestBehaveParityPowerAndInputFollowTheRoom(t *testing.T) {
	for _, tc := range []struct{ profile, input string }{
		{"boreal-edge-86", "status.display.input"}, {"boreal-beam-340", "status.projector.input"},
	} {
		for _, noCal := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/no-calendar=%v", tc.profile, noCal), func(t *testing.T) {
				p := newParity(t, tc.profile, "d-1", parityCal(), nil, parityRoom)
				if noCal {
					p.noCalendar()
				}
				var woke, slept int
				p.walk("07:00:00", "15:00:00", 30*time.Second, func(hms string) {
					switch hms {
					case "08:31:00": // an operator's change in the middle of a meeting holds
						p.set("status.device.power", "off")
						p.set(tc.input, "usbc")
					case "09:50:00":
						p.set(tc.input, "hdmi2")
					case "13:46:00": // and nothing in a no-show wakes it
						p.set("status.device.power", "standby")
					}
					prev := behave.PowerState{Power: p.s("status.device.power"), Busy: p.b("status.sim.busy"), Known: p.b("status.sim.known"),
						Awake: p.b("status.sim.awake"), Until: p.i("status.sim.until")}
					prevInput := p.s(tc.input)
					p.tick("room_meeting")
					next, edge := behave.PowerFollowsRoom(prev, p.room(), *p.at, behave.DefaultPower)
					p.same(hms, "status.device.power", next.Power)
					p.same(hms, "status.sim.busy", next.Busy)
					p.same(hms, "status.sim.known", next.Known)
					p.same(hms, "status.sim.awake", next.Awake)
					p.same(hms, "status.sim.until", next.Until)
					p.same(hms, tc.input, behave.InputFollowsMeeting(prevInput, edge, behave.DefaultPower))
					if edge {
						woke++
					}
					if next.Power != prev.Power && next.Power == "standby" {
						slept++
					}
				})
				if !noCal && (woke == 0 || slept == 0) {
					t.Fatalf("the day woke it %d times and slept it %d", woke, slept)
				}
				if noCal && woke != 0 {
					t.Fatalf("a device with no calendar woke %d times", woke)
				}
			})
		}
	}
}

// ---- follows the call, content sharing ----

// callOf reads the typed call state a conferencing device holds. The bar keeps
// the call's id in status.call.id and its bookkeeping in status.sim; the relay
// keeps the id in status.sim.call.
func callOf(p *parityRun, relay bool) behave.CallState {
	s := behave.CallState{
		Active: p.b("status.call.active"), ID: p.s("status.call.id"), Participants: p.n("status.call.participants"),
		Event: behave.CallEvent(p.s("status.sim.event")), Skip: p.i("status.sim.skip"), Want: p.s("status.sim.want"), WantN: p.n("status.sim.want_n"),
		Hungup: p.b("status.sim.hungup"), Dialed: p.b("status.sim.dialed"),
	}
	if relay {
		s.ID = p.s("status.sim.call")
		return s
	}
	s.IDPrev, s.ParticipantsPrev, s.Cal = p.s("status.sim.id_prev"), p.n("status.sim.participants_prev"), p.b("status.sim.cal")
	s.DialTicks, s.DialLength, s.CallSeq = p.n("status.sim.dial_ticks"), p.n("status.sim.dial_length"), p.n("status.sim.call_seq")
	s.Plan, s.Slot, s.Boundary, s.Booked = p.b("status.sim.plan"), p.n("status.sim.slot"), p.i("status.sim.boundary"), p.b("status.sim.booked")
	s.StartAt, s.EndAt, s.PlanAt = p.i("status.sim.start_at"), p.i("status.sim.end_at"), p.i("status.sim.plan_at")
	return s
}

func (p *parityRun) sameCall(hms string, c behave.CallState, relay bool) {
	p.t.Helper()
	p.same(hms, "status.call.active", c.Active)
	p.same(hms, "status.call.participants", c.Participants)
	p.same(hms, "status.sim.event", string(c.Event))
	p.same(hms, "status.sim.skip", c.Skip)
	p.same(hms, "status.sim.want", c.Want)
	p.same(hms, "status.sim.want_n", c.WantN)
	p.same(hms, "status.sim.hungup", c.Hungup)
	p.same(hms, "status.sim.dialed", c.Dialed)
	if relay {
		p.same(hms, "status.sim.call", c.ID)
		return
	}
	p.same(hms, "status.call.id", c.ID)
	p.same(hms, "status.sim.id_prev", c.IDPrev)
	p.same(hms, "status.sim.participants_prev", c.ParticipantsPrev)
	p.same(hms, "status.sim.cal", c.Cal)
	p.same(hms, "status.sim.dial_ticks", c.DialTicks)
	p.same(hms, "status.sim.call_seq", c.CallSeq)
	p.same(hms, "status.sim.plan", c.Plan)
	p.same(hms, "status.sim.slot", c.Slot)
	p.same(hms, "status.sim.boundary", c.Boundary)
	p.same(hms, "status.sim.booked", c.Booked)
	p.same(hms, "status.sim.start_at", c.StartAt)
	p.same(hms, "status.sim.end_at", c.EndAt)
	p.same(hms, "status.sim.plan_at", c.PlanAt)
}

func barCallParams() behave.CallParams {
	p := behave.DefaultCall
	p.Standalone, p.DialEnds = &behave.DefaultStandalone, true
	return p
}

func relayCallParams() behave.CallParams {
	p := behave.DefaultCall
	p.ClearIDOnEnd = true
	return p
}

func TestBehaveParityVideoBarCallAndSharing(t *testing.T) {
	for _, noCal := range []bool{false, true} {
		t.Run(fmt.Sprintf("no-calendar=%v", noCal), func(t *testing.T) {
			p := newParity(t, "kestrel-vroom", "bar-1", parityCal(), nil, parityRoom)
			from, to := "07:00:00", "15:00:00"
			if noCal {
				p.noCalendar()
				to = "13:00:00"
			}
			events := map[behave.CallEvent]int{}
			var shared int
			p.walk(from, to, time.Minute, func(hms string) {
				switch hms {
				case "08:42:00": // an operator stops sharing inside the calendar's stretch: it holds
					p.set("status.presentation.local_active", false)
				case "10:20:00": // a hang-up: the meeting it ended is not joined again
					p.set("status.call.active", false)
					p.set("status.call.participants", 0)
					p.set("status.sim.hungup", true)
				case "12:00:00": // a dialed call overtakes the walk-in that follows
					p.set("status.call.active", true)
					p.set("status.call.id", "c-dial")
					p.set("status.call.participants", 3)
					p.set("status.sim.dialed", true)
					p.set("status.sim.dial_length", 4)
				}
				prev, share := callOf(p, false), behave.ShareState{Sharing: p.b("status.presentation.local_active"), Prev: p.b("status.sim.share_prev")}
				p.tick("call_cycle")
				rs := p.room()
				next := behave.FollowsCall(prev, rs, *p.at, barCallParams(), p.rng)
				share = behave.ContentSharing(share, rs, behave.CallSignals{Active: next.Active, Dialed: next.Dialed, Ended: next.Event == behave.EventEnd}, behave.DefaultShare, p.rng)
				next = behave.PlanOwnSchedule(next, *p.at, behave.DefaultStandalone, p.rng)
				p.sameCall(hms, next, false)
				p.same(hms, "status.presentation.local_active", share.Sharing)
				p.same(hms, "status.sim.share_prev", share.Prev)
				events[next.Event]++
				if share.Sharing {
					shared++
				}
			})
			if events[behave.EventStart] == 0 || events[behave.EventEnd] == 0 {
				t.Fatalf("the day held %v: want calls that start and end", events)
			}
			if !noCal && (events[behave.EventJoin] == 0 || events[behave.EventLeave] == 0 || shared == 0) {
				t.Fatalf("the day held %v and %d minutes of sharing: want joins, leaves and a share", events, shared)
			}
		})
	}
}

func TestBehaveParityRelayCallAndSharing(t *testing.T) {
	sources := []string{"hdmi1", "hdmi2", "usbc", "wireless"}
	for _, noCal := range []bool{false, true} {
		t.Run(fmt.Sprintf("no-calendar=%v", noCal), func(t *testing.T) {
			p := newParity(t, "kestrel-relay-c1", "relay-1", parityCal(), nil, parityRoom)
			if noCal {
				p.noCalendar()
			}
			events := map[behave.CallEvent]int{}
			var shared int
			p.walk("07:00:00", "15:00:00", time.Minute, func(hms string) {
				switch hms {
				case "08:42:00":
					p.set("status.content.sharing", false)
				case "10:20:00":
					p.set("status.call.active", false)
					p.set("status.call.participants", 0)
					p.set("status.sim.hungup", true)
				case "12:00:00":
					p.set("status.call.active", true)
					p.set("status.sim.call", "c-dial")
					p.set("status.call.participants", 3)
					p.set("status.sim.dialed", true)
				case "12:30:00": // the dialed call is hung up by the operator
					p.set("status.sim.dialed", false)
					p.set("status.call.active", false)
				}
				prev, share, src := callOf(p, true), behave.ShareState{Sharing: p.b("status.content.sharing"), Prev: p.b("status.sim.share_prev")}, p.s("status.content.source")
				p.tick("call_cycle")
				rs := p.room()
				next := behave.FollowsCall(prev, rs, *p.at, relayCallParams(), p.rng)
				hold := behave.ShareParams{ToggleChance: behave.DefaultShare.ToggleChance, HoldIdle: true}
				share = behave.ContentSharing(share, rs, behave.CallSignals{Active: next.Active, Dialed: next.Dialed, Ended: next.Event == behave.EventEnd}, hold, p.rng)
				p.sameCall(hms, next, true)
				p.same(hms, "status.content.sharing", share.Sharing)
				p.same(hms, "status.sim.share_prev", share.Prev)
				p.same(hms, "status.content.source", behave.ContentSource(src, share.Sharing, sources, p.rng))
				events[next.Event]++
				if share.Sharing {
					shared++
				}
			})
			if noCal {
				if events[behave.EventStart] != 0 {
					t.Fatalf("a relay with no calendar called: %v", events)
				}
				return
			}
			if events[behave.EventStart] == 0 || events[behave.EventEnd] == 0 || shared == 0 {
				t.Fatalf("the day held %v and %d minutes of sharing", events, shared)
			}
		})
	}
}

// ---- occupancy, climate, battery ----

func TestBehaveParityPeopleCount(t *testing.T) {
	type counter struct {
		profile, behavior, count string
		params                   behave.OccupancyParams
		callPeers                func(p *parityRun) int
	}
	for _, tc := range []counter{
		{"tessellate-census-c1", "occupancy_drift", "status.occupancy.people_count", behave.DefaultOccupancy, func(*parityRun) int { return 0 }},
		{"kestrel-vroom", "people_count", "status.video.people_count", behave.CallCount, func(p *parityRun) int {
			if p.b("status.call.active") {
				return p.n("status.call.participants")
			}
			return 0
		}},
	} {
		for _, noCal := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/no-calendar=%v", tc.profile, noCal), func(t *testing.T) {
				p := newParity(t, tc.profile, "cnt-1", parityCal(), nil, parityRoom)
				if noCal {
					p.noCalendar()
				}
				seen := map[int]int{}
				p.walk("07:00:00", "15:00:00", 30*time.Second, func(hms string) {
					if tc.profile == "kestrel-vroom" && hms == "10:00:00" { // a call the bar is in itself
						p.set("status.call.active", true)
						p.set("status.call.participants", 4)
					}
					prev := behave.OccupancyState{Count: p.n(tc.count)}
					peers := tc.callPeers(p)
					p.tick(tc.behavior)
					got := behave.PeopleCount(prev, p.room(), peers, tc.params, p.rng)
					p.same(hms, tc.count, got.Count)
					if tc.profile == "tessellate-census-c1" {
						p.same(hms, "status.occupancy.people_count_prev", got.Prev)
						p.same(hms, "status.occupancy.last_event", got.LastEvent)
						p.same(hms, "status.occupancy.vacant", got.Vacant)
					}
					seen[got.Count]++
				})
				if len(seen) < 2 {
					t.Fatalf("the count never moved: %v", seen)
				}
			})
		}
	}
}

func TestBehaveParityClimateAndRoomPower(t *testing.T) {
	for _, noCal := range []bool{false, true} {
		t.Run(fmt.Sprintf("no-calendar=%v", noCal), func(t *testing.T) {
			p := newParity(t, "newtron-core-3", "core-1", parityCal(), nil, parityRoom)
			if noCal {
				p.noCalendar()
			}
			var starts, ends int
			var warmest float64
			p.walk("07:00:00", "15:00:00", 30*time.Second, func(hms string) {
				ctl := behave.ControlState{RoomPower: p.s("status.control.room_power"), Macro: p.s("status.control.macro"), MacroAt: p.i("status.control.macro_at")}
				clim := behave.ClimateState{TemperatureC: p.f("status.room.temperature_c"), ClimateC: p.f("status.room.climate_c"),
					SetpointC: p.f("status.room.setpoint_c"), LoadCPerPerson: p.f("status.room.load_c_per_person")}
				p.tick("room_meeting")
				p.tick("room_climate")
				rs := p.room()
				ctl = behave.RoomPower(ctl, rs, *p.at, behave.DefaultMacros)
				clim = behave.Climate(clim, rs, behave.DefaultClimate, p.rng)
				p.same(hms, "status.control.room_power", ctl.RoomPower)
				p.same(hms, "status.control.macro", ctl.Macro)
				p.same(hms, "status.control.macro_at", ctl.MacroAt)
				p.same(hms, "status.room.temperature_c", clim.TemperatureC)
				p.same(hms, "status.room.climate_c", clim.ClimateC)
				warmest = max(warmest, clim.ClimateC)
				if ctl.Macro == "start-meeting" && ctl.MacroAt == p.at.Unix() {
					starts++
				}
				if ctl.Macro == "end-meeting" && ctl.MacroAt == p.at.Unix() {
					ends++
				}
			})
			if noCal && (starts != 0 || warmest != 21.5) {
				t.Fatalf("no calendar: %d macros, warmest %v", starts, warmest)
			}
			if !noCal && (starts != 5 || ends != 5 || warmest <= 21.5) {
				t.Fatalf("%d starts, %d ends, warmest %v", starts, ends, warmest)
			}
		})
	}
}

func TestBehaveParityBatteryIsTheCalendarsOver48Hours(t *testing.T) {
	for _, noCal := range []bool{false, true} {
		t.Run(fmt.Sprintf("no-calendar=%v", noCal), func(t *testing.T) {
			// the day after a pinned run: the window reaches back over a whole earlier day
			p := newParity(t, "lyra-hx-1", "mic-1", parityCal(), nil, parityRoom)
			if noCal {
				p.noCalendar()
			}
			lo, hi := 101.0, -1.0
			p.walk("07:00:00", "23:50:00", 10*time.Minute, func(hms string) {
				prev := p.f("status.mic.battery_percent")
				p.tick("battery_drain")
				var spans []behave.Span
				if !noCal {
					spans = behave.SpansBetween(p.cal, parityRoom, p.at.Add(-behave.DefaultBattery.Window), *p.at)
				}
				got := behave.Battery(prev, p.room().HasCalendar, spans, *p.at, behave.DefaultBattery)
				p.same(hms, "status.mic.battery_percent", got)
				lo, hi = min(lo, got), max(hi, got)
			})
			if !noCal && (hi-lo < 5) {
				t.Fatalf("the pack's charge stayed within %v..%v over a day of meetings", lo, hi)
			}
		})
	}
}

// ---- mic activity and mute, mixer gates, amplifier signal and levels, outlet load ----

func TestBehaveParityMicActivityMuteAndLevel(t *testing.T) {
	for _, tc := range []struct{ profile, active string }{{"lyra-arc-a2", "status.mic.active"}, {"lyra-hx-1", "status.mic.transmitting"}} {
		for _, noCal := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/no-calendar=%v", tc.profile, noCal), func(t *testing.T) {
				p := newParity(t, tc.profile, "mic-1", parityCal(), nil, parityRoom)
				if noCal {
					p.noCalendar()
				}
				var muted, active int
				p.walk("07:00:00", "15:00:00", 15*time.Second, func(hms string) {
					if hms == "10:15:00" { // an operator mutes the room
						p.set("status.mic.mute", true)
					}
					prev := behave.MicState{Active: p.b(tc.active), Mute: p.b("status.mic.mute"), LevelDB: p.f("status.mic.level_db")}
					p.tick("call_state")
					rs := p.room()
					got := behave.MicActivity(prev, rs, behave.DefaultMic, p.rng)
					p.same(hms, tc.active, got.Active)
					p.same(hms, "status.mic.mute", got.Mute)
					p.tick("level_drift")
					got.LevelDB = behave.MicLevel(got, rs, behave.DefaultMic, p.rng)
					p.same(hms, "status.mic.level_db", got.LevelDB)
					if got.Mute {
						muted++
					}
					if got.Active {
						active++
					}
				})
				if noCal && active != 0 || !noCal && (active == 0 || muted == 0) {
					t.Fatalf("%d active ticks and %d muted", active, muted)
				}
			})
		}
	}
}

func TestBehaveParityMixerGates(t *testing.T) {
	for _, noCal := range []bool{false, true} {
		t.Run(fmt.Sprintf("no-calendar=%v", noCal), func(t *testing.T) {
			p := newParity(t, "polaris-dsp-16", "dsp-1", parityCal(), nil, parityRoom)
			if noCal {
				p.noCalendar()
			}
			var open int
			p.walk("07:00:00", "15:00:00", 15*time.Second, func(hms string) {
				if hms == "10:00:00" { // an operator mutes a strip: it stays shut
					p.set("status.channels.1.mute", true)
				}
				var prev behave.GateState
				for i := 0; i < len(behave.DefaultGates.Opens); i++ {
					path := fmt.Sprintf("status.channels.%d.", i)
					prev.Strips = append(prev.Strips, behave.Strip{Muted: p.b(path + "mute"), Gate: p.b(path + "gate"), PeakDB: p.f(path + "peak_db")})
				}
				prev.Active = p.n("status.dsp.active_channels")
				p.tick("room_signal")
				got := behave.Gates(prev, p.room(), behave.DefaultGates, p.rng)
				p.same(hms, "status.dsp.active_channels", got.Active)
				for i, s := range got.Strips {
					path := fmt.Sprintf("status.channels.%d.", i)
					p.same(hms, path+"gate", s.Gate)
					p.same(hms, path+"peak_db", s.PeakDB)
				}
				open += got.Active
			})
			if !noCal && open == 0 {
				t.Fatalf("%d gate-ticks open over a day of meetings", open)
			}
		})
	}
}

func TestBehaveParityAmplifierSignalTemperatureAndLevels(t *testing.T) {
	wake := []float64{-24, -24, -27, -27}
	for _, noCal := range []bool{false, true} {
		t.Run(fmt.Sprintf("no-calendar=%v", noCal), func(t *testing.T) {
			p := newParity(t, "polaris-amp-4100", "amp-1", parityCal(), nil, parityRoom)
			if noCal {
				p.noCalendar()
			}
			var live int
			p.walk("07:00:00", "15:00:00", 30*time.Second, func(hms string) {
				switch hms { // an operator powers it down for a while mid-day
				case "10:30:00":
					p.set("status.amp.power", "standby")
				case "11:30:00":
					p.set("status.amp.power", "on")
				}
				on := p.s("status.amp.power") == "on"
				prevSignal, prevTemp := p.b("status.amp.signal_present"), p.f("status.amp.temperature")
				var prevOut [4]float64
				for i := range prevOut {
					prevOut[i] = p.f(fmt.Sprintf("status.channels.%d.output_db", i))
				}
				p.tick("output_drift")
				rs := p.room()
				p.same(hms, "status.amp.signal_present", behave.AmpSignalPresent(prevSignal, rs, on))
				for i := range prevOut {
					p.same(hms, fmt.Sprintf("status.channels.%d.output_db", i), behave.AmpOutputLevel(prevOut[i], rs, on, wake[i], behave.DefaultAmp, p.rng))
				}
				p.tick("temperature_drift")
				p.same(hms, "status.amp.temperature", behave.AmpTemperature(prevTemp, rs, on, behave.DefaultAmp, p.rng))
				if behave.AmpSignalPresent(false, rs, on) {
					live++
				}
			})
			if noCal && live != 0 || !noCal && live == 0 {
				t.Fatalf("%d live ticks", live)
			}
		})
	}
}

func TestBehaveParityPDUOutletLoad(t *testing.T) {
	outlets := []behave.OutletParams{
		{Idle: 150, InUse: 700, InCall: 950, Walk: behave.DefaultOutletWalk},
		{Idle: 180, InUse: 300, Walk: behave.DefaultOutletWalk},
		{Idle: 120, InUse: 500, InCall: 800, Walk: behave.DefaultOutletWalk},
		{CalendarOff: true, Walk: behave.DefaultOutletWalk},
	}
	for _, noCal := range []bool{false, true} {
		t.Run(fmt.Sprintf("no-calendar=%v", noCal), func(t *testing.T) {
			p := newParity(t, "ferrite-pdu-4c", "pdu-1", parityCal(), nil, parityRoom)
			if noCal {
				p.noCalendar()
			}
			levels := map[int]bool{}
			p.walk("07:00:00", "15:00:00", 30*time.Second, func(hms string) {
				switch hms { // an operator switches the video outlet off for a while
				case "10:40:00":
					p.set("status.outlets.0.state", "off")
				case "11:10:00":
					p.set("status.outlets.0.state", "on")
				}
				var prev, on [4]any
				for i := range outlets {
					prev[i] = p.i(fmt.Sprintf("status.outlets.%d.current_ma", i))
					on[i] = p.s(fmt.Sprintf("status.outlets.%d.state", i)) == "on"
				}
				p.tick("load_drift")
				rs := p.room()
				for i, o := range outlets {
					got := behave.OutletCurrent(int(prev[i].(int64)), on[i].(bool), rs, o, p.rng)
					p.same(hms, fmt.Sprintf("status.outlets.%d.current_ma", i), got)
					levels[got] = true
				}
			})
			if len(levels) < 4 {
				t.Fatalf("only %d distinct draws over the day", len(levels))
			}
		})
	}
}

// ---- recordings, building hours, bookings ----

// parityWorld is a world whose rooms are the given covers, each built to the
// pinned meeting standard, and the one service that speaks for them.
func parityWorld(service, profileName string, covers []world.Cover) (*world.World, *calendar.Calendar) {
	w := &world.World{Version: world.Version, CalendarSeed: 24, Meetings: map[string]calendar.Knobs{"conf": parityKnobs()}}
	var rooms []world.Room
	for _, c := range covers {
		rooms = append(rooms, world.Room{Name: c.Room, Building: c.Building, Standard: "conf", System: c.System, Label: "Room " + c.Room})
	}
	w.Sites = []world.Site{{Name: "hq", Rooms: rooms}}
	w.Services = []world.Service{{Name: service, Profile: profileName, Covers: covers}}
	return w, w.Calendar()
}

func parityCovers() []world.Cover {
	return []world.Cover{
		{System: "sys-a", Site: "hq", Building: "tower", Room: "tower-1-01", Label: "Room tower-1-01"},
		{System: "sys-b", Site: "hq", Building: "tower", Room: "tower-1-02", Label: "Room tower-1-02"},
		{System: "sys-c", Site: "hq", Building: "tower", Room: "tower-2-07", Label: "Room tower-2-07"},
	}
}

func TestBehaveParityRecordings(t *testing.T) {
	covers := parityCovers()
	w, wcal := parityWorld("rec-1", "recording-service", covers)
	for _, tc := range []struct {
		name  string
		cal   *calendar.Calendar
		w     *world.World
		room  calendar.Room
		names []string // the records' names
	}{
		{"covered rooms", wcal, w, calendar.Room{}, []string{"sys-a", "sys-b", "sys-c"}},
		{"its own room", parityCal(), nil, parityRoom, []string{parityRoom.Room}},
		{"no calendar", nil, nil, calendar.Room{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newParity(t, "recording-service", "rec-1", tc.cal, tc.w, tc.room)
			var rooms []behave.RecordingRoom
			var inProgress, queued, drifted int
			p.walk("07:00:00", "15:30:00", time.Minute, func(hms string) {
				prev := behave.RecordingCounts{InProgress: p.n("status.recordings.in_progress"), QueueDepth: p.n("status.recordings.queue_depth")}
				p.tick("room_recordings")
				rooms = rooms[:0]
				var own calendar.RoomState
				switch {
				case tc.w != nil:
					for i, c := range covers {
						rooms = append(rooms, behave.RecordingRoom{Name: tc.names[i], State: tc.cal.StateAt(calendar.Room{Site: c.Site, Building: c.Building, Room: c.Room}, *p.at)})
					}
				case tc.cal != nil:
					own = tc.cal.StateAt(tc.room, *p.at)
				}
				got := behave.Recordings(prev, rooms, own, behave.DefaultRecordings, p.rng)
				p.same(hms, "status.recordings.in_progress", got.InProgress)
				p.same(hms, "status.recordings.queue_depth", got.QueueDepth)
				for _, r := range behave.PerRoom(append([]behave.RecordingRoom(nil), recordingRoomsOf(tc.w != nil, rooms, tc.cal != nil, tc.room.Room, own)...)) {
					p.same(hms, "status.recordings.rooms."+r.Name+".in_progress", r.InProgress)
					p.same(hms, "status.recordings.rooms."+r.Name+".queue_depth", r.QueueDepth)
				}
				inProgress, queued = max(inProgress, got.InProgress), max(queued, got.QueueDepth)
				if tc.cal == nil {
					drifted += got.InProgress + got.QueueDepth
				}
			})
			if tc.cal != nil && (inProgress == 0 || queued == 0) {
				t.Fatalf("the day recorded %d at most and queued %d", inProgress, queued)
			}
			if tc.cal == nil && drifted == 0 {
				t.Fatal("the counters never drifted")
			}
		})
	}
}

// recordingRoomsOf is the rooms a service lists: those it covers, else its own.
func recordingRoomsOf(covered bool, rooms []behave.RecordingRoom, hasOwn bool, ownName string, own calendar.RoomState) []behave.RecordingRoom {
	if covered {
		return rooms
	}
	if hasOwn {
		return []behave.RecordingRoom{{Name: ownName, State: own}}
	}
	return nil
}

func TestBehaveParityBuildingDayAndSpan(t *testing.T) {
	for _, noCal := range []bool{false, true} {
		t.Run(fmt.Sprintf("player/no-calendar=%v", noCal), func(t *testing.T) {
			p := newParity(t, "boreal-cast-m1", "sign-1", parityCal(), nil, parityRoom)
			if noCal {
				p.noCalendar()
			}
			var opened, closed int
			step := 0
			p.walk("00:00:00", "23:59:00", time.Minute, func(hms string) {
				step++
				if step%3 == 0 {
					p.tick("playlist_advance") // the playlist's own advance, which the day leaves alone
				}
				switch hms {
				case "03:00:00": // an operator plays a stopped sign: it holds until opening
					p.set("status.playback.state", "playing")
				case "10:00:00": // and pauses an open one: it holds until closing
					p.set("status.playback.state", "paused")
				}
				prev := behave.SignState{Open: p.b("status.sim.open"), Playback: p.s("status.playback.state"), Index: p.n("status.playback.index")}
				p.tick("building_day")
				span, known := behave.BuildingSpan(nil, p.calOrNil(), parityRoom)
				got := behave.BuildingDay(prev, behave.ReadBuildingHours(span, known, *p.at), behave.DefaultSign)
				p.same(hms, "status.playback.state", got.Playback)
				p.same(hms, "status.playback.index", got.Index)
				p.same(hms, "status.playback.content", got.Content)
				p.same(hms, "status.sim.open", got.Open)
				if got.Open && !prev.Open {
					opened++
				}
				if !got.Open && prev.Open {
					closed++
				}
			})
			if !noCal && (opened == 0 || closed == 0) {
				t.Fatalf("the sign opened %d and closed %d times", opened, closed)
			}
		})
	}
}

// calOrNil is the calendar the device reads.
func (p *parityRun) calOrNil() *calendar.Calendar {
	if p.off {
		return nil
	}
	return p.cal
}

// BuildingSpan is the profile engine's buildingSpan over a world of several
// buildings, standards and a signage room that declares none.
func TestBehaveParityBuildingSpanOverAWorld(t *testing.T) {
	knobs := func(s, e int) calendar.Knobs {
		k := parityKnobs()
		k.Hours = calendar.Hours{Start: s, End: e}
		return k
	}
	w := &world.World{Version: world.Version, Meetings: map[string]calendar.Knobs{"short": knobs(9, 12), "long": knobs(6, 18), "all": knobs(0, 0)},
		Sites: []world.Site{
			{Name: "hq", Rooms: []world.Room{{Name: "r1", Building: "tower", Standard: "short"}, {Name: "r2", Building: "tower", Standard: "long"},
				{Name: "r3", Building: "annex", Standard: "all"}, {Name: "sign", Building: "tower", Standard: "signage"}}},
			{Name: "lab", Rooms: []world.Room{{Name: "x", Building: "tower", Standard: "short"}}}}}
	cal := parityCal()
	for _, room := range []calendar.Room{
		{Site: "hq", Building: "tower", Room: "sign"}, {Site: "hq", Building: "annex", Room: "r3"}, {Site: "lab", Building: "tower", Room: "x"},
		{Site: "hq", Building: "ghost", Room: "g"}, {Site: "nowhere", Building: "tower", Room: "g"}, parityRoom,
	} {
		for _, c := range []*calendar.Calendar{nil, cal} {
			for _, ww := range []*world.World{nil, w} {
				wantH, wantOK := buildingSpan(ww, c, room)
				gotH, gotOK := behave.BuildingSpan(ww, c, room)
				if wantH != gotH || wantOK != gotOK {
					t.Errorf("%+v world %v cal %v: behave %+v %v, engine %+v %v", room, ww != nil, c != nil, gotH, gotOK, wantH, wantOK)
				}
			}
		}
	}
}

// BookingsAt is RoomBookingsAt, read minute by minute over three days.
func TestBehaveParityBookingsAtIsRoomBookingsAt(t *testing.T) {
	cal := parityCal()
	for tm := parityDay.Add(-24 * time.Hour); tm.Before(parityDay.Add(48 * time.Hour)); tm = tm.Add(time.Minute) {
		want, got := RoomBookingsAt(cal, parityRoom, tm), behave.BookingsAt(cal, parityRoom, tm)
		for k, v := range map[string]any{"today": got.Today, "remaining": got.Remaining, "booked": got.Booked, "state": got.State} {
			if norm(want[k]) != norm(v) {
				t.Fatalf("%s: %s is %v in the engine, %v from sim/behave", tm.Format("Jan 2 15:04"), k, want[k], v)
			}
		}
		for k, b := range map[string]*behave.Booking{"now": got.Now, "next": got.Next} {
			m, _ := want[k].(map[string]any)
			if (m == nil) != (b == nil) {
				t.Fatalf("%s: %s is %v in the engine, %v from sim/behave", tm.Format("Jan 2 15:04"), k, want[k], b)
			}
			if b != nil && (norm(m["start"]) != norm(b.Start) || norm(m["end"]) != norm(b.End) || norm(m["slot_start"]) != norm(b.SlotStart) ||
				norm(m["slot_end"]) != norm(b.SlotEnd) || m["organizer"] != b.Organizer) {
				t.Fatalf("%s: %s is %v in the engine, %+v from sim/behave", tm.Format("Jan 2 15:04"), k, m, *b)
			}
		}
	}
	// a room with no calendar reads free and empty either way
	want, got := RoomBookingsAt(nil, parityRoom, parityAt("10:00:00")), behave.BookingsAt(nil, parityRoom, parityAt("10:00:00"))
	if want["state"] != got.State || want["today"] != got.Today || want["now"] != nil || got.Now != nil {
		t.Fatalf("no calendar: %v vs %+v", want, got)
	}
}

func TestBehaveParityRoomBookingPanel(t *testing.T) {
	p := newParity(t, "newtron-slate-5", "slate-1", parityCal(), nil, parityRoom)
	var changes int
	p.walk("07:00:00", "15:00:00", time.Minute, func(hms string) {
		if hms == "09:50:00" { // an operator books the free room from the panel: it holds
			p.set("status.room.booking", "booked")
		}
		prev := behave.RoomBookingState{Booking: p.s("status.room.booking"), CalBooking: p.s("status.room.cal_booking")}
		p.tick("room_bookings")
		got := behave.RoomBooking(prev, behave.BookingsAt(p.cal, parityRoom, *p.at))
		p.same(hms, "status.room.booking", got.Booking)
		p.same(hms, "status.room.cal_booking", got.CalBooking)
		p.same(hms, "status.room.current_meeting", got.Current)
		p.same(hms, "status.room.next_meeting", got.Next)
		if got.Booking != prev.Booking {
			changes++
		}
	})
	if changes < 4 {
		t.Fatalf("the panel changed %d times over a day of bookings", changes)
	}
}

func TestBehaveParitySchedulingServiceTotalsAndRooms(t *testing.T) {
	covers := parityCovers()
	w, cal := parityWorld("sched-1", "scheduling-service", covers)
	p := newParity(t, "scheduling-service", "sched-1", cal, w, calendar.Room{})
	var busy int
	p.walk("06:00:00", "15:00:00", time.Minute, func(hms string) {
		prevToday := p.n("status.bookings.today")
		p.tick("bookings_sync")
		var rooms []behave.Bookings
		for _, c := range covers {
			rooms = append(rooms, behave.BookingsAt(cal, calendar.Room{Site: c.Site, Building: c.Building, Room: c.Room}, *p.at))
		}
		sum := behave.SumBookings(prevToday, rooms)
		p.same(hms, "status.bookings.today", sum.Today)
		p.same(hms, "status.bookings.remaining", sum.Remaining)
		p.same(hms, "status.bookings.in_progress", sum.InProgress)
		for i, c := range covers {
			s := rooms[i].Summary()
			path := "status.rooms." + c.System + "."
			p.same(hms, path+"state", s.State)
			p.same(hms, path+"today", s.Today)
			p.same(hms, path+"remaining", s.Remaining)
			p.same(hms, path+"current", s.Current)
			p.same(hms, path+"next_start", s.NextStart)
		}
		busy = max(busy, sum.InProgress)
	})
	if busy == 0 {
		t.Fatal("no room was ever booked")
	}
	// a service with nothing to serve keeps its day and reports nothing running
	p = newParity(t, "scheduling-service", "sched-2", nil, nil, calendar.Room{})
	*p.at = parityAt("10:00:00")
	prevToday := p.n("status.bookings.today")
	p.tick("bookings_sync")
	sum := behave.SumBookings(prevToday, nil)
	p.same("10:00:00", "status.bookings.today", sum.Today)
	p.same("10:00:00", "status.bookings.remaining", sum.Remaining)
	p.same("10:00:00", "status.bookings.in_progress", sum.InProgress)
}
