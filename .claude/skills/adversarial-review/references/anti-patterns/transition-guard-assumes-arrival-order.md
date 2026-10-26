## transition-guard-assumes-arrival-order (fixed; found by the #311 re-review)

- **Cue:** a drop-if-equal-to-latest guard (a transition-only series) behind a consumer that
  Naks a failed message and keeps consuming, behind prefetch, or behind a worker pool.
- **Failure:** a successor lands before a redelivered message and is judged against the wrong
  latest. A flip pair (A, then B fails once, then A again) lands A, skips the second A as
  unchanged, then lands B: the series reads B though the device reported A last, and a node that
  sends only on change never corrects it. A per-series lock stops duplicate rows; it does not order
  them.
- **Fix:** keep each publisher's messages landing in order end to end: one message in flight on
  the consumer (`MaxAckPending` 1, internal/bus/consumer.go), a transient failure redelivered with
  a growing delay before anything behind it, a write no retry changes dropped at once
  (`storage.IsRefusedWrite`) so it cannot hold the rest, and an in-order publisher
  (`serial-ack-on-caller-goroutine`). A pool needs sequencing keyed by publishing subject. Pin it
  with a flip pair through a once-failing landing (`TestATransientFailureKeepsASeriesInOrder`).
