---
title: "A push refused at the publish turn is read as a publish row counted in pushes"
date: 2026-11-11
status: "Accepted"
summary: "Accepted: the push lane's publisher reports every turn it grants or refuses, and the server writes the window's publishes, refusals and turn wait (p99 and max) on its platform component. The delivery read gives the push lane a publish row ahead of its ingest rows, counted in pushes (in every push that waited, out turn-taken, loss refused-turn), read from those metrics because a refused push mints nothing a receipt could carry. It is never composed with a kind's items."
refs: [1746, 1696, 1641, 1628, 1623]
---

- **Date:** 2026-11-11 | **Status:** Accepted | **Pages:** [messaging](/architecture/messaging/#accounting-every-item-the-loss-ratio), [lab](/guides/lab/#reading-the-push-lanes-turn), [platform health](/guides/operator/platform-health/#ingest)
- **Decision:** (#1746, under #1623, extending [ADR-1696](#adr-1696-the-push-lanes-batches-carry-a-ledger-minted-by-the-server-and-read-as-their-own-ingest-row).)
  1. **The publisher reports each turn.** `delivery.Publisher` hands an observer every turn as it
     ends: how long the caller waited and whether the wait ran out. The bus server's pipeline
     metrics fold them into the window: `push-lane-publish-count`, `push-lane-refusal-count`, and
     the wait's p99 and max (`push-lane-turn-wait-p99`, `push-lane-turn-wait-max`, a refusal's whole
     wait included), written with the rest of the server's figures once a report window.
  2. **A refused push is a publish-hop loss, not missing.** A push whose wait ran out was answered
     `503` and minted nothing, so it holds no seq and leaves no hole: the ingest rows cannot see it.
     The read gives the push lane a `publish` hop with the outcomes `turn-taken` (out) and
     `refused-turn` (lost), read by summing the two counts over the measured window.
  3. **Counted in pushes.** A push is one call whatever it carries, and a refused one never said what
     it carried, so the row's in and out are pushes. It names its subject `pushes` in the table and
     the perf figure (`delivery_loss/push/publish/pushes`), has no dedup, and is never composed into a
     kind's end to end.
  4. **No pushers, no row.** A window with no publishes has no publish row rather than a 0 that
     no push earned.
- **Context:** The push lane serializes its publishes (ADR-1696), and a push that waits past the
  turn's bound is answered `503`. Before this, nothing counted those refusals: a push lane refusing a
  share of its pushers read 0 loss in `lab measure`, and nothing on Platform health said it was near
  the bound.
- **Thin cuts:**
  - The counts are stamped at each report window's close, the receipts at each batch, so the
    publish row's window can lead or trail the ingest rows' by up to one report window.
  - A push that took the turn and then failed to publish (the stream never acked) reads `turn-taken`
    here; its items are taken back off the ledger as ADR-1696 decided, so it shows on neither row.
    The pusher was told `503` and still holds them.
  - The bound is the publisher's (5 seconds), not a setting.
- **Not chosen:**
  - **Counting the refusal in items.** The refused call's payload was never minted; counting it would
    mean decoding a payload the server just declined to take.
  - **A receipt for a refusal.** Minting a ledger entry for a call that published nothing would put
    a batch on the ledger that the stream never saw, and every reader would have to skip it.
  - **Folding it into the ingest row.** The ingest row counts items the server minted; mixing calls
    into it would change its unit.
