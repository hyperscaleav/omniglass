---
title: "Held sessions reconcile on a worklist's arrival and survive a panic in one session (#1636)"
date: 2026-11-10
refs: [1636, 1637, 1624, 1700]
---

A node's held sessions now change the moment a worklist lands: the run loop's worklist case reconciles
them, so a held endpoint added, removed or re-keyed no longer waits for the next heartbeat, and a
reconcile on its own timer stays as the safety net. The loop is now a `select` over channel clocks on
the one goroutine that owns the session maps, so a unit test drives it with a heartbeat that never
fires and still sees a session start and stop. Every session-layer goroutine runs under
`collection.Supervise`: a panic on one device's bytes is logged with its stack, counted, and becomes
that session's `recovering` transition with the reason `panic: <value>`, then the normal backoff; the
node and every other session keep running. First dials and handshakes share node-wide slots
(`node run --session-dials`, default 16; a redial takes none), so a restart with fifty held endpoints opens them a few at a
time; the integration test restarts a node over fifty simulated panels with four slots, checks no more
than four connect at once, injects a panic into one panel's reader and watches that one recover while
the other forty-nine keep the connections they held. Unclaimed lines and push counts flush on a
housekeeping timer at the report window, capped at the heartbeat.
