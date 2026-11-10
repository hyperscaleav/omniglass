---
title: "Held sessions reconcile on arrival, are supervised, and dial through node-wide slots"
date: 2026-11-10
status: Accepted
summary: "Accepted: a worklist reconciles held sessions the moment it lands, with a reconcile on its own heartbeat-interval timer as the safety net; every session-layer goroutine runs under a recover that turns a panic into that session's ordinary failure; a session's first dial and handshake share node-wide slots (--session-dials, default 16), a redial takes none; unclaimed lines and push counts flush on a housekeeping timer at the shorter of the report window and the heartbeat. All of it stays on the one run-loop goroutine (ADR-1637), so #1634's lock is not needed for the push count. Publishing the recovered-panic count as a node metric is #1700."
refs: [1636, 1637, 1624, 1634, 1094, 1625, 1700, 816, 1588]
---

- **Date:** 2026-11-10 | **Status:** Accepted | **Pages:** [nodes](/architecture/nodes/#sessions), [collection](/architecture/collection/)
- **Decision:**
  1. **Reconcile on arrival.** The worklist case of the node's run loop reconciles held sessions and
     datagram sockets as soon as a new worklist lands, instead of storing it for the next heartbeat
     to reconcile. A held endpoint added, removed or re-keyed changes within the pull, not within the
     pull plus a heartbeat. A reconcile on its own timer, at the heartbeat interval, stays as the
     safety net for a gate or a retry that changed between worklists.
  2. **One owner.** Every reconcile, the heartbeat's work and the housekeeping pass run on the one
     run-loop goroutine, the session maps' one writer (ADR-1637); the lane workers that borrow a
     session read the maps under their lock ([ADR-1634](/architecture/decisions/#adr-1634-each-poll-and-probe-runs-on-its-own-interval-from-a-dispatcher-of-its-own)).
     The loop is a `select` over channels, so a test drives it with clocks it owns and a heartbeat
     that never fires.
  3. **Supervision.** A `Supervise` primitive in `internal/collection` wraps each session-layer
     goroutine: the session machine, each connection's reader (line, http stream, udp, websocket,
     ssh), the arm conversation, the node's hooks, and the datagram arm. A recovered panic is logged
     at error with its stack (facility `collection`), counted, and returned as a `PanicError` that
     takes the session's ordinary failure path: the connection is closed, the session reads
     `recovering` with the reason `panic: <value>`, the normal backoff redials, and the attempt
     budget still drives a session that panics every time to `down`. A datagram arm that panics
     closes its socket and the next reconcile binds and arms it again.
  4. **Bounded establish.** A session's first dial and handshake (connect, TLS or ssh handshake,
     the login dialog) acquire one of the node's `DialSlots` before they start and release it once
     the connection is made or has failed. `node run --session-dials` (env
     `OMNIGLASS_NODE_SESSION_DIALS`, default 16) sets the count. A session waiting for a slot reads
     `establishing`. A redial takes no slot: an endpoint that blackholes its dials holds a slot for
     its whole dial timeout on every attempt, so bounding redials would let a few dead endpoints
     queue every healthy session's recovery behind theirs (at 16 slots, the 5 s default timeout and the
     30 s backoff cap, about a hundred blackholed endpoints keep every slot busy, and fewer already
     make a recovery wait). The jittered backoff already spreads a
     mass redial.
  5. **Housekeeping timer.** `flushUnclaimed`, the push count into the collection report and the
     report's publish run on their own timer at the shorter of the report window and the heartbeat.
     The spec named the report cadence; at the default one-minute window and 30 s heartbeat that
     would have held unclaimed lines twice as long as the heartbeat did, so the heartbeat caps it.
- **Divergences and follow-ups:** the spec had `reportPushes` take the report under the lock #1634
  adds. Housekeeping stays on the run-loop goroutine, so nothing else touches the report while it
  runs and no lock is needed here; #1634 adds one if it moves the poll path off that goroutine. The
  spool retry stays on the heartbeat, so its ordering against the batch it follows is unchanged. The
  recovered-panic count is a process counter in the log line today; publishing it as a node metric
  (as a loss ratio of session attempts) is **#1700**, alongside the platform metrics of #1094 and
  #1625.
- **Context:** before #1636 a panic on any session goroutine ended the node process, and with it
  every other session on the node; a node restarting with hundreds of held endpoints dialed all of
  them in one pass; and a held endpoint's change waited up to a heartbeat after its worklist landed.
  #1588 made a fleet-wide start survivable for worklist pulls, not for session dials.
- **Rejected:** a supervisor goroutine that restarts a dead session from outside (the session already
  owns its retry and backoff, so a panic becoming its ordinary failure reuses both and needs no second
  owner); recovering a panic silently (a panic is a bug, so it is logged with its stack and counted);
  bounding dials per endpoint type instead of node-wide (the cost bounded is the node's, file
  descriptors and handshake CPU, whatever the transport).
