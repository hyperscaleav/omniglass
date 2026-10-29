---
title: "The ingest fans each identified value out to its bound owner (#1388)"
date: 2026-10-29
refs: [1388, 1282]
---

Slice 4 of the shared-API fan-out (#1282,
[ADR-1282](/architecture/decisions/#adr-1282-a-conduit-fans-each-identified-value-out-to-the-owner-its-binding-names-and-a-binding-costs-a-write-on-both-of-its-ends)).
The ingest consumer gains one fan-out step that the node lane and the webhook lane both pass. It
splits a batch's identified samples from the conduit's own, resolves every identity against the
conduit's bindings in one read, and lands each bound value on its owner in the batch's one
transaction, under its receipt: observed, at the empty instance, its source naming the conduit and
its endpoint. The conduit is the component the batch is already confined to (the node's own task's
component, or the webhook token's endpoint's component), so a payload names only an identity, never
an owner, and a batch confinement orphans is dropped before any identity is read.

The Storage Gateway gains a fan-out writer class: a collected key only, on an owner one of the
conduit's bindings names, re-read in the write's own transaction, so a binding deleted mid-flight
refuses the row. A batch about two hundred owners resolves them in one read and one fence; a query
count pins that it issues the statements a batch of three does. An identity no binding places lands
on no owner and is upserted into the conduit's new unbound inbox (a count, first and last heard, the
keys carried; 1024 per conduit, aged out after seven days), read at
`GET /components/{name}/unbound-identities`; binding it lets its row go. A received identity is held
to the engine's bounds again on the server. A sample with no identity lands exactly as before.

One acceptance behavior waits on another issue: event rules do not yet judge a system's samples
(#1002), so a rule over a fanned key raises its alarm on a component owner today and on a system
owner once that lands. The discovery rule that would propose bindings from the inbox is #1395.
