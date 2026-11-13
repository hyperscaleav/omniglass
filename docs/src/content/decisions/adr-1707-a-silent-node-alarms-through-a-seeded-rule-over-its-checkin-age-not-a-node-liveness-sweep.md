---
title: "A silent node alarms through a seeded rule over its checkin-age, not a node-liveness sweep"
date: 2026-11-11
status: Accepted
summary: "Accepted: the node-down alarm the nodes and workers pages designed as a sweep of its own, raised directly with no event rule because a dead node emits no sample, is the seeded official node-silent rule over the checkin-age the server writes for every claimed node every 15 s: past 90 s it opens a high alarm on the node's platform component, and the next heartbeat clears it. No OMNIGLASS_NODE_DOWN_AFTER; the threshold is the rule's."
refs: [1707, 1094, 1625, 419]
---

- **Date:** 2026-11-11 | **Status:** Accepted | **Pages:** [nodes](/architecture/nodes/), [workers](/architecture/workers/), [collection](/architecture/collection/)
- **Decision:**
  1. **A rule, not a sweep.** The design (#419) had a node-liveness sweep of its own scan heartbeat
     freshness and raise a node-owned `node-down` alarm directly, keyed for idempotency, since a
     dead node emits no sample for a rule to read. Since #1625 the server writes one: `checkin-age`,
     each claimed node's seconds since it last spoke, every 15 s on the node's platform component,
     growing while the node is silent. The seeded official `node-silent` rule reads it, so the
     event rule sweep and streak already in place raise and clear it, with its events, timeline,
     notifications and Rules page row like any rule's.
  2. **On the node's platform component, at high.** The alarm is raised where `checkin-age` is
     written, not on a node-owned arc, so it reads in the platform system's health like every other
     self-monitoring alarm. `high` is an outage for that component.
  3. **Fire past 90, clear at or under 90, no sustain.** 90 s is three missed 30 s heartbeats; the
     age is already the sustain, so a second one would only delay the alarm.
  4. **No `OMNIGLASS_NODE_DOWN_AFTER`.** The threshold is the official rule's. An operator who wants
     another writes a rule of their own on `checkin-age`.
- **Context:** #1094 asks for a silent node and a filling disk to alarm with no operator setup.
  `disk-nearly-full` (over 0.9 for 2 minutes, cleared at 0.85) follows the `ingest-lag-high` shape
  and diverges from no design.
- **Rejected:** a dedicated sweep (a second path to the same alarm, outside the rule's events and
  state); a fire sustain on top of the 90 s age (three heartbeats is the sustain already).
