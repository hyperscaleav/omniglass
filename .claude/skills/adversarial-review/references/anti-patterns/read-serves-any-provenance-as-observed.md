## read-serves-any-provenance-as-observed (fixed; found by the #1080 review, closed by ADR-0161)

- **Cue:** a read that serves a value as one producer's observation (the node's verdict, a session's
  state, a probe timing, a gate) and selects the latest row of the key and instance with no
  `provenance = 'observed'` filter; a transition or dedupe guard that compares a new observation
  against the latest row of any provenance.
- **Failure:** a declared, intended or calculated row beside the series, newer than the last
  observation, is served as the observation, or makes the guard skip a real one. Refusing the writes
  that could plant such a row narrows the hole but leaves every row written before the refusal, and
  any future writer, read as the node's.
- **Fix:** the read names the provenance it serves: `LatestProperty`, `LatestMetricInstance`,
  `LatestMetric` and `PropertyTransitions` read the observed series only (a series is `(type, owner,
  instance, provenance)`), and a read that wants another provenance asks for it by name
  (`LatestValue`); the health reads serve the rollup's calculated rows alone. Pair it with the
  write side: the key's writer class.
