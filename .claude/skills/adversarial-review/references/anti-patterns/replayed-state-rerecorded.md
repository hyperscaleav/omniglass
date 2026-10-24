## replayed-state-rerecorded (fixed; found by the #1110 review)

- **Cue:** a component replays state it carried back (a gate adopted from the worklist, a session
  started rejected under it) through the same hook that records a fresh observation, and the
  record step does not tell the two apart.
- **Failure:** the replay is re-recorded as new and drops its provenance marker. A restarted
  node's held session started rejected under an adopted gate reported the gate's placeholder
  refusal back through `OnTransition`; `refuse` recorded it as a fresh gate, so the next publish
  sent the placeholder as `endpoint-rejection-reason`, overwriting the component's own words.
- **Fix:** a record that matches an adopted entry's value is its replay and keeps the entry as it
  is (`recordAt` in internal/node/gate.go); prove it with a session-backed restart test, not one
  built with no dialers (`TestAnAdoptedGatesSessionReplayPublishesNoReason`).
