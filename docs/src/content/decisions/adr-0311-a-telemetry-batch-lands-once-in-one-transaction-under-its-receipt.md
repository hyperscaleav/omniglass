---
title: "A telemetry batch lands once, in one transaction under its receipt"
date: 2026-10-26
status: "Accepted"
summary: "The ingest consumer lands each batch (its log lines and its three sample lanes) in ONE transaction that first inserts the batch's receipt, keyed by its subject and Nats-Msg-Id (`telemetry_receipt`): all or nothing, and an id that already landed writes nothing, so a redelivery after a lost ack, a consumer that died after its commit, or a publisher's retry past the stream's duplicate window lands each row once, log lines included. The property lane's transition guard moves into the insert as a conditional write held per series by a transaction lock (the #455 ruling, ratified with the lock it needs under READ COMMITTED), so serial dispatch is no longer a correctness dependency. Receipts are kept 24 hours. The `(series, ts)` natural key is not the mechanism"
refs: [311, 455, 457, 430]
---

- **Date:** 2026-10-26 | **Status:** Accepted | **Pages:** [messaging](/architecture/messaging/), [workers](/architecture/workers/), [storage](/architecture/storage/), [nodes](/architecture/nodes/), [metrics and properties](/architecture/properties/)
- **Decision:** (#311, with #455's ruling and #457's pin folded in.)
  1. **One transaction per batch.** The consumer derives a batch's rows (pure, as before) and lands
     them through one gateway call, `LandBatch`, that writes the node's self-logs, the component's
     log lines, and the metric, event and property lanes in one transaction, retried whole on a
     deadlock abort. A row the gateway refuses fails the batch, so no sink's rows are left behind for
     the redelivery to write a second time.
  2. **The receipt makes it idempotent.** The transaction's first statement inserts the batch's id
     into `telemetry_receipt` (`ON CONFLICT DO NOTHING`); a conflict means the batch already landed,
     and nothing is written. The id is the subject the batch arrived on and its `Nats-Msg-Id`
     ([ADR-0453](#adr-0453-telemetry-is-published-through-jetstream-with-an-ack-a-minted-nats-msg-id-and-a-bounded-spool)),
     so a publisher can claim only ids under its own subject; a batch with no id is keyed by its
     stream sequence under the stream's creation time, which holds across its redeliveries though not
     across a publisher's retry. Two deliveries of one batch at once serialize on the receipt's key.
     The endpoint's freshness stamp (#933) stays after the transaction, so it adds no lock to it.
  3. **The transition guard is the insert's condition.** A property row is inserted only where the
     series' latest row (by `ts`, then `id`) does not hold the same value, and the batch first takes
     a transaction-scoped advisory lock per property series it writes, in sorted order and before any
     mark or rule lock, so the condition reads the committed latest. #455 ruled the conditional write;
     a conditional insert alone still races under READ COMMITTED (each transaction's statement
     snapshot misses the other's uncommitted row), so the lock is what makes it hold under concurrent
     dispatch. The consumer still dispatches serially, but nothing depends on it.
  4. **Receipts are kept 24 hours** and pruned by the server hourly, in chunks. That is far past the
     stream's redelivery, and past a node's retry of a batch it could not ship, which its spool drops
     after 12 hours ([ADR-0453](#adr-0453-telemetry-is-published-through-jetstream-with-an-ack-a-minted-nats-msg-id-and-a-bounded-spool));
     a copy arriving after it (a push sender's own replay a day later) would land again, the one
     stated residual.
  5. **The shipped registries are disjoint by name** (#457): the boot seed refuses to seed a name two
     of the metric, property and event registries share, and a unit test pins the shipped YAML.
  6. **The batch keeps the platform's lock order, and nothing it carries can fail it.** It takes every
     lane's event rule rows FOR SHARE and then every lane's calc marks before any lane writes, so it
     never holds a lane's series or health lock while waiting on another lane's rule row (a rule
     edit takes the row and then health). And since one refused value fails every row, the gateway
     replaces a NUL in every lane's text and jsonb before the insert, as the log-line lane already
     did.
- **Context:** The consumer wrote its sinks in separate transactions and acked once, so a failure after
  an earlier sink committed rewrote it on each of up to five redeliveries, and `log_line`, written
  first, multiplied with every one. Two fixes were on the table (#311): one transaction, or
  `ON CONFLICT DO NOTHING` on a natural key. The natural key cannot be the mechanism: `log_line` has
  none (a device emits identical lines in one instant), and the proposed `(owner, key, instance, ts)`
  omits provenance, so an observed row confirming an intended one in the same instant would be
  silently dropped. One transaction alone fixes a partial failure but not a duplicate delivery of a
  batch that already committed (an ack lost, a consumer dying between commit and ack, a node's retry
  past the stream's window), so the transaction carries a receipt: one row per batch rather than a key
  per row, which covers every sink at once and keeps the sample tables free of a uniqueness index the
  partitioning work (#420) would have to carry. The pages' earlier "idempotent on `(series, ts)`" is
  superseded by the per-batch receipt.
