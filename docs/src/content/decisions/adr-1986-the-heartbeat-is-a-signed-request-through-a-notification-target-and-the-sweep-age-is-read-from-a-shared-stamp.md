---
title: "The heartbeat is a signed request through a notification target, and the sweep age is read from a shared stamp"
date: 2026-11-15
status: Accepted
summary: "Accepted: a stalled rule sweep is a platform gauge, `rule-sweep-age`, and a seeded `high` rule over 30 s. The sweep stamps the shared database on the database's clock; a 10 s peer timer writes each peer's age on its component, and a server's own age comes from an in-memory clock. A peer is judged only while it marks itself seen, so a crashed server drops out rather than holding an alarm open; a clean stop releases both stamps. The optional heartbeat is a `heartbeat` notification built, signed, sent and audited through the existing target machinery, named by the `heartbeat.target` setting, audited on a change of outcome only."
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
  3. **A peer is judged only while it is seen alive.** The same 10 s timer stamps
     `platform_server.seen_at` for its own server, apart from the sweep, so a server whose sweep has
     stalled is still seen and judged. A peer is read only while its `seen_at` is under 20 s old. A
     server that dies stops stamping both, and since its last sweep was at most one 5 s period before
     its last mark, it drops out of the read before its stamp could pass 30 s: no live server raises a
     `high` alarm on a dead component that nothing would ever clear (a container's hostname identity
     never comes back to clear it). A clean stop releases both stamps, so a rolling deploy does not
     alarm. A dead server, and a lone one, is the gap the heartbeat exists for, and the docs say so.
  4. **The threshold is 30 s, six sweep periods, high, no sustain.** The age is already a duration, as
     `checkin-age` is ([ADR-1707](/architecture/decisions/#adr-1707-a-silent-node-alarms-through-a-seeded-rule-over-its-checkin-age-not-a-node-liveness-sweep)).
  5. **The heartbeat is a notification, not a second sender.** It is a `heartbeat` transition built and
     signed by `notify.Build`, sent by the egress client under the allow list, to a webhook
     notification target the `heartbeat.target` platform setting names, with `heartbeat.interval_seconds`
     (60, 10 to 86400). No target means no request. A disabled target or a missing credential is
     refused and audited, and so is a target with no `hmac-key` secret bound: a beat is never sent
     unsigned.
  6. **It audits on a change of outcome.** One audit row when the heartbeat first succeeds or fails,
     fails for a new reason, or moves to another target, not one per interval: a minute-by-minute row would bury the audit log
     with the one fact it already holds.
- **Rejected:** a heartbeat to a URL held in a setting (it would bypass the target's secret and the
  audit); judging a peer by its wall-clock stamp (skew would raise false alarms); judging every held
  stamp, or every stamp younger than a limit (a crashed peer's stamp would open an alarm nothing
  clears, or a limit under 30 s would never open one); hosting a receiver
  (out of scope).
