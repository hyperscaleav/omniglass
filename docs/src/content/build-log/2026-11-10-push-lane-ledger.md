---
title: "The push lane's batches carry a ledger and read as their own ingest row (#1696)"
date: 2026-11-10
refs: [1696, 1641, 1623, 1691]
---

What is pushed to omniglass is now accounted like what its nodes collect. The bus server stamps every
batch it publishes on the push subject, from the push route or a webhook, with the delivery ledger
(one boot per server process, a dense seq, and per kind the count and running offset), publishing one
batch at a time so `resolved` is always the seq below. A publish the stream refuses answers the pusher
`503` and takes its items back off the offsets, since the pusher still holds them. A new
`internal/delivery` `Publisher` owns the stamp, the turn and the withdrawal. The push lane's receipts
carry no node, so `omniglass lab measure` reconciles each lane apart: the delivery table gains a
`lane` column (and the JSON a `lane` key), a Termed pushed batch reads under its own cause on the
push lane (its Term receipt under the server's boot), a hole there reads `missing` with its own footer,
and the end-to-end line composes the node lane alone. The decisions are
[ADR-1696](/architecture/decisions/#adr-1696-the-push-lanes-batches-carry-a-ledger-minted-by-the-server-and-read-as-their-own-ingest-row).
