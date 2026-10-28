---
title: "One verb creates from a template (#827)"
date: 2026-10-28
refs: [827, 1348]
---

The four methods that create from a template take one name on all four collections:
`POST /standards:createFromTemplate`, `/components:createFromTemplate`, `/systems:createFromTemplate`
and `/locations:createFromTemplate` (`omniglass <resource> createFromTemplate`). A custom method names
what differs from Create, and the difference is the template input, so one verb now means one thing
across the kinds. Every row a template creates is audited as a create carrying the template's name as
`from_template`, one key in place of two, and an answer's `standards_created` lists the standards a
write created from their templates. The day-old `:fork` and `:instantiate` are retired terms, refused
by the vocabulary lint
([ADR-0827](/architecture/decisions/#adr-0827-the-template-layer-owns-the-clone-model-and-the-componenttemplate-authoring-model-is-superseded-by-the-driver-spec)).
