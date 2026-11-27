//go:build labintegration

package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/internal/labcheck"
)

// A room's audio chain reads its room's meeting (#2774): on a corporate lab
// the divisible conference room's ceiling microphone, DSP and amplifier,
// real devices on a real daemon, each report what the lab calendar the plan's
// world implies says of that room, whether a meeting is held in it now. The
// wireless pack has no slot in a corporate standard, so its readings are the
// unit tests' (sim/room_audio_test.go).
func TestIntegrationCorporateRoomAudioReadsItsMeeting(t *testing.T) {
	ctx := context.Background()
	const systems = 20
	const system = "hq-divisible-conference-1"
	base, client := startCorporateLab(t, "roomaudio", systems)

	m, err := scenario.Shipped("corporate")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := scenario.ShippedCatalog()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := scenario.Resolve(m, cat, systems)
	if err != nil {
		t.Fatal(err)
	}
	w, err := scenario.SiteWorld(m, cat, plan, "hq")
	if err != nil {
		t.Fatal(err)
	}
	at, ok := w.PlaceSystem(system)
	if !ok {
		t.Fatalf("the plan's world has no system %s", system)
	}
	cal := w.Calendar()
	if cal == nil {
		t.Fatal("the corporate world holds no calendar")
	}
	room := at.CalRoom()

	get := func(product, path string, into any) error {
		body, err := labcheck.FireOnShowcaseBody(ctx, client, base, system, product, labcheck.Fire{Surface: "http-api", Method: "GET", Path: path})
		if err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(body), into); err != nil {
			return fmt.Errorf("GET %s on %s answered %q: %w", path, product, body, err)
		}
		return nil
	}

	// What the room may be said to hold across a read: the devices tick every
	// 15 to 60 seconds, so a reading may be of any instant of the last two
	// minutes. in is whether someone is in the room, gates how many table and
	// ceiling strips the DSP opens for them.
	possible := func(lo, hi time.Time) (in, gates map[int]bool) {
		in, gates = map[int]bool{}, map[int]bool{}
		for t := lo; !t.After(hi); t = t.Add(5 * time.Second) {
			e, ok := cal.Now(room, t)
			if !ok || e.Meeting.Actual == 0 {
				in[0], gates[0] = true, true
				continue
			}
			in[1], gates[min(4, (e.Meeting.Actual+1)/2)] = true, true
		}
		return in, gates
	}

	var arc struct {
		Mic struct {
			Active bool    `json:"active"`
			Level  float64 `json:"level_db"`
		} `json:"mic"`
	}
	var dsp struct {
		Active   int `json:"active_channels"`
		Channels []struct {
			Gate bool    `json:"gate"`
			Peak float64 `json:"peak_db"`
		} `json:"channels"`
	}
	var amp struct {
		Amp struct {
			Signal bool `json:"signal_present"`
		} `json:"amp"`
		Channels []struct {
			Out float64 `json:"output_db"`
		} `json:"channels"`
	}
	var last string
	deadline := time.Now().Add(6 * time.Minute)
	for {
		lo := time.Now().Add(-2 * time.Minute)
		errs := []error{get("lyra-arc-a2", "/api/status", &arc), get("polaris-dsp-16", "/api/channels", &dsp), get("polaris-amp-4100", "/api/status", &amp)}
		hi := time.Now()
		in, gates := possible(lo, hi)
		last = ""
		for _, err := range errs {
			if err != nil {
				last = err.Error()
			}
		}
		if last == "" {
			use := func(b bool) int {
				if b {
					return 1
				}
				return 0
			}
			switch {
			case !in[use(arc.Mic.Active)]:
				last = fmt.Sprintf("the ceiling mic is active=%v, the calendar says in use is one of %v", arc.Mic.Active, in)
			case !gates[dsp.Active]:
				last = fmt.Sprintf("the DSP opens %d gates, the calendar allows %v", dsp.Active, gates)
			case !in[use(amp.Amp.Signal)]:
				last = fmt.Sprintf("the amplifier signal_present=%v, the calendar says in use is one of %v", amp.Amp.Signal, in)
			}
		}
		if last == "" {
			open := 0
			for _, c := range dsp.Channels {
				if c.Gate {
					open++
				}
			}
			if open != dsp.Active && len(dsp.Channels) > 0 {
				last = fmt.Sprintf("the DSP reports %d active channels beside %d open gates", dsp.Active, open)
			}
		}
		// A device's declared defaults are an empty room too, so a reading is
		// only the calendar's once its behaviors have ticked: the readings that
		// carry a draw (a level, a peak, an output) have left their defaults.
		if last == "" {
			moved := func(vs []float64, def float64) bool {
				for _, v := range vs {
					if v != def {
						return true
					}
				}
				return false
			}
			var peaks, outs []float64
			for _, c := range dsp.Channels[:min(4, len(dsp.Channels))] {
				peaks = append(peaks, c.Peak)
			}
			for _, c := range amp.Channels {
				outs = append(outs, c.Out)
			}
			if arc.Mic.Level == -69 || !moved(peaks, -84) || !moved(outs, -90) {
				if !in[1] {
					last = "the devices have not ticked yet: their readings are still the declared defaults"
				}
			}
		}
		if last == "" {
			t.Logf("%s (room %s): ceiling mic active=%v level=%v, DSP gates open=%d, amplifier signal=%v; the calendar allowed in-use %v, gates %v",
				system, room.Key(), arc.Mic.Active, arc.Mic.Level, dsp.Active, amp.Amp.Signal, in, gates)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("after six minutes %s's audio does not read its room's meeting: %s", system, last)
		}
		time.Sleep(15 * time.Second)
	}
}
