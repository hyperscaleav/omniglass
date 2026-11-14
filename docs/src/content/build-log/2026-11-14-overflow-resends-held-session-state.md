---
title: "A spool overflow resends each held session's state, auth verdict and listener faults (#1213)"
date: 2026-11-14
refs: [1213, 453, 454]
---

When the node's telemetry spool drops batches at its cap, the run loop forgot the reachability
verdicts and a poll's faults and occurrences so the next tick would say them again, as ADR-0453 point
4 describes. It did not reset the held sessions' outputs, which publish only on a change: the
`session-state` from the session's transition, the auth verdict, and a listener's fault. A session
that went down while the bus was unreachable, with the batch saying so dropped, stayed up on the
server until its next transition.

The session manager now records each held session's last transition and auth verdict and each
listener's current faults, and has one `resync()` entry point that publishes them again. The run loop
calls it when the spool reports an overflow, beside the forgetting it already did; the sessions'
transition-only behavior is otherwise unchanged. The resend goes through the spool, so a resend that
is dropped raises the overflow again and the next tick repeats it. The nodes page names what an
overflow resends. ADR-0453's point 4 already says the node sends current state again, so it needs no
amendment. A unit test cuts the bus under a held session, overflows a one-batch spool, restores the
bus and asserts the next tick publishes the state, the verdict and the fault once each, and another
asserts a tick without an overflow publishes nothing.
