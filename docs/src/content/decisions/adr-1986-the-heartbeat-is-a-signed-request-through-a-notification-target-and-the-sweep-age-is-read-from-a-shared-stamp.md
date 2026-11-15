---
title: "The heartbeat is a signed request through a notification target, and the sweep age is read from a shared stamp"
date: 2026-11-15
status: Accepted
summary: "Accepted: a stalled rule sweep is a platform gauge, `rule-sweep-age`, and a seeded `high` rule over 30 s. The sweep stamps the shared database on the database's clock; a 10 s peer timer writes each peer's age on its component, and a server's own age comes from an in-memory clock. A clean stop releases the stamp, a crash keeps it. The optional heartbeat is a `heartbeat` notification built, signed, sent and audited through the existing target machinery, named by the `heartbeat.target` setting, audited on a change of outcome only."
refs: [1986, 1623, 1625, 1628]
---

- **Date:** 2026-11-15 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [alarms and actions](/architecture/alarms-actions/), [workers](/architecture/workers/)
- **Decision:**
  1. **The sweep age is independent of the sweep.** A server's own `rule-sweep-age` is read from an
     in-memory clock the sweep sets after each completed pass and that begins at server start, so a
     sweep that never completes still ages.
  2. **Peers read the shared database, on its clock.** Each completed sweep stamps
     `platform_server.rule_swept_at` with `now()` on the database, so no server's wall clock is
     compared with another's. A 10 s timer writes every other server's age on that server's component,
     never its own, so a live server raises `rule-sweep-stalled` for a stalled peer.
  3. **A clean stop releases the stamp; a crash does not.** A rolling deploy therefore does not alarm.
     A server that dies keeps its stamp and ages, and the alarm is raised only while a live peer
     exists. A lone server has no peer: that is the gap the heartbeat exists for, and the docs say so.
  4. **The threshold is 30 s, six sweep periods, high, no sustain.** The age is already a duration, as
     `checkin-age` is ([ADR-1707](/architecture/decisions/#adr-1707-a-silent-node-alarms-through-a-seeded-rule-over-its-checkin-age-not-a-node-liveness-sweep)).
  5. **The heartbeat is a notification, not a second sender.** It is a `heartbeat` transition built and
     signed by `notify.Build`, sent by the egress client under the allow list, to a webhook
     notification target the `heartbeat.target` platform setting names, with `heartbeat.interval_seconds`
     (60, 10 to 86400). No target means no request. A disabled target or a missing credential is
     refused and audited, never sent unsigned.
  6. **It audits on a change of outcome.** One audit row when the heartbeat first succeeds or fails, or
     fails for a new reason, not one per interval: a minute-by-minute row would bury the audit log
     with the one fact it already holds.
- **Rejected:** a heartbeat to a URL held in a setting (it would bypass the target's secret and the
  audit); judging a peer by its wall-clock stamp (skew would raise false alarms); hosting a receiver
  (out of scope).
