---
title: "A held-session test follows its session, not a count (#1784)"
date: 2026-11-11
refs: [1784, 1577]
---

The test that a transient storage error leaves a node's held session alone read the panel's session
count and wanted exactly one. The endpoint's reachability probe dials the same panel every 200 ms in
that test, and each dial is a session for a moment, so a read that landed on a probe saw two and the
test failed under CI load. Sampling the count through the outage reached two in one run of three; with
the probe slowed to an hour it stayed at one in five of five.

The simulator's session registry now names its open sessions (`SessionIDs`). The test captures the held
session's ID once the panel holds exactly one, and checks that same session is still open while storage
is down and after it returns. A probe beside it no longer matters, and a node that re-dialed during the
outage would still fail it, since a new connection carries a new ID.
