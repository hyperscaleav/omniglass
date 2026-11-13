## retry-booked-from-run-start (fixed; found by the #1634 review)

- **Cue:** a failed attempt's retry booked as `start + backoff` from a time read before the attempt
  ran, on a scheduler that runs a past due at once.
- **Failure:** an attempt that takes longer than its backoff to fail (a device that times out after
  10s, a backoff starting at 1s) finishes with its retry already past due, so the dispatcher runs it
  again the moment it ends: back to back attempts at 0, 10, 20, 30 and 40s against a dead device,
  until the doubled backoff outgrows the timeout. Under the old heartbeat ticker the next tick hid
  it; a timer that wakes at the earliest due exposes it.
- **Fix:** book a retry from the clock read after the attempt ends (`runPoll` in
  `internal/node/probe.go`), pinned by a getter that moves the fake clock by its timeout:
  `TestAFailedPollRetriesABackoffAfterItsRunEnded` in `internal/node/scheduler_race_test.go`.
