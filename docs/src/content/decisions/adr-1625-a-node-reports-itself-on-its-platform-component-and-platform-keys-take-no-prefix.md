---
title: "A node reports itself on its platform component, and platform keys take no prefix"
date: 2026-11-09
status: Accepted
summary: "Accepted: the first slice of #1094 builds the platform primitive. The boot seed creates one reserved, unplaced `platform` system; registering a node gives it an `omniglass-node` component there, named on the node row, which an operator may relabel and never reshape (a typed refusal, 409). A node reports itself in the node-self block of its tick batch, which names no owner and lands on its own platform component under the platform writer class, for its declared keys only; the server writes every claimed node's `checkin-age` on a 15 s timer. Platform keys take no prefix (amending ADR-0161 point 6). Only an all-scope read sees the platform rows. Each #1094 slice ships its own PR."
refs: [1625, 1094, 161, 1641, 1634, 1626, 1627, 1628, 1659]
---

- **Date:** 2026-11-09 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [nodes](/architecture/nodes/), [metrics and properties](/architecture/properties/), [glossary](/architecture/glossary/), [storage](/architecture/storage/)
- **Decision:**
  1. **Platform keys take no prefix.** #1094's ruling, amending
     [ADR-0161](#adr-0161-every-key-has-one-writer-class-held-at-the-storage-gateway) point 6: a
     platform key is reserved by its declaration in the `platform` map in `internal/writerclass`, as
     `health` is, and the collision point 6 names needs operator data that, before the first external
     deployment, does not exist. The node-self keys are a declared subset of that map
     (`writerclass.NodeSelf`), so the node may write them and no other platform key.
  2. **The platform rows are unplaced, and only an all-scope read sees them.** The platform system and
     every node's component have no location, so they resolve through the unplaced-root path and a
     location-scoped operator does not see them. `internal/scope` gains no special case. This is the
     slice's reading of #1094: the platform is the operator of the whole install's concern, not a
     site's.
  3. **Node registration owns the node's component.** `CreateNode` creates the component in its own
     transaction and names it in `node.platform_component_id`; `DeleteNode` removes it. A node
     registered on a database the boot seed has not run on gets no component, and the next boot's
     `EnsurePlatform` adopts it. A node whose name an operator's unplaced component already holds is
     refused at registration. A boot never refuses: a release meeting an operator's unplaced system
     already named `platform` names the platform system `platform-2` (it is found by its reserved type,
     never its name), and a node adopted beside an operator's component of its name gets `<node>-2`.
  4. **The node never names an owner.** The node-self block (`TelemetryBatch.node_self`, field 15)
     lands on the publishing node's platform component, resolved from the node row. A batch that sets
     an owner is refused as every node batch with an owner is; a node-self sample that names an
     identity drops the block with a warning; a node with no platform component, or a key outside the
     node-self set, refuses the block alone while the tick's task groups land. A platform key in a task
     group, a push, a command or a calc is refused by the writer class.
  5. **How the schedule is read.** `pollSchedule.due` returns the booked due time and the whole
     intervals it skipped. Gap excess is the absolute deviation of a run's actual gap from its
     interval, early or late, so heartbeat rounding reads the same either way (a 45 s poll on a 30 s
     heartbeat reads 15 s every run); a task with no interval of its own (a probe with the probe
     interval unset) is measured against the heartbeat. Lateness of a run the schedule had no booking
     for is measured from the tick's start. A dropped tick is a gap between ticker fires above 1.5
     heartbeats. `tick-duration` and `tick-overrun-count` describe the serial loop and #1634 redefines
     them. Amended when this met the dispatcher
     ([ADR-1634](/architecture/decisions/#adr-1634-each-poll-and-probe-runs-on-its-own-interval-from-a-dispatcher-of-its-own)):
     the figures come from the dispatcher's heartbeat window. `tick-duration` is its longest pass,
     `tick-overrun-count` the dues that met their run in flight, `schedule-skip-count` every due it
     passed over, `schedule-lateness` its latest dispatch, and `schedule-gap-excess` the worst gap
     between two scheduled starts of a task off its interval. With no heartbeat rounding left, a poll
     keeping its interval reads 0.
  6. **Counts go once per window and only when nonzero.** Overruns, skipped intervals, spool drops and
     publish failures (refused, not acked, or failed to encode) follow the `poll-failure-count`
     convention on the node's report window; the each-tick figures go every tick, and
     `session-held-count` sends one sample per session state, zero included, so a state that empties
     reads 0. The drop counts are the node hop of #1641's loss ratio in count form; #1659 folds them
     into its uniform `delivery-*` keys.
  7. **The server writes check-in age.** A timer beside the settlement sweep writes `checkin-age` every
     15 s for every claimed node: seconds since its last heartbeat, or since its claim before its
     first, clamped at 0. An unclaimed node has no series. It writes through `RecordPlatformMetrics`,
     whose rows carry their component (not the spec's `(componentID, rows)` form), one transaction per
     component, a component deleted between the read and the write dropped; #1626, #1627 and #1628
     reuse it.
  8. **One PR per slice in this campaign.** The architect's ruling for #1623's work: each #1094 slice
     ships and is reviewed as its own PR rather than rolled up into one loop PR (ADR-0074's default).
- **Context:** #1094 asks Omniglass to monitor itself as a platform system. The first figure the lab
  needs is whether a node keeps up with its schedule, which the node only logged. Building that one
  figure as a one-off would leave #1626 to #1628 and #1644 each inventing where a platform figure
  lives and who may write it; the platform system, the node's component, the node-self lane and the
  platform write are the primitive they consume.
