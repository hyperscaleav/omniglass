---
title: "A node reports its schedule health on its own platform component (#1625)"
date: 2026-11-09
refs: [1625, 1094, 1641, 1659, 1660]
---

The first slice of #1094 builds the platform primitive and its first consumer. The boot seed creates
one reserved, unplaced `platform` system, and registering a node gives it an `omniglass-node`
component there, named on `node.platform_component_id`; the Storage Gateway refuses every rename,
retype, move, delete or membership change of either with a typed error the API answers as 409. Each
tick the node sends a node-self block (`TelemetryBatch.node_self`) with its pass duration, the worst
lateness and gap excess of its due runs, its spool depth, its held sessions by state and its Go
process figures, and once per report window its overrun, skip, drop and publish-failure counts,
zero when quiet; the server lands the block on the node's platform component under the platform writer class,
refusing any key the node does not own. `pollSchedule.due` now reports the due time it booked and the
intervals it skipped instead of rebooking silently, so a 45 s poll on a 30 s heartbeat reads a 15 s
gap excess. The server writes every claimed node's `checkin-age` every 15 s through a new
`RecordPlatformMetrics` gateway write, and the age keeps growing while a node is silent. The keys are
rendered on the collection page from the code that declares them; the decisions are
[ADR-1625](/architecture/decisions/#adr-1625-a-node-reports-itself-on-its-platform-component-and-platform-keys-take-no-prefix)
and an amendment to ADR-0161. The lab run with console captures is #1660, and folding the drop counts
into #1641's uniform loss keys is #1659.
