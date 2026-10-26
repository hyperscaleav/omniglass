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
by a transaction lock, so it no longer rests on the consumer dispatching serially (#455). Receipts are
kept a day and pruned hourly. The boot seed now refuses to ship a name two registries share (#457).

Both publishers moved to JetStream (`collection.PublishBatch`): the node's every telemetry publish
and the push and webhook routes wait for the stream's ack, carrying a `Nats-Msg-Id` minted once per
batch and kept across retries, and `OG_TELEMETRY` keeps a five-minute duplicate window. The push
route answers 202 only after the ack and 503 after three attempts. The node holds what the stream
has not acked in a bounded spool (`internal/node/spool.go`: 10,000 batches or 16 MiB, the oldest
dropped and told) and ships it on the next tick before new work
([ADR-0453](/architecture/decisions/#adr-0453-telemetry-is-published-through-jetstream-with-an-ack-a-minted-nats-msg-id-and-a-bounded-spool)).

Proven against a real Postgres and a real JetStream: a batch whose ack is lost after its commit is
redelivered and lands once; a consumer killed inside a landing, before its commit and after it,
replays cleanly into a restarted server over the same stream store; a batch replayed under one id
inside the window is stored once, and past it (a fresh stream) is held by its receipt; the push route
answers 503 with no stream to store the batch; and a node publish whose ack is lost twice is retried
on the next ticks under its id and lands once, a self-log line and a task's sample alike. At the
gateway: a batch id lands once (eight concurrent deliveries land it once), a refused row leaves
nothing behind, and sixteen concurrent batches carrying one unchanged verdict write it once.
