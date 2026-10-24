## node-control-key-pushable (fixed; found by the fingerprint review; the driver-emit lane by the #1035 review)

- **Cue:** a property a node publishes to steer its own future attempts (a gate, a trusted key)
  that is also a seeded property type, so any lane that lands a named sample accepts it: the
  operator push lane, a driver's emits (a poll or listener emit named after it lands as the
  observed value through the poll path), a calc rule's output.
- **Failure:** a push grant plants a gate (commands fail, probes drop rungs) or lifts one with a
  future-dated `none`, an unaudited bypass of the audited `:retry`.
- **Fix:** refuse every node-only key by name on every lane that lands a named sample: the push
  lane (`nodeOnlyRejection` over `probekeys.IsNodeOnly`, internal/api/telemetry.go), driver emits
  at spec write (`validateEmits`, internal/driver/spec.go, #1035: a driver emitting
  `tls-certificate` would have had the worklist deliver its constant as the pin, bypassing the
  audited `:retry`), calc outputs (`probekeys.IsNative`, internal/storage/calc_rules.go).
- **Also (#1059):** a node's account of itself that a surface reads as the node's (a held
  session's `session-state` and `session-reason`) is the same class: a push read a rejected
  session up, hiding its Retry, or planted a reason the node never scrubbed.
- **Also (#1080):** so is every native probe key (the verdict and its rungs, the probe metrics
  and timings), so the ruling is per key in `internal/collection/probekeys`: `nodeOnly`, or
  `pushable` with a one-line reason. `TestEveryKeyRuled` fails on a declared key ruled neither,
  and `TestReachabilityReadsOnlyNodeOnlyKeys` on a key the reachability read serves that is not
  node-only. A new node-authored key is declared there, not as a literal elsewhere: a key
  declared outside `probekeys` is invisible to the ruling (`collection-failed` was, #1086).
- **Generalized (ADR-0161):** a per-route refusal is the class's weakest form. Every key now has one
  writer class (`internal/writerclass`), and every gateway write path holds its rows to it
  (`admitWrites`); `TestEverySampleWritePathAdmitsItsWriter` fails on a sample or event insert in
  a function that is not a registered write path or skips the check, so the cue is now a new write
  path, a new writer, or a new key a surface reads as the node's that is not declared in `probekeys`.
