---
title: "Omniglass receives webhooks and maps them to any lane (#1024)"
date: 2026-10-25
refs: [1024, 1146]
---

Slice 1 of the server ingress epic (#1146,
[ADR-1024](/architecture/decisions/#adr-1024-omniglass-receives-webhooks-on-the-server-and-a-webhook-token-is-a-sealed-secret)).
`webhook` is the registry's first push transport (`internal/transport`): an endpoint on it is
hosted by the server, has no reachability task or ladder, and reads last heard. Creating one mints
its token, a sealed `webhook-token` secret on its component (`internal/storage/webhooks.go`), and
`POST /endpoints/{endpoint}:rotateToken` mints it anew. A webhook driver declares its scheme
(`hmac` over a signed timestamp, `token`, `basic`, or `none`, which reads unsigned), the
signature's shape, a delivery id and a verification challenge; its listeners claim a post by its
body or a header and read a `json`, `xml`, `form` or `text` body through the one primitive
(`internal/driver/webhook.go`).

The listener primitive's core (`internal/ingress`) is pure: the schemes' constant-time checks,
failing closed on a missing or blank credential; the replay window and the per-endpoint delivery
memory; token buckets bounded under a flood of sources; listeners-first routing to a batch that
lands the raw post as a log line unless a listener says `log: false`; and the tally of refusals and
unknown senders. The server hosts it at `POST /webhooks/{token}` (ungated by necessity, beside
`POST /nodes:claim`), answering 202 once published, 401, an identical 404, 413, and 429 with
Retry-After, and publishes on the push lane, whose writer admits only collected keys. Refusals
count onto the endpoint's read; posts to an unknown token land in the unclaimed-source inbox
(`GET /unclaimed-sources`, `node:read`), let go once the sender's posts land on an endpoint. The
console's endpoint row shows a webhook endpoint's scheme, last heard and refusals, with its URL
revealed and copied under `secret:reveal`.

The simulated UCC service takes a webhook subscription on its admin API and posts each meeting's
start and end signed, with a delivery id, its event header, and faults that post a wrong signature,
a stale timestamp or one delivery twice; the shipped `ucc-webhook` driver maps them to
`call-started`, `call-ended` and `call-state`, and the dev fleet carries the service with its
webhook endpoint. Proven by the spec's validation, the ingress core's tests, the face's posts
checked by the server's own check, and end to end over the real wire
(`internal/api/webhooks_e2e_test.go`).
