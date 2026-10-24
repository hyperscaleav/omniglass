---
title: "One connection fingerprint gates every connection (#1056, #1060)"
date: 2026-10-23
refs: [1056, 1060]
---

The architect's rulings on #1056 and #1049, folded into the rollup. A component that hangs up after
the final secret with no refusal text charges a **login budget** (three in a row read `rejected` as
`auth`; `ErrLoginDropped` in the login dialog, a sim telnet face that rejects silently proves it over
the wire). The retry signal is an explicit `session_retry` counter on the endpoint (a migration), not
`updated_at`, so a label edit no longer pulls a rejected session out. `internal/fingerprint` is the
**connection fingerprint**: the worklist computes one per endpoint over every input a connection is
made with, the attached secrets as digests of their sealed envelopes, and a reflection test and a
table test hold its inputs complete. The node holds a **gate** per endpoint
(`internal/node/gate.go`) at the ladder rung a terminal refusal holds, for that fingerprint, across
its held session (started rejected), the ssh probe (#1060: its params credential tried once, and
never past a changed pinned key), stateless polls and commands; the gate publishes as
`endpoint-rejection` and is carried back, so a restarted node makes no attempt. The driver spec's
`ladder` declares the rungs (an informational ping, the transport rung that gates, the session rung),
defaulted by the registry's new `rung`, a request rung proven over the line client. Proven end to end
(`run_session_identity_test.go`): a silent panel spends the login budget; a label edit and a node
restart try nothing; a retry, a login-declaration change, a rotated secret and a re-attach over ssh
each try again; a wrong port reads down on refused connects and tries no login; the ssh probe sends a
wrong password once, across a restart, then authenticates after a params edit. Recorded as
[ADR-0158](/architecture/decisions/#adr-0158-one-connection-fingerprint-gates-every-connection-on-a-ladder-the-driver-declares),
with ADR-0155 amended.
