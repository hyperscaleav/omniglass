---
title: "A webhook notification target, sent through the egress primitive (#1320)"
date: 2026-10-28
refs: [618, 1320]
---

The second slice of the notification lane (#618). Omniglass had no server-side outbound HTTP; the
only egress was the simulator's. `internal/egress` is now the one client every outbound send uses:
internal, link-local and carrier-grade NAT destinations refused unless the new
`notification.egress_allow` setting names them, cloud metadata refused always, judged on the
address actually dialed; https, or http only to a named destination; TLS verified; redirects not
followed; every phase bounded; no environment proxy. Its tests dial real sockets, since a loopback
receiver is exactly the destination the contract refuses by default.

A `notification_target` names a channel kind (`webhook`), a URL and an optional platform secret.
It is admin-only (a new sensitive resource, off the viewer floor) and every route takes an
all-scope grant. The URL is masked on read and in the audit log, and revealed whole by `:reveal`
under its own audit row; a secret a target binds cannot be deleted from under it.

`internal/notify` builds the request in the page's order: the envelope rendered once, signed over
those exact bytes in the scheme the inbound `hmac` webhook verifies, with an `Idempotency-Key`. A
test feeds a delivery through `ingress.Authenticate`, so Omniglass verifies its own signature. The
envelope's JSON Schema is generated from its struct into the docs. `:test` sends one synthetic
message and answers what the receiver said.
