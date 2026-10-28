## serial-drain-behind-slow-destination (live)

- **Cue:** a queue drained one row at a time, oldest due first, where each row's cost is a remote
  timeout (a notification tick, an outbox relay).
- **Failure:** one dead destination's retries sit ahead of every other destination's fresh rows,
  so a storm with one bad target delays every healthy one by its timeouts times its retries.
  Found in the #1321 review; tracked as #1334.
- **Fix:** fairness across destinations (the oldest due row per destination round-robin), bounded
  parallel sends, or skipping a destination for the rest of a pass once it times out.
