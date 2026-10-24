---
title: "Listeners and their instantiation commands (#817)"
date: 2026-10-23
refs: [817]
---

The second slice of epic #603's stateful arc: a driver's listeners come alive over the held session.
The session arms every connection with the listeners' `arm` commands before it reads up (on establish
and on every recover; plumbing per the #603 ruling, each a `session armed` self-log with the device's
reply, an unanswered one a failed attempt). The held line connection's one reader routes every inbound
line by a documented precedence
([ADR-0151](/architecture/decisions/#adr-0151-a-held-line-session-routes-every-inbound-line-listeners-first-answers-by-reply-shape)):
a line a listener's `match` claims lands at once as a push sample under its listen task (the endpoint
its instance, no poll involved), and is never an exchange's answer unless it also matches the
exchange's declared reply shape; an exchange takes the first line its new `request.reply` shape
matches (or the first unclaimed line); a line waiting in the socket when an ask begins is routed, not
discarded; the rest is counted unclaimed, with a new spec-level `unmatched: collection-failed` policy
landing one fault per tick. Commands ride the held session too (the delivery now names its endpoint
and carries the binding's reply shape), falling back to a throwaway connection only when none is held,
and the transport registry marks `tcp` as held. The seeded `newtron-nvp` declares its reply shapes.
Proven over the simulator's NVP port (`internal/sim/listener_hold_test.go`: SUBSCRIBE INPUT armed, a
change made elsewhere heard within a second, thirty GET polls amid a stream of pushes each getting
their own answer, the subscription re-armed after the listener dies and returns) and end to end
(`TestNodeLandsPushedSamplesThroughARecover`: with the node's tick at an hour, a change on the panel
lands as the component's `video-input` within a second, and again after a recover).
The slice's adversarial review, fixed before the rollup, each with a real-socket test that failed with
the fix disabled. A reply may declare an `error` shape: the panel's `-ERR` refusal now answers a command
at once as a refusal carrying the device's text (its execution error and its reply), not a timeout that
recycled the session and charged its budget (`TestLineConnErrorReplyIsAFailureNotATimeout`, and over the
simulator in the listener test); the seeded `newtron-nvp` declares it. A shaped exchange's timeout fails
only that exchange and keeps the connection (`TestLineConnShapedTimeoutKeepsTheConnection`). An arm's ack
is the first line of any kind, a listener's push included, and no ack within the timeout is logged as
armed, unacknowledged rather than failing the attempt, so a device that acks with a push or not at all
no longer stays down (`TestSessionArmAckedByAPushReadsUp`, `TestSessionArmNeverAckedStillReadsUp`).
Regexes compile once per process (`TestRegexCompiledOnce`), and the drivers guide says a regex is an
RE2 substring search to anchor (`TestLineConnUnanchoredReplyRegexMatchesAPush` pins it).
