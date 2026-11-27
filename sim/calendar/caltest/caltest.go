// Package caltest is the calendar's test helper: a test that has picked an
// entry of a kind reads its payload by value, and fails loudly if the entry is
// of another kind. Production code does not import it; it opens a payload
// through calendar.Entry's AsMeeting and AsProblem and handles the false.
package caltest

import "github.com/hyperscaleav/omniglass/sim/calendar"

// M is the entry's meeting. It panics on an entry of another kind, which in a
// test is the failure.
func M(e calendar.Entry) calendar.Meeting {
	m, ok := e.AsMeeting()
	if !ok {
		panic("caltest: " + string(e.Kind) + " entry is not a meeting")
	}
	return m
}

// P is the entry's problem. It panics on an entry of another kind.
func P(e calendar.Entry) calendar.Problem {
	p, ok := e.AsProblem()
	if !ok {
		panic("caltest: " + string(e.Kind) + " entry is not a problem")
	}
	return p
}

// IsMeeting says whether the entry is a meeting.
func IsMeeting(e calendar.Entry) bool { _, ok := e.AsMeeting(); return ok }
