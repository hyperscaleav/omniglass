//go:build linux

package sim

import (
	"fmt"
	"strconv"
	"time"
)

// The Ferrite PDU 4C's wire script (#2836): every route valid and refused, the
// SNMP arc, the two drifting readings, and the rack's pinned day with and
// without the lab calendar.

const ferriteProduct = "ferrite-pdu-4c"

func ferriteScript() []wireStep {
	return []wireStep{
		wireGet("the status document", "/pdu.json"),
		{Name: "the document with no login", Method: "GET", Path: "/pdu.json", NoKey: true},
		{Name: "an unknown path", Method: "GET", Path: "/nope"},
		{Name: "the root", Method: "GET", Path: "/"},
		{Name: "the wrong method on the document", Method: "POST", Path: "/pdu.json", Body: `{}`},
		{Name: "the wrong method on the switch", Method: "GET", Path: "/api/outlets/state"},
		{Name: "a head request on the document", Method: "HEAD", Path: "/pdu.json"},
		wirePost("outlet 2 off", "/api/outlets/state", `{"outlet": 2, "state": "off"}`),
		wireGet("the document with an outlet off", "/pdu.json"),
		wirePost("outlet 4 on", "/api/outlets/state", `{"outlet": 4, "state": "on"}`),
		wirePost("outlet 2 on", "/api/outlets/state", `{"outlet": 2, "state": "on"}`),
		wirePost("outlet named as text", "/api/outlets/state", `{"outlet": "3", "state": "off"}`),
		wirePost("outlet named as a whole float", "/api/outlets/state", `{"outlet": 1.0, "state": "off"}`),
		wirePost("outlet 1 on again", "/api/outlets/state", `{"outlet": 1, "state": "on"}`),
		wirePost("an outlet the PDU does not have", "/api/outlets/state", `{"outlet": 9, "state": "on"}`),
		wirePost("outlet zero", "/api/outlets/state", `{"outlet": 0, "state": "on"}`),
		wirePost("a state outside on and off", "/api/outlets/state", `{"outlet": 1, "state": "bogus"}`),
		wirePost("a state of the wrong case", "/api/outlets/state", `{"outlet": 1, "state": "ON"}`),
		wirePost("no state", "/api/outlets/state", `{"outlet": 1}`),
		wirePost("no outlet", "/api/outlets/state", `{"state": "off"}`),
		wirePost("an empty object", "/api/outlets/state", `{}`),
		wirePost("no body", "/api/outlets/state", ``),
		wirePost("outlet 3 off", "/api/outlets/state", `{"outlet": 3, "state": "off"}`),
		wireGet("the document at the end", "/pdu.json"),
		// The requests the profile answered with an accident of its templates
		// come last, so the state they left does not carry into the rest.
		{Name: "an array body", Method: "POST", Path: "/api/outlets/state", Body: `[1, 2]`, Snapshot: true, Quirk: "the mutation pipeline read the array as a body with every field missing"},
		{Name: "a text body", Method: "POST", Path: "/api/outlets/state", Body: `outlet 1 off`, Snapshot: true, Quirk: "the mutation pipeline read the text as a body with every field missing"},
	}
}

func ferriteWrites() []wireStep {
	return []wireStep{
		wirePost("outlet 2 off", "/api/outlets/state", `{"outlet": 2, "state": "off"}`),
		wirePost("outlet 4 on", "/api/outlets/state", `{"outlet": 4, "state": "on"}`),
	}
}

func ferriteOIDs() []string {
	oids := []string{"1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.3.0",
		"1.3.6.1.4.1.55560.1.1.0", "1.3.6.1.4.1.55560.1.2.0", "1.3.6.1.4.1.55560.1.3.0",
		"1.3.6.1.4.1.55560.5.1.0", "1.3.6.1.4.1.55560.5.2.0", "1.3.6.1.4.1.55560.9.9.0"}
	for col := 1; col <= 3; col++ {
		for n := 1; n <= 4; n++ {
			oids = append(oids, fmt.Sprintf("1.3.6.1.4.1.55560.2.1.%d.%d", col, n))
		}
	}
	return append(oids, "1.3.6.1.4.1.55560.2.1.1.5", "1.3.6.1.4.1.55560.2.1.4.1")
}

func ferriteTicks() []tickPlan {
	return []tickPlan{
		{"mains_drift", []string{"status.mains.voltage_mv"}},
		{"load_drift", []string{"status.outlets.0.current_ma", "status.outlets.1.current_ma", "status.outlets.2.current_ma", "status.outlets.3.current_ma", "status.load.total_current_ma", "status.load.energy_mwh"}},
	}
}

func ferriteWatch() []string {
	return []string{"status.outlets.0.current_ma", "status.outlets.1.current_ma", "status.outlets.2.current_ma", "status.outlets.3.current_ma",
		"status.outlets.0.state", "status.outlets.2.state", "status.load.total_current_ma", "status.load.energy_mwh", "status.mains.voltage_mv"}
}

// ferriteRoomDay walks the rack through the parity room day: its load follows
// the room, and an operator's hand switches an outlet off and on in the middle
// of a meeting.
func ferriteRoomDay(d *Device, at *time.Time) []wireChange {
	w := newWatch(d, ferriteWatch()...)
	w.sample("start")
	operator := func(hms string) {
		set := func(path string, v any) {
			idx, _ := strconv.Atoi(path)
			s := d.State()
			outlets := s["status"].(map[string]any)["outlets"].([]any)
			outlets[idx].(map[string]any)["state"] = v
			d.SetState(s, d.Version+1)
		}
		switch hms {
		case "09:40:00":
			set("0", "off")
		case "11:00:00":
			set("0", "on")
		case "13:50:00":
			set("2", "off")
		}
	}
	roomDayWalk(d, at, w, "07:00:00", "15:00:00", 30*time.Second, func(string) {
		d.TickBehavior("mains_drift")
		d.TickBehavior("load_drift")
	}, operator)
	return w.out
}

var ferriteWire = wireFamily{
	Name: "ferrite-pdu-4c", Products: []string{ferriteProduct}, Engine: shippedEngineOf, Source: wireYAMLSource,
	Script: ferriteScript(), Writes: ferriteWrites(), OIDs: ferriteOIDs(), Walks: []string{"1.3.6.1.4.1.55560", "1.3.6.1.2.1.1"},
	Ticks: ferriteTicks(), RoomDay: ferriteRoomDay,
}

// wireYAMLSource is the provenance note the transcripts of the YAML profiles
// this slice replaced carry.
const wireYAMLSource = "recorded from the YAML profile before its deletion (#2836); see sim/testdata/wire/README.md"

func init() { wireFamilies = append(wireFamilies, ferriteWire) }
