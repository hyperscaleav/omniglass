## due-booked-off-the-tick-grid (retired by #1634; found by the #1273 review)

- **Cue:** a per-task schedule on a fixed ticker that compares a task's due time against, and books
  its next run from, a `now` taken after variable work in the tick (a publish, a pull, a reconcile).
- **Failure:** a tick that reaches the task a few milliseconds sooner than the tick that booked it
  finds it not yet due and skips it, so an interval equal to a multiple of the heartbeat runs at
  random gaps of double that interval. The node's `pollSchedule.due` skipped ticks 1, 3 and 5 of six
  at 2s/2s with 8 to 15ms of jitter, for probes (#1273) and for driver polls whose `schedule.every`
  equalled the heartbeat.
- **Fix:** first a half-tick tolerance on the due check (#1273); then #1634 retired the ticker for
  task runs. The node's dispatcher sleeps to the earliest due on a timer of its own and books each
  next run on the task's phase grid (`slotAfter` in `internal/node/scheduler.go`), a slot at least
  half an interval past the due it ran for, so no jitter in when it ran moves the cadence. Pinned by
  `internal/node/scheduler_test.go` and `internal/node/probe_interval_test.go`.
