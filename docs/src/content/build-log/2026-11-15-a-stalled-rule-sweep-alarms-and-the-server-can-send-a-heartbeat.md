---
title: "A stalled rule sweep opens an alarm, and the server can send a heartbeat (#1986)"
date: 2026-11-15
refs: [1986, 1623, 1625, 1628]
---

A new platform gauge, `rule-sweep-age`, reads the seconds since a server's event rule sweep last
completed, and the seeded `rule-sweep-stalled` rule opens a `high` alarm on that server's component
when it passes 30 s, with no sustain. The report window reads a clock the sweep stamps, so the figure
does not depend on the goroutine it measures. Each server also stamps the shared database, and a 10 s
timer writes every peer's age onto its component, so a live server raises the alarm for a stalled one;
a clean stop releases the stamp so a rolling deploy does not alarm
([ADR-1986](/architecture/decisions/#adr-1986-the-heartbeat-is-a-signed-request-through-a-notification-target-and-the-sweep-age-is-read-from-a-shared-stamp)).

The alarm catches a stalled sweep, not a dead server. For that, an optional heartbeat, off by default:
set `heartbeat.target` to a webhook notification target and the server sends it a signed request every
`heartbeat.interval_seconds` (60 by default), through the egress allow list and into the audit log,
carrying the server's name, build and count of open platform alarms. A receiver that alarms on the
heartbeat's absence is the dead-man's switch; hosting one is out of scope.
