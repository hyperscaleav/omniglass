---
title: "Telemetry is published through JetStream with an ack, a minted Nats-Msg-Id, and a bounded spool"
date: 2026-10-26
status: "Accepted"
summary: "Every telemetry publish (the node lane and the push lane, the webhook path with it) is a JetStream publish that waits for the stream's ack, carrying a `Nats-Msg-Id` minted once per batch (a UUIDv7, never a content hash, never the node's name) and kept across every retry. The push route answers 202 only after the ack and 503 after three attempts. The node's send only queues; a shipper keeps a window of 32 batches with the stream, in order, over a connection that reconnects without limit and buffers nothing across a reconnect, and holds what the stream has not acked in a bounded in-memory spool (10,000 batches, 16 MiB, 12 hours, oldest dropped and told once; a batch the bus can never take dropped) for the next tick. OG_TELEMETRY keeps a five-minute duplicate window; the consumer double-acks; the receipt (ADR-0311) is the guarantee past the window"
refs: [453, 454, 430, 426]
---

- **Date:** 2026-10-26 | **Status:** Accepted | **Pages:** [messaging](/architecture/messaging/), [nodes](/architecture/nodes/), [workers](/architecture/workers/), [api](/architecture/api/)
- **Decision:** (#453, absorbing #454.)
  1. **A publish is done when the stream says so.** Both lanes publish under the batch's id through
     `collection`'s helpers: the push route (and the webhook route, which publishes the same way)
     through `PublishBatch`, a JetStream publish that waits (bounded) for the stream's ack, retrying a
     failed attempt twice, backing off, and answering 503 when none is acked, so a 202 is the stream's
     word that `OG_TELEMETRY` stored the batch; the node through `StartBatch`, an asynchronous
     publish whose ack its spool waits on (point 4).
  2. **The `Nats-Msg-Id` is minted, once per batch.** A UUIDv7, minted when the batch is built and
     kept across every attempt, so an attempt the stream stored but whose ack was lost makes the retry
     a duplicate. Random in its tail rather than a hash of the content, because two batches a device
     really sent twice are two batches; time-ordered in its head, so each publisher's receipts append
     at the right edge of the receipt index rather than dirtying a random page of it per batch; and
     free of the node's name, so it does not wait on bus identity moving to `principal_id` (#426).
     The server keys its receipt by the subject as well, so a publisher cannot claim another's id.
  3. **The stream remembers an id for five minutes** (`Duplicates` on `OG_TELEMETRY`): a retry inside
     it (the next tick, the route's retry) is answered with the first copy's ack and stored once. A
     retry past it is stored again and held by the receipt
     ([ADR-0311](#adr-0311-a-telemetry-batch-lands-once-in-one-transaction-under-its-receipt)), so the
     window saves work and the receipt is the guarantee. The consumer double-acks, and a lost ack
     costs only a redelivery the receipt turns into a no-op.
  4. **The node spools what the stream has not acked.** Every node telemetry publish goes through a
     bounded, in-memory spool. A send only queues; one shipper goroutine keeps a window of up to 32
     batches with the stream, removing each as it is acked, so no caller waits on the stream and the
     node ships about 32 batches per round trip. A batch the stream has not acked is held and shipped
     on the next tick, under its id, and the batches with the stream behind it are sent again with it
     (a copy already stored is a duplicate). After a failed attempt the spool holds, so a node that
     cannot reach the bus pays one failed round per tick rather than one per batch. A batch the bus
     can never take (over its payload limit, a malformed subject) is dropped and told, never held,
     since held at the head it would block every batch behind it. Past 10,000 batches, 16 MiB or 12
     hours the oldest batch drops (never one with the stream); the drop is told once at its start and
     once, with the count, when the spool drains, and the run loop forgets the transitions it
     remembers as sent so the next tick sends current state again. The age cap sits inside the
     receipt's 24 hours, so a stored batch whose ack was lost cannot return after its receipt is
     pruned. A one-shot run ends only once its batches are acked; a stopping node drains for five
     seconds.
  5. **The window stays in order, over a connection that never gives up.** One connection delivers in
     the order publishes were made, and the node buffers nothing across a reconnect
     (`ReconnectBufSize` -1): a publish during an outage fails at once and the spool holds it, so a
     later batch never lands ahead of an earlier one that failed. That order is what the property
     lane's transition guard, which compares a value with its series' latest, relies on. The
     connection reconnects without limit, so an outage longer than the client's default budget (60
     attempts) ends with the spool shipping again rather than a node that runs and never ships.
- **Context:** Both publishers used core NATS (#453): a node's publish returned before anything was
  stored, and the push route's flush proved only that the bytes reached the server, so a 202 could
  stand for a batch the stream never kept, and a node behind a flapping link lost every tick it
  could not deliver, silently (#454). The spool is memory, as the nodes page designed (not durable at
  the edge): a node restart loses what it held, and the cascade-resolved buffer knobs and a
  `node.buffer` sample stay design.
