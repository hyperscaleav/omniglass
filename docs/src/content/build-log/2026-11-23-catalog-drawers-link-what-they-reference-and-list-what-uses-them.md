---
title: "Catalog drawers link what they reference and list what uses them (#2430)"
date: 2026-11-23
refs: [2430, 2429, 2511]
---

The catalog could not be followed: a product's vendor, driver and type were plain text, no drawer said
what used it, and a product that "declares no metrics" gave no hint that its driver polls several.

Every reference a catalog drawer renders is now a link through `drawerHref` (`EntityLink`, and
`CatalogRef` for a stored handle that reads as the catalog row's label). One shared `UsedBy` section
carries the reverse lists (a product's components, a driver's products, the rules that read a metric),
and a product's drawer adds **Collected by its driver**, what a component will actually report, apart
from the contract it declares. The metrics and properties **Written by** column names the writer class
when no calc rule writes the key. The dev seed carries one calc rule so the Calc rules page is populated
in a lab.

The fleet counts come from the list routes, not the browser: `GET /components` takes `product`, `driver`,
`component_type` and `limit` and returns `total`; `GET /systems` takes `standard` and `limit`;
`GET /products` takes `declares_metric` and `declares_property`. Scope is applied before the count.
The filters run after the scoped read, so the database does the same work as the unfiltered list; what
they save is the wire and the browser. A reverse index is the next step if the fleet makes that matter.
