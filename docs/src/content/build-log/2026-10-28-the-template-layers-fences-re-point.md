---
title: "The template layer's fences re-point, and ADR-0827 records what the layer owns (#1322)"
date: 2026-10-28
refs: [1322, 827]
---

The first slice of #827 is docs only. Every fence that cited #827 for collection authoring described
the `ComponentTemplate` model (functions as a trigger plus a step DAG, Expr extractors, `inputs`
bound at `:apply`), which the driver spec replaced; the four collection fences and the glossary's
edge-function terms now read as historical records pointing at the driver spec, with the still-wanted
pieces (an Expr transform on an extractor, a step guard, Omniglass functions, function-run bindings)
re-pointed to #1331. The registry scope ladder drops its template rung and moves to #318, on the
glossary and the properties page, whose section is now "Key scope: org and official". The nodes
page's config-propagation fence became a built note: the node re-pulls its whole worklist every tick,
edits and spec changes re-derive tasks server-side, `config_generation` rides the reply, and `:apply`
retired. Drift named by the definition is fixed: the spine's Collect section names the driver as the
reusable shape, the cascade's template bands are the product and standard classifier bands (#1255,
#184), a signal is settable through the driver's command binding, the identity page says shipped
location types seed `official: true`, and the utilization KPI line names drivers. The templates page
now carries the approved design of record (four kinds, `GET /templates`, `:fork` and `:instantiate`,
provenance in the audit detail, no versioning, shipped templates only) inside its #827 fence, with the
trust surface fenced on #318
([ADR-0827](/architecture/decisions/#adr-0827-the-template-layer-owns-the-clone-model-and-the-componenttemplate-authoring-model-is-superseded-by-the-driver-spec)).
