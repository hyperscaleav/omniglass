---
title: "Platform health shows each server's request timing, ingest lag, queues and retention (#1645)"
date: 2026-11-10
refs: [1645, 1644, 1627, 1628, 1629, 1646]
---

[Platform health](/guides/operator/platform-health/) now gives each Omniglass server a panel set:
**Requests**, **Ingest**, **Queues** and **Retention**. The first set is open. A row is one
key: its last hour as a sparkline beside its current figure. Click a row to open the full chart. A
key's tooltip carries its catalog description and a link onto its series on the server's platform
component.

The panels are groups the platform view returns per server (`storage.PlatformServerGroups`), so
the page holds no key list. The slowest and the busiest routes are each cut in SQL to the newest
window's ten, highest first. A panel turns amber only while an alarm is open whose rule samples one
of its keys, such as Ingest lag high or Retention behind. A key never reported reads not reported,
and a caller without a fleet-wide component read gets no server panels at all.

The page polls, and each poll is a fresh body, so every level (server, panel, key, route, node,
column) renders through a new `KeyedFor` primitive that keeps a row by its key: an expanded chart,
an open tooltip and a closed panel set survive the poll.

Walkthroughs gained a `hover` step and an off-camera `ready` wait. Docs shots gained a `wait` step.
The divergences from the ticket are
[ADR-1645](/architecture/decisions/#adr-1645-server-panels-rank-in-sql-and-flag-from-the-rule).
The loss view across hops is #1646.
