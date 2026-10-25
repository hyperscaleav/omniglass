## producer-clock-outlives-tenure (fixed; found by the ADR-0161 review)

- **Cue:** a series whose latest row is read as a producer's current account (a node's verdict, its
  gate, the key it trusted) ordered by a time the producer supplies, with nothing bounding that time
  against the server's clock.
- **Failure:** a producer dates its rows far ahead, so they stay "latest" after it stops being the
  producer: a node moved off an endpoint keeps its verdict and gate current, the new node's
  readings are judged stale, and the control reads hand the new node the old one's gate and pin.
- **Fix:** cap a node-probe sample's time at the database's now past a small skew
  (`sampleClock`, internal/storage/key_writer.go), or order control reads by arrival.
- **Second instance (open, #1143):** a calc window measured as the producer's `ts` against the
  server's now (`readCalcWindow`): a lagging collector has no sample in the window and the input reads
  missing; one running ahead widens the window. Documented on the calculations page; an arrival-time
  window is filed.
