## exactly-once-assumption (live; #430 tracks the contract)

- **Cue:** new bus code that publishes fire-and-forget to a JetStream subject, assumes
  redelivery cannot happen, or writes sinks in multiple transactions before a single ack.
- **Failure:** samples silently lost on a JetStream outage; duplicates on redelivery
  into sinks with no uniqueness key (the audit's P0-1).
- **Fix:** until #430's staged contract lands, new code must not widen the gap: publish
  with ack where the API allows, and never assume dedup exists downstream.
- **Single-shot trust publish** (found by the #1032 review, fixed): a security-relevant fact the
  node learns once (an ssh host key trusted on first use) published once, deduped in memory, and
  lost on the next recycle. Republish it until the server reflects it back, and carry it in
  memory across recycles (`pins` in internal/node/session.go). The same holds for the gate a node
  publishes (`endpoint-rejection`, found by the fingerprint review): republish until the worklist
  reflects it, under a task chosen from the current worklist (a re-derived driver deletes old task
  ids), never a cached "already published".
