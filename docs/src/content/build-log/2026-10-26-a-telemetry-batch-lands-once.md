---
title: "A telemetry batch lands once, however many times it is delivered (#311, #453)"
date: 2026-10-26
refs: [311, 453, 454, 455, 457, 430, 825]
---

The delivery half of telemetry integrity for 0.1.0 (#825, #430). A sample a node collects is now
written exactly once as an outcome, and a caller who gets a `202` from the push route can stand on
it.

The ingest consumer lands a batch through one gateway call, `LandBatch`
(`internal/storage/land_batch.go`), that writes its node self-logs, its log lines and its metric,
event and property rows in one transaction whose first statement inserts the batch's receipt
(`telemetry_receipt`, keyed by the subject and the batch's `Nats-Msg-Id`). A failure part way writes
nothing; an id that already landed writes nothing at all, so a redelivery after a lost ack, a
consumer that died between its commit and its ack, and a publisher's retry past the stream's
duplicate window all land each row once, log lines included
([ADR-0311](/architecture/decisions/#adr-0311-a-telemetry-batch-lands-once-in-one-transaction-under-its-receipt)).
The property lane's transition guard moved into the insert as a conditional write, held per series
by a transaction lock, so concurrent batches never write a value twice (#455); since the guard is right
only if a series lands in order, the consumer keeps one batch in flight, redelivering a transient
failure before anything behind it and dropping a refused write at once. The batch
takes every lane's rule rows and calc marks before any lane writes, the platform's lock order held
for the whole transaction, and the gateway replaces a NUL in any lane's text before the insert, so a
batch landing all or nothing can neither deadlock a rule edit nor be lost to one stray byte. Receipts
are kept a day and pruned hourly. The boot seed now refuses to ship a name two registries share
(#457).

Both publishers moved to JetStream: the push and webhook routes wait for the stream's ack
(`collection.PublishBatch`) and the node keeps a window of 32 batches with it (`collection.StartBatch`),
each carrying a `Nats-Msg-Id` minted once per batch (a UUIDv7) and kept across retries, and
`OG_TELEMETRY` keeps a five-minute duplicate window. The push route answers 202 only after the ack and
503 after three attempts. The node's send only queues; a shipper goroutine holds what the stream has
not acked in a bounded spool (`internal/node/spool.go`: 10,000 batches, 16 MiB or 12 hours, the
oldest dropped and told, a batch the bus can never take dropped rather than blocking the rest) and
ships it on the next tick, over a connection that reconnects without limit and buffers nothing across
a reconnect, so the window stays in order
([ADR-0453](/architecture/decisions/#adr-0453-telemetry-is-published-through-jetstream-with-an-ack-a-minted-nats-msg-id-and-a-bounded-spool)).

Proven against a real Postgres and a real JetStream: a batch whose ack is lost after its commit is
redelivered and lands once; a consumer killed inside a landing, before its commit and after it,
replays cleanly into a restarted server over the same stream store; a batch replayed under one id
inside the window is stored once, and past it (a fresh stream) is held by its receipt; the push route
answers 503 with no stream to store the batch; and a node publish whose ack is lost twice is retried
on the next ticks under its id and lands once, a self-log line and a task's sample alike; a node
behind an outage longer than any reconnect budget ships what it held once the bus is back; and a
batch over the bus's payload limit is dropped and told while the batch behind it lands. At the
gateway: a batch id lands once (eight concurrent deliveries land it once), a refused row leaves
nothing behind, concurrent batches carrying one verdict write each transition once, a transient
failure redelivered keeps a flip pair in order and a refused batch is dropped on its first delivery, a batch carrying
NUL on every lane lands, and a batch waiting on a rule row an edit holds holds no health lock. In the
spool: a send never waits on a stream 50 ms away, and 120 batches ship in order in a few round trips.
