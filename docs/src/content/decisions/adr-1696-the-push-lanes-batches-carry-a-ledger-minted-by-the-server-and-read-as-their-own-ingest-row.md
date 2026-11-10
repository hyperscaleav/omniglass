---
title: "The push lane's batches carry a ledger minted by the server and read as their own ingest row"
date: 2026-11-10
status: "Accepted"
summary: "Accepted: the bus server stamps every batch it publishes on the push subject (the push route's and the webhook listener's) with the ADR-1641 ledger, one boot per server process, a dense seq, and resolved the seq below, publishing one batch at a time. A publish the stream refuses takes its items back off the offsets and keeps its seq spent. The push lane's receipts carry no node, so the delivery read reconciles each lane apart and prints a lane column; the end-to-end line composes the node lane alone, and the missing footer names each lane's own meaning."
refs: [1696, 1641, 1623, 1659, 1691, 1024]
---

- **Date:** 2026-11-10 | **Status:** Accepted | **Pages:** [messaging](/architecture/messaging/#accounting-every-item-the-loss-ratio), [lab](/guides/lab/#reading-the-delivery-table)
- **Decision:** (#1696, under #1623, extending [ADR-1641](#adr-1641-every-telemetry-item-is-accounted-on-its-batchs-receipt-and-read-as-a-loss-ratio).)
  1. **The server mints the push lane's ledger as it publishes.** The stamp is taken in the bus
     server's `PublishTelemetry`, the one path the push route and the webhook listener share, not in
     the push route, so a webhook's batches are accounted too. Its boot is minted once per server
     process; each server in a cluster is its own boot.
  2. **One publish at a time, resolved the seq below.** Publishes take a turn (a wait the caller's
     context can abandon, bounded at one ack timeout, after which the caller is answered `503`;
     either way it mints nothing), and each waits for the stream's ack before the
     next mints, so the seq and offsets are dense in the stream's order and every batch below the
     one being minted has its answer: `resolved = seq - 1`.
  3. **A refused publish takes its items back.** A publish the stream does not ack answers the
     pusher `503`, and the pusher still holds its items, so they are not the lane's loss: the
     server withdraws them from the offsets. The seq stays spent, so a copy the stream did store
     (its ack lost) lands under its own seq and never under a later batch's.
  4. **The receipt keys the push lane by no node.** A push receipt carries the server's boot and
     a null node; the consumer records the ledger and checks a declared shortfall exactly as it
     does a node's batch. A batch with no ledger (a publisher before this build) leaves those
     columns empty and is skipped by the read, as before.
  5. **Each lane reads apart.** The read reconciles the node lane and the push lane separately and
     tags each row with its `lane` (in the JSON too). The push lane crosses the ingest hop alone, so
     its row is its own end to end; `delivery_end_to_end` and the end-to-end line compose the node
     lane alone, so a push-heavy lab cannot dilute a node lane's loss or the reverse.
  6. **Missing means something narrower on the push lane.** A batch the ingest Terms whole records
     its Term receipt (ADR-1691) under the server's boot and reads under its cause, so a hole there
     is a batch the ingest could not decode, or one the stream gave up before it was read; the
     footer says so per lane instead of one line for both.
- **Context:** The push lane (an integration's `POST /telemetry:push`, a webhook) recorded receipts
  with no ledger, so a Termed pushed batch or a refused item vanished from the delivery read: a lab
  reading 0 said nothing about what was pushed to it.
- **Thin cuts:**
  - Serializing publishes caps the push lane at one stream round trip a batch, server-wide; the
    stream is local (the embedded server), so the cap is well above the lane's traffic today. A lane
    that outgrows it shards the publisher, one boot per shard. A stalled stream holds the turn
    through every attempt of the publish at the head, so the turn's wait is bounded: a caller
    behind it hears `503` within one ack timeout, not that stall times its place in the queue.
  - A refused publish whose copy the stream did store (the ack lost in flight) lands with offsets
    that overlap its next neighbor's. Read alone it counts correctly, but when the batch above it is
    also missing, that hole reads up to the withdrawn count short. Both must happen in one window,
    on an embedded stream.
  - A server stopped mid-window leaves its push boot with no later batch, so it reads unsettled with
    no ratio, not missing.
- **Not chosen:**
  - **Stamping in the push route.** Webhooks publish through the same bus path but not the route,
    and would stay unaccounted.
  - **A ledger per pusher (per token or service account).** It would need the pusher's own seq to
    be trustworthy, which an external integration's is not; the server's publish order is the one
    sequence omniglass controls.
  - **Composing push rows into end to end.** The two lanes cross different hops; one product over
    both would mix a one-hop lane into a multi-hop one and move as traffic shifts between them.
  - **Counting a refused publish as missing.** The pusher was told and still holds the items; reading
    them as lost would charge omniglass for loss the protocol already handed back.
