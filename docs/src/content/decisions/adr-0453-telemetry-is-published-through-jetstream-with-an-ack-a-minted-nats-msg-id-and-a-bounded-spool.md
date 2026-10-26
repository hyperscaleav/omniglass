---
title: "Telemetry is published through JetStream with an ack, a minted Nats-Msg-Id, and a bounded spool"
date: 2026-10-26
status: "Accepted"
summary: "Every telemetry publish (the node lane and the push lane, the webhook path with it) is a JetStream publish that waits for the stream's ack, carrying a `Nats-Msg-Id` minted once per batch (128 random bits, never a content hash, never the node's name) and kept across every retry. The push route answers 202 only after the ack and 503 after three attempts. The node holds a batch the stream has not acked in a bounded in-memory spool (10,000 batches, 16 MiB, oldest dropped and told once) and ships it on the next tick before new work. OG_TELEMETRY keeps a five-minute duplicate window; the consumer double-acks; the receipt (ADR-0311) is the guarantee past the window"
refs: [453, 454, 430, 426]
---

- **Date:** 2026-10-26 | **Status:** Accepted | **Pages:** [messaging](/architecture/messaging/), [nodes](/architecture/nodes/), [workers](/architecture/workers/), [api](/architecture/api/)
- **Decision:** (#453, absorbing #454.)
  1. **A publish is done when the stream says so.** Both lanes publish through one helper,
     `collection.PublishBatch`, a JetStream publish that waits (bounded) for the stream's ack. The
     push route (and the webhook route, which publishes the same way) retries a failed attempt twice,
     backing off, and answers 503 when none is acked: a 202 is the stream's word that `OG_TELEMETRY`
     stored the batch.
  2. **The `Nats-Msg-Id` is minted, once per batch.** 128 random bits, minted when the batch is built
     and kept across every attempt, so an attempt the stream stored but whose ack was lost makes the
     retry a duplicate. Random rather than a hash of the content, because two batches a device really
     sent twice are two batches; and free of the node's name, so it does not wait on bus identity
     moving to `principal_id` (#426). The server keys its receipt by the subject as well, so a
     publisher cannot claim another's id.
  3. **The stream remembers an id for five minutes** (`Duplicates` on `OG_TELEMETRY`): a retry inside
     it (the next tick, the route's retry) is answered with the first copy's ack and stored once. A
     retry past it is stored again and held by the receipt
     ([ADR-0311](#adr-0311-a-telemetry-batch-lands-once-in-one-transaction-under-its-receipt)), so the
     window saves work and the receipt is the guarantee. The consumer double-acks, and a lost ack
     costs only a redelivery the receipt turns into a no-op.
  4. **The node spools what the stream has not acked.** Every node telemetry publish goes through a
     bounded, in-memory spool: shipped in order, a batch the stream has not acked held and shipped on
     the next tick before that tick's work, under its id. After a failed attempt the spool holds, so a
     node that cannot reach the bus pays one failed attempt per tick rather than one per batch. Past
     10,000 batches or 16 MiB the oldest batch drops (never the one in flight); the drop is told once
     at its start and once, with the count, when the spool drains, and the run loop forgets the
     transitions it remembers as sent so the next tick sends current state again. A one-shot run
     ends only once its batches are acked; a stopping node drains for five seconds.
- **Context:** Both publishers used core NATS (#453): a node's publish returned before anything was
  stored, and the push route's flush proved only that the bytes reached the server, so a 202 could
  stand for a batch the stream never kept, and a node behind a flapping link lost every tick it
  could not deliver, silently (#454). The spool is memory, as the nodes page designed (not durable at
  the edge): a node restart loses what it held, and the cascade-resolved buffer knobs and a
  `node.buffer` sample stay design.
