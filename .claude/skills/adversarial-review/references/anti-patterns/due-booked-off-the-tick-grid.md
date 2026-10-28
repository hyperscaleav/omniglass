## due-booked-off-the-tick-grid (fixed; found by the #1273 review)

- **Cue:** a per-task schedule on a fixed ticker that compares a task's due time against, and books
  its next run from, a `now` taken after variable work in the tick (a publish, a pull, a reconcile).
- **Failure:** a tick that reaches the task a few milliseconds sooner than the tick that booked it
  finds it not yet due and skips it, so an interval equal to a multiple of the heartbeat runs at
  random gaps of double that interval. The node's `pollSchedule.due` skipped ticks 1, 3 and 5 of six
  at 2s/2s with 8 to 15ms of jitter, for probes (#1273) and for driver polls whose `schedule.every`
  equalled the heartbeat.
- **Fix:** judge due within half a tick of the due time (`now.Add(heartbeat/2).Before(at)` is not
  due), keep booking from the due time so the tolerance never drifts the cadence, and pin it with a
  test at jittered offsets: `internal/node/probe_interval_test.go`.
