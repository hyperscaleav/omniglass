## node-control-key-pushable (fixed; found by the fingerprint review)

- **Cue:** a property a node publishes to steer its own future attempts (a gate, a trusted key)
  that is also a seeded property type, so the operator push lane accepts it.
- **Failure:** a push grant plants a gate (commands fail, probes drop rungs) or lifts one with a
  future-dated `none`, an unaudited bypass of the audited `:retry`.
- **Fix:** refuse every node-only key on the push lane by name, on every lane
  (`nodeOnlyRejection` over `probekeys.IsNodeOnly`, internal/api/telemetry.go).
- **Also (#1059):** a node's account of itself that a surface reads as the node's (a held
  session's `session-state` and `session-reason`) is the same class: a push read a rejected
  session up, hiding its Retry, or planted a reason the node never scrubbed.
- **Also (#1080):** so is every native probe key (the verdict and its rungs, the probe metrics
  and timings), so the ruling is per key in `internal/collection/probekeys`: `nodeOnly`, or
  `pushable` with a one-line reason. `TestEveryKeyRuled` fails on a declared key ruled neither,
  and `TestReachabilityReadsOnlyNodeOnlyKeys` on a key the reachability read serves that is not
  node-only. A new node-authored key is declared there, not as a literal elsewhere: a key
  declared outside `probekeys` is invisible to the ruling (`collection-failed` is, #1086).
