---
title: "A probe's state is a transition-only property, not a metric row"
date: 2026-11-08
status: Accepted
summary: "Accepted: the two probe states (icmp-reachable, tcp-open) leave the metric lane for the property lane, written only when they flip and encoded as words (up/down, open/closed); the probe timings stay metrics, every tick. Liveness is untouched because it never read these rows. No node-side guard; the ingest insert's guard is the one place a repeat is dropped. Landing the property lane in one statement is #1593, camera-preset is #1594."
refs: [1592, 1265, 1591, 1593, 1594, 1274, 1273]
---

- **Date:** 2026-11-08 | **Status:** Accepted | **Pages:** [properties](/architecture/properties/), [collection](/architecture/collection/), [nodes](/architecture/nodes/), [scaling](/architecture/scaling/)
- **Decision:**
  1. **The probe states are properties.** `icmp-reachable` and `tcp-open` leave the `metric_type`
     catalog for the `property_type` catalog, `data_type: string` with a closed enum each
     (`up`/`down`, `open`/`closed`). The node emits them as text samples, ingest routes them down
     the property lane by registry membership, and the lane's transition-only guard drops every
     observation that equals the stored latest. A steady fleet writes each state once.
  2. **The probe timings stay metrics.** `icmp-rtt-avg` and `tcp-connect-time` are numbers an
     operator trends, so they land every tick as before. A probe still produces two samples; one
     changed lanes.
  3. **Readers read the word.** The endpoint verdict, the reachability panel and the node's
     request-layer rungs (udp, snmp) carry the state as a string; the API's reachability `value`
     field is a string, not a number. There is no numeric shadow of the state anywhere.
  4. **Liveness is untouched.** `endpoint_live.last_seen_at` is stamped by the ingest consumer
     after every landed batch whether or not the property guard wrote a row, so a steady "up" that
     writes nothing still proves the endpoint was seen. The storage test asserts this directly.
  5. **No node-side guard.** The node sends every observation; the server-side guard inside the
     insert is the one place a repeat is dropped. A node that remembered its last state would
     have to persist it across restarts and reconcile it with the server, for no saving on the
     wire that matters at this cadence.
- **Divergences and follow-ups:** the property lane still costs an advisory lock and a guarded
  insert per sample, so the standard mix's one-batch statement budget rose from 18 to 22 with the
  two new properties; landing the property lane in one statement, as #1274 did the metric lane,
  is **#1593**. `camera-preset`, a numeric name that belongs in the same lane, is **#1594**.
- **Measured basis (#1591, corporate shape, 1,000 systems):** the metric lane landed 75,914 rows a
  minute (about 109 M a day, 11.84 GiB a day at about 116 B a row, 30.76 GiB of WAL a day), and
  `icmp-reachable` plus `tcp-open` were about half of those rows. Every one of them restated the
  previous one on a healthy fleet.
- **Context:** the probe's reachable result is the most-written datum in the platform and the least
  useful to an operator as a series: nobody asks how often a ping succeeded, they ask whether the
  endpoint is healthy and when it last flipped. Reach is a platform-level fact (can the node see
  the device) that the health roll-up and the liveness stamp already consume; storing it as a
  property keeps the flip history and discards the repetition.
- **Rejected:** keeping the states as metrics and deduplicating at the metric lane (a change-only
  metric is a property by another name, and would fork the lane rule); encoding the states as
  `0`/`1` text (a word reads as itself in the console, a log line and `jq`, a digit needs a legend);
  a node-side last-state cache (point 5).
