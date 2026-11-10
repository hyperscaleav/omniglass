---
title: "Ingest lag is the commit time minus the sample ts, read on the server"
date: 2026-11-09
status: "Accepted, amended by ADR-1659"
summary: "Accepted: the server reports its pipeline on its own platform component each report window through the #1627 recorder. Ingest lag is the server's clock when a batch's transaction has committed minus each sample's ts, read on the server: the window mean over each batch's newest and oldest sample is `ingest-lag`, the worst oldest is `ingest-lag-max`, an untimed sample counts toward throughput and never toward lag, and a negative lag is kept as the evidence of a node clock ahead (#419). Every failure count is paired with its attempts so each loss reads as a ratio where 0 is healthy; the server and a node read the `process-*` figures through one runtime sampler; the seeded `ingest-lag-high` rule warns at 30 s held 2 minutes."
refs: [1628, 1094, 1625, 1627, 419, 1517, 1577, 1630, 1641, 1659]
---

- **Date:** 2026-11-09 | **Status:** Accepted, amended by ADR-1659 | **Pages:** [messaging](/architecture/messaging/), [workers](/architecture/workers/), [scaling](/architecture/scaling/), [collection](/architecture/collection/), [alarms and actions](/architecture/alarms-actions/)
- **Decision:**
  1. **Ingest lag is landing time at commit minus the sample's `ts`, server side.** The consumer reads
     the server's clock once a batch's transaction has committed and subtracts each sample's
     effective `ts` (its own, else its group's, else its batch's, the rule its row lands by). The
     figure covers the whole trip, from the node's stamp through its spool, the publish, the stream,
     the consumer's queue and the landing. The lab's figure, read after the fact from
     `pg_xact_commit_timestamp`, stays the lab's until #1517 moves it onto this one.
  2. **Two figures a window, from two samples a batch.** Each committed batch contributes its newest
     and its oldest sample to the window mean `ingest-lag`, and its oldest to the window max
     `ingest-lag-max`; no per-sample observation, so a batch of fifty costs what a batch of one does.
     A sample with no `ts` anywhere is stamped at ingest and was never late: it counts into
     `ingest-sample-count` and not toward lag. A batch with no samples observes nothing. Only what
     landed counts: a redelivery or replay its receipt held to a no-op (`Landed` false) adds no
     batch, sample or lag, or every lost ack would read as a batch 30 s late, and a node tick's
     landing is only its groups that landed, never one the fence or the gateway dropped.
  3. **A negative lag is kept.** Lag subtracts two clocks, the node's and the server's. A node clock
     ahead reads short or negative, one behind reads long. Clamping at 0 would hide the one signal a
     skew gives, so the value is kept as read; the clock is #419's.
  4. **Every loss is a ratio where 0 is healthy, so counts come in pairs.** The spec named failure
     counts alone; each now has its attempts beside it: `ingest-batch-count` for
     `bus-telemetry-drop-count` (the drop ratio is drops over drops plus committed) and
     `bus-telemetry-ack-failure-count`, a new `bus-request-count` per path for
     `bus-request-failure-count`, and a new `sweep-count` per sweep for `sweep-failure-count`. The
     worklist outcomes are their own denominator (every outcome but `served` over the sum).
     Amended (#1659): `bus-telemetry-drop-count` is retired; the server hop's drops read as its
     `delivery-loss-ratio` per item kind, items rather than batches, [ADR-1659](/architecture/decisions/#adr-1659-a-hop-writes-its-delivery-in-out-and-loss-ratio-per-kind-every-window).
  5. **A fourth drop reason, `malformed`.** The spec named `refused`, `metadata` and `max-deliver`. A
     batch that will never land as sent (undecodable, an API push with no usable owner, a node batch
     naming an owner or a top-level task) was terminated silently too, so it counts as `malformed`
     rather than vanishing from the ratio. Amended (#1659): the reasons went with the count; a
     Termed batch's items count as in and not out on the server hop, and an undecodable one names
     no items, so it counts nothing there and its items read as `missing` on the ledger.
  6. **Worklist pulls are window deltas of the running totals.** The counters #1577 built stay
     running totals; the window reading keeps its previous read and lands the difference by outcome,
     never negative (a lower read is a fresh tally) and only for outcomes that moved.
  7. **A failing heartbeat write warns once per run, per node.** The first failure for a node logs
     one WARN, and no other until a heartbeat from that node lands, so a database outage under a
     fleet heartbeating every few seconds is a line a node, not a line a beat; every failure counts.
     The command pull failure counts and does not log; its log line is #1630's.
  8. **One runtime sampler, two processes.** The `process-*` reading moved into
     `internal/platformmetric` and the node and the server both call it. `process-cpu-seconds` is the
     CPU time since the previous read, as `runtime/metrics` estimates it (the runtime's busy time,
     not the kernel's account), so on a node it is per tick and on the server per report
     window; divided by its interval it is cores kept busy. `process-fd-count` reads nothing where
     the platform does not expose it.
  9. **Queues read as the tick and the store see them.** `calc-pending-depth` is the window max of
     each calc pass's depth, every pending mark counted, a debounced one included.
     `outbox-queued-depth` and `outbox-due-age` are one storage read at the window's close, by the
     predicate the sender takes rows by, so a row in its retry backoff counts toward the depth and
     not the age. A read that fails reports nothing that window, never a zero.
  10. **`ingest-lag-high` warns at 30 s held for 2 minutes.** The official rule reads
      `ingest-lag-max` on the server's component: over 30 s for 2 minutes opens a `warning` alarm,
      a window at 30 s or under clears it. The sustain keeps a restart's catch-up or one slow window
      from paging.
- **Context:** #1094 asks the platform to monitor itself, and #1627 built the server's component and
  its window recorder. The pipeline had no inside view: ingest lag existed only in the lab, the
  lane's busy time and redeliveries were parsed out of logs, the worklist counters had no reader, and
  the command pull and heartbeat failures were silent. These figures are the evidence for whether a
  bigger box or a second consumer is the fix ([scaling](/architecture/scaling/#scale-up-or-scale-out-what-the-figures-say)).
  Folding the drops into the one delivery loss ratio is #1641.
