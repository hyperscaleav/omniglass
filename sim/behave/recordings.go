package behave

import "github.com/hyperscaleav/omniglass/sim/calendar"

// RecordingRoom is a room a recording service speaks for and its state.
type RecordingRoom struct {
	Name  string
	State calendar.RoomState
}

// RecordingCounts is what a recording service reports as its work.
type RecordingCounts struct{ InProgress, QueueDepth int }

// RecordingParams are a recording service's rules for a service that has no
// calendar to read: the walks its two counters take.
type RecordingParams struct {
	InProgressMax, InProgressStep float64
	QueueMax, QueueStep           float64
}

// DefaultRecordings is the recording service's drift.
var DefaultRecordings = RecordingParams{InProgressMax: 12, InProgressStep: 1.5, QueueMax: 40, QueueStep: 4}

// recorded says the room's running meeting is recorded.
func recorded(s calendar.RoomState) bool {
	if !s.HasMeeting {
		return false
	}
	m, _ := s.Meeting.AsMeeting()
	return m.Recorded
}

// Recordings is what the service reports: a recording is in progress while a
// recorded meeting runs in a room it reads, and queued for the recording tail
// after one ends, waiting to be written. The rooms it reads are those it
// covers, else the one it sits in (own). A service whose rooms hold no
// calendar drifts: in-progress first, then the queue, one draw each.
func Recordings(prev RecordingCounts, covered []RecordingRoom, own calendar.RoomState, p RecordingParams, rng Source) RecordingCounts {
	var next RecordingCounts
	seen := false
	if len(covered) > 0 {
		for _, r := range covered {
			seen = seen || r.State.Scheduled
			if recorded(r.State) {
				next.InProgress++
			}
			if r.State.RecordingPending {
				next.QueueDepth++
			}
		}
	} else {
		seen = own.HasCalendar
		if recorded(own) {
			next.InProgress = 1
		}
		if own.RecordingPending {
			next.QueueDepth = 1
		}
	}
	if !seen {
		next.InProgress = int(Round(0, Clamp(0, p.InProgressMax, Step(rng, p.InProgressStep, float64(prev.InProgress)))))
		next.QueueDepth = int(Round(0, Clamp(0, p.QueueMax, Step(rng, p.QueueStep, float64(prev.QueueDepth)))))
	}
	return next
}

// RoomRecording is one room's own recording and queue.
type RoomRecording struct {
	Name                   string
	InProgress, QueueDepth int
}

// PerRoom is each room's recording and queue off its own state, one record a
// room in the order given.
func PerRoom(rooms []RecordingRoom) []RoomRecording {
	out := make([]RoomRecording, 0, len(rooms))
	for _, r := range rooms {
		rec := RoomRecording{Name: r.Name}
		if recorded(r.State) {
			rec.InProgress = 1
		}
		if r.State.RecordingPending {
			rec.QueueDepth = 1
		}
		out = append(out, rec)
	}
	return out
}
