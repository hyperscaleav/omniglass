---
title: "The server reports its ingest lag, queue depths and runtime on its own platform component (#1628)"
date: 2026-11-09
refs: [1628, 1094, 1627, 1625, 419, 1517]
---

The server now measures its own pipeline and lands it on its `omniglass-server` platform component
each report window, through the window recorder #1627 built. Ingest lag is read at commit, server
side: the server's clock once a batch's transaction has committed minus each sample's `ts`, as the
window mean (`ingest-lag`) and the worst (`ingest-lag-max`), beside the batches and samples landed. A
node clock out of step with the server's reads as lag, a negative one kept as read (#419). The
telemetry consumer's backlog, in-flight and redelivered batches, its lane's busy fraction, its drops by
reason and its unconfirmed acks land too, with the worklist pulls by outcome, the largest worklist
reply, and the heartbeat, command pull and command report requests and their failures; a heartbeat
write that fails now logs one WARN a node instead of nothing. The calc tick reports its backlog, rows
served and pass time, the outbox its depth and the age of its oldest due row, and the settlement
tick's two sweeps their time, passes and failures. Every failure count has its attempts beside it, so
each loss reads as a ratio where 0 is healthy. The server reads its Go runtime through the same
sampler a node does, now in `internal/platformmetric`, which gains `process-cpu-seconds` and
`process-fd-count` for both. A second official event rule, `ingest-lag-high`, warns on the server's
component when `ingest-lag-max` holds over 30 s for 2 minutes. Moving the lab's lag measurement onto
these figures is #1517. The decisions are
[ADR-1628](/architecture/decisions/#adr-1628-ingest-lag-is-the-commit-time-minus-the-sample-ts-read-on-the-server).
