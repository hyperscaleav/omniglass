## replayed-state-rerecorded (fixed; found by the #1110 review)

- **Cue:** a component replays state it carried back (a gate adopted from the worklist, a session
  started rejected under it) through the same hook that records a fresh observation, and the
  record step does not tell the two apart.
- **Failure:** the replay is re-recorded as new and drops its provenance marker. A restarted
  node's held session started rejected under an adopted gate reported the gate's placeholder
  refusal back through `OnTransition`; `refuse` recorded it as a fresh gate, so the next publish
  sent the placeholder as `endpoint-rejection-reason`, overwriting the component's own words.
- **Fix:** tell the replay by identity, never by value: the session hands back the very refusal it
  was started with, so `start` in internal/node/session.go skips the record when `tr.Refusal` is
  that pointer. Value equality is the trap a second time: a driver's re-probe redials under the same
  fingerprint and meets a real refusal with the same gate value, and its words must still record
  and publish (`syncRejection` republishes a moved reason under an unchanged value). Prove both
  with session-backed tests, not ones built with no dialers
  (`TestAnAdoptedGatesSessionReplayPublishesNoReason`,
  `TestAReprobedRefusalPublishesItsReasonUnderAnUnchangedGate`).
