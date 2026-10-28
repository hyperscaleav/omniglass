---
title: "The properties read carries reconciliation, and the reconciliation route retires"
date: 2026-10-28
status: Accepted
summary: "An owner's properties read (`GET /{components,systems,locations}/{name}/properties`) lists every property the owner has, declared, calculated, observed or told, and every row but a calculated one carries its want/told/is reconciliation. `GET /components/{name}/reconciliation` and `omniglass component reconciliation list` are removed outright, with no deprecated alias."
refs: [1297]
---

- **Date:** 2026-10-28 | **Status:** Accepted | **Pages:** [metrics and properties](/architecture/properties/#reconciliation-want-told-is), [API](/architecture/api/), [config and credentials](/architecture/variables/), [views](/architecture/views/)
- **Decision:** One storage read answers "what are this owner's properties and where does each
  stand": the reconciliation resolver folds into the properties pivot, and the component and system
  properties routes, the CLI and the console consume it. A row is the union of the declared cascade,
  calc rule outputs, keys with a live observed instance, and keys a command targeted; each carries a
  `reconciliation` block (`want`, `told`, `is`, `is_instance`, `drift`), absent on a calculated row,
  `want` null and `drift` false where nothing is declared. A place's rows carry the block with no `is`
  (it owns no observed sample). The component reconciliation route and its generated command are
  removed in the same change rather than kept one release as a deprecated alias.
- **Context:** The reconciliation read walked the declared properties alone, so a key the owner only
  observed never appeared anywhere a client could read it with its standing, and the console fetched
  the same owner twice (properties, then reconciliation) to draw two panels that could disagree. The
  architect resolved the open decision on #1297 as removal: the platform is pre-release, and every
  internal consumer (the console's panel, the CLI, the demo tape) moved in the same PR, so an alias
  would serve no caller. A client of the removed route reads the same pivot under each row's
  `reconciliation` on the properties read.
