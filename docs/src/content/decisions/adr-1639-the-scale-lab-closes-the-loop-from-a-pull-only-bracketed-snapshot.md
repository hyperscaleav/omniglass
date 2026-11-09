---
title: "The scale lab closes the loop from a pull-only bracketed snapshot of each endpoint"
date: 2026-11-09
status: Accepted
summary: "Accepted: every simulated endpoint counts what reached it and how it answered, and every node counts what it sent, in matched units that never reset on read; the scale lab pulls both at a window's start and end, each pull a bracket of three (the simulator, the node, the simulator again), and reconciles them with the telemetry receipts hop by hop into named gaps, each a lower bound with its masking term beside it. Rejected: the hit stream and node-self metric rows as the source, and a latency sketch that cannot be summed across nodes."
refs: [1639, 1640, 1661, 1662, 1663, 1664, 1632]
---

- **Date:** 2026-11-09 | **Status:** Accepted | **Pages:** [simulation](/architecture/simulation/#what-an-endpoint-observed), [the lab](/guides/lab/#every-request-reached-its-endpoint-and-was-answered), [test-driven](/contributing/test-driven/#measuring-a-running-lab)
- **Decision:**
  1. **Pull-only, bracketed snapshots.** The simulator and the node each keep cumulative counters
     that never reset on read, and the lab pulls them over the bus through
     `GET /nodes/{name}/simulation/observation`, which reads the simulator, the node, then the
     simulator again. A request in transit between two reads falls inside the bracket, so it can
     only ever be a masking term, never a gap. The hit stream was refused as the source: it is a
     once-a-second push the server keeps in memory and folds per device, so a window's edges would
     be wherever a publish happened to fall, and a lost publish would read as a lost request. Node
     self-metrics as rows were refused too: they land a report window late, on the ingest path the
     loop is checking, so the instrument would depend on the thing it measures.
  2. **Matched units on both ends.** A connection, a request (one exchange, settled by exactly one
     of `answered`, `refused`, `error`, `dropped`, `withheld`) and a stream, counted where the
     bytes leave the node and where they reach the endpoint. `withheld` is the simulator's own
     planted loss, kept apart so a fault never reads as loss. The counters are raised and read in a
     fixed order (in flight before received, outcome before leaving flight; the reader takes
     received, in flight, outcomes) so no snapshot shows a false gap, and the hot path is a fixed
     row per device and atomic adds, with no allocation.
  3. **A summable histogram.** Service time is a fixed log-linear histogram (eight buckets to a
     power of two of microseconds) in a shared `obsstat` package, beside an in-flight high-water
     ring of ten-second epochs; histograms from every node merge by summing buckets. A t-digest
     was refused because it cannot be summed across nodes without its own error.
  4. **Every gap is a lower bound with its mask beside it.** The hops are `connect-not-accepted`,
     `sent-not-received`, `received-not-settled`, `answered-not-returned`,
     `published-not-landed`, `spool-dropped` and `unhit-endpoint`; each reads positive only where
     something was lost, and what could hide a loss is printed apart as `masked`, never subtracted
     silently. Traffic on a protocol the node does not tally yet (sessions and streams, #1661) is
     an `untallied` mask on `answered-not-returned`, and an answer the node gave up on is counted
     late only up to the residual, so a timeout never hides more than the gap it explains.
  5. **Receipts on the database's clock.** The start mark reads the database's clock and then the
     nodes; the end mark reads the nodes and then the clock, so the receipts that clock bounds fall
     inside the reads. A node's receipts are counted by `starts_with` on its subject prefix (a node
     name's own characters stay literal) from 30 s before the window (the bus's acknowledgement
     wait), and the receipts in that margin or after the end are the mask. The window is refused
     past 23 hours, inside the server's 24-hour receipt prune.
  6. **Poll targets from the worklist's own rule.** The enabled poll targets are read in SQL with
     `driver.IsBaked`'s test, each at the interval its node actually runs it (a driver poll at its
     function's schedule, a reachability probe at the node's probe interval, both read off the node
     container's command), and only targets due at least twice in the window count. The report
     prints each protocol's target count beside what it received.
  7. **A measurement, not a failed run.** When the loop cannot be read, the lab measurement still
     reports every other figure and names why under `endpoints_not_measured`. The gate on the gaps
     being zero is #1640's, not this measurement's.
- **Divergences and follow-ups:**
  - Unhit endpoints are read since the simulator started, from a sample of device ids capped at
    twenty per protocol, with the rest a mask; checking every target inside the window is #1663.
  - An applied impairment marks the whole node `impaired`, every gap; attributing it to the
    protocol it touches, counting its changes, and the kernel-impairment run are #1662.
  - The node tallies http, snmp and tcp reachability; session and stream exchanges are #1661.
  - The run that plants a spool drop and reads `spool-dropped` is #1664.
  - The five worst overlapping faces are counted since each simulator started, not windowed.
  - The bus's acknowledgement wait and receipt horizon are mirrored as local constants in the lab
    package rather than imported: both are the bus's own unexported tuning, and the lab does not
    import the bus.
  - Every `ScaleSample` carrying the report waits on #1632's sampling mode.
- **Context:** a feature push should never quietly cost the fleet a request. The hit stream says a
  device was touched; it cannot say every request a node sent arrived and was answered, or name
  the hop that lost one. This is the instrument #1640 gates on.
