---
title: "The push lane reports its publish turn, and lab measure reads a refused push as refused (#1746)"
date: 2026-11-11
refs: [1746, 1696, 1623]
---

The push lane publishes one batch at a time, and a push that waits past 5 seconds for its turn is
answered `503` with nothing minted, so until now a push lane refusing its pushers read 0 loss. The
publisher now reports every turn to an observer, and the server writes four figures each report
window on its platform component: `push-lane-publish-count`, `push-lane-refusal-count`,
`push-lane-turn-wait-p99` and `push-lane-turn-wait-max` (the platform recorder gained a quantile
fold for the p99). Platform health shows them on the server's Ingest panel, refusals as a share of
publishes. `lab measure` sums the two counts over its window into a push-lane `publish` row ahead of
the ingest rows, counted in pushes, its loss `refused-turn`, and the perf gate reads it as
`delivery_loss/push/publish/pushes`. A unit test holds the turn, refuses a push, and proves the
ledger still reconciles with nothing missing, while the scale summary reads the refusal on the
publish row. The decisions are
[ADR-1746](/architecture/decisions/#adr-1746-a-push-refused-at-the-publish-turn-is-read-as-a-publish-row-counted-in-pushes).
