---
title: "The conduit's page binds the identities it reports (#1389)"
date: 2026-10-29
refs: [1389, 1282]
---

Slice 5 of the shared-API fan-out (#1282,
[ADR-1282](/architecture/decisions/#adr-1282-a-conduit-fans-each-identified-value-out-to-the-owner-its-binding-names-and-a-binding-costs-a-write-on-both-of-its-ends)).
A component that speaks for many owners gains an **Identities** card on its Overview, under
Collection, and a component that speaks only for itself shows none. The card reads the two routes
slice 1 and slice 4 built: **Fans out to**, each binding with its owner linked and the owners the
caller cannot read counted, never named; and **Heard, not bound**, the conduit's inbox with each
identity's count, last heard and the keys it carried. An unbound row binds in place through a picker
over the systems and components the caller may update, and the row moves up; a refusal (403, 404 or
422) reads in plain words against the row, and a 404 reads the owners again so a vanished one leaves
the picker. A bound row unbinds after a confirm. Both lists poll while the card shows, so an
identity heard for the first time appears without a reload.

In the lab, binding the one identity the simulated UCC platform reports unbound (`zr-boardroom-b`) from the card made the
next poll land its call state on the chosen system, observed, its source naming the conduit and its
endpoint ([the component guide](/guides/operator/fleet/#a-component)).
