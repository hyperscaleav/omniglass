---
title: "Node worklists survive a fleet-wide start (#1577)"
date: 2026-11-08
refs: [1577, 1571, 1579, 1583, 1581, 1584, 1586, 1587]
---

At 1,000 systems the scale lab's headquarters node never got its worklist: its 3.3 MB reply was over
the bus's 1 MB limit, the server answered every pull in series, and a node whose first pull timed out
exited and was restarted by Docker. Three tunes, per the architect's narrowed decision on
[ADR-1577](/architecture/decisions/#adr-1577-a-nodes-worklist-is-compressed-served-by-a-bounded-responder-and-retried-without-exiting),
and no rebuild. How the pull now behaves is on the [nodes](/architecture/nodes/#pulling-the-worklist)
page.

- **The node never exits on a pull.** The first pull and every later one retry with jittered backoff
  (1 s to a 30 s cap) and keep the last good worklist; the pull runs on its own interval
  (`node run --worklist-every`, 30 s by default, 5 s in the lab) instead of every heartbeat. The
  server answers a pull it cannot read in full with the reason and no body, so a database blip never
  reaches the node as a short worklist. `TestATransientStorageErrorLeavesTheNodesSessionsUntouched`
  holds that: three failed pulls against a node holding a live session, and the session is still one,
  still up, with no transition logged.
- **The reply is gzipped past 64 KB** under `Content-Encoding: gzip`. Headquarters at 1,000 systems is
  3,273,096 B of JSON and about 463 KB on the wire.
- **A bounded responder.** A pool of 8 workers (held under the database pool less one connection for
  ingest) answers pulls, and a pull whose `Omniglass-Timeout` budget ran out while it queued is
  dropped unread. Every outcome is counted (`Server.WorklistCounts`).

**Measured on the worker (12 cores).** `make scale-lab SYSTEMS=1000`: all 18 sites up 199 s after the
command (17 of 18 after 10 minutes before), and no node container restarted. The fleet test, 18 nodes
pulling the 1,000-system corporate lab at once over the real bus, serves every pull in 1.64 to 1.72 s
with none expired. That misses the 1.4 s target: HQ's own build, about 1.2 s on one worker, is the
floor, and the set-based reads (#1578, #1581) are what lower it.

The 1,000-system column of the [scale measurement](#omniglass-lab-measure-prints-the-scale-labs-table-1556)
is still partial: the bring-up now fits, but `lab measure` at 1,000 systems fails on its ingest lag
read (#1587), so only the figures it printed before that are recorded there.

Filed from it: paging and the normalized wire, if HQ at 3,000 systems is still over 512 KB gzipped
(#1583); the set-based secret, pin and rejection reads (#1581); an "unchanged" answer from the
worklist's input generation (#1584); the materialized worklist as the target, with its triggers
(#1586); and the measure's lag failure (#1587).
