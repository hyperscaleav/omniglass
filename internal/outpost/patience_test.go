package outpost

import "time"

// patience is how long a test waits for an event it is not itself timing: a
// property to land, a session to come up, a command to settle. The wait ends
// the moment the event is seen, so a generous ceiling costs nothing when the
// outpost is healthy; it is only the time a red run spends proving the event
// never came. How long the event takes is the machine's speed (the stack under
// test is Postgres, a bus, the ingest lane and the outpost sharing the cores
// of a runner that is running other shards), so the ceiling is a multiple of
// the base a quiet machine needs, with a floor, never a window to be met: a
// starved runner took 4.8 s for what a quiet one does in 100 ms (#1717). A
// test that asserts how long something takes uses pushWindow, or a bound on
// the work done, instead.
func patience(base time.Duration) time.Duration {
	return max(6*base, 30*time.Second)
}

// pushWindow is how long a pushed change may take to land when the test proves
// it was pushed: the cadence it must beat is a poll of a minute or more (an
// hour for the drivers whose polls the test removes), so a change that lands
// inside it cannot have been polled. It bounds the push from the poll, not the
// push's latency, which no assertion on a shared runner can pin.
const pushWindow = 20 * time.Second
