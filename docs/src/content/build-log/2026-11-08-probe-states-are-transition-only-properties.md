---
title: "A probe's state is written when it flips, not every tick (#1592)"
date: 2026-11-08
refs: [1592, 1265, 1591, 1593, 1594]
---

The two probe states, `icmp-reachable` and `tcp-open`, left the metric lane for the property lane.
They are words now (`up`/`down`, `open`/`closed`), and the property lane's transition-only guard
writes each one once until it changes; the probe timings beside them (`icmp-rtt-avg`,
`tcp-connect-time`) stay metrics, every tick. The reachability API's `value` reads the word, the
console panel and the endpoint verdict read the word, and the request-layer rungs (udp, snmp)
report `up` or `down` the same way.

The basis is the 1,000-system measured run (#1591): the metric lane landed about 109 M rows a day
and the two states were about half of them, each restating the previous row on a healthy fleet.
Liveness never read those rows, so nothing an operator sees changes except that the storage curve
loses the flat half; `endpoint_live.last_seen_at` is stamped after every landed batch, and the new
storage test proves a steady `closed` batch that writes no property row still refreshes it.
The ingest footprint's statement budget for the standard mix rose from 18 to 22 because the property
lane still takes a lock and a guarded insert per sample; one statement for the whole property lane is
#1593, and `camera-preset` as a property is #1594
([ADR-1592](/architecture/decisions/#adr-1592-a-probes-state-is-a-transition-only-property-not-a-metric-row)).
