---
title: "The ladder climbs to layer 7"
date: 2026-10-17
refs: []
form: bullet
---

- **The ladder climbs to layer 7.** The `http` probe draws a real response off the API (any
  status is an answer; the dial is hooked so port-closed classifies on the L4 rung while an
  accepting port that never answers reads reached-but-not-responsive) and the `ssh` probe runs a
  real key exchange (a completed exchange that refuses the credential reads
  responded-but-not-authenticated, distinct from a daemon that never speaks SSH). The canon grows
  five-lane-shaped: `http-response-time` and `ssh-handshake-time` on the metric lane,
  `endpoint-responsive` and `endpoint-authenticated` (written only when a credential was actually
  tried, so an unauthenticated probe never reads as a failed login) on the property lane, and the
  `collection-failed` event type, which the node now lands for a task whose config cannot be used
  instead of only whispering into its own self-logs. The reachability read serves the new rungs
  per endpoint and the panel wears them as chips that appear only once a probe has climbed them.
  Proof is the capability carve-out's both halves: fake-based sample shapes plus real-socket
  integration tests, an httptest server, an in-process ed25519 SSH daemon whose password callback
  genuinely adjudicates, and accept-and-hang listeners for the not-responsive rung (#812, #603).
