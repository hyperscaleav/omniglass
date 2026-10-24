---
title: "`component_make` registry slice 1, an `official` boolean, a deferred referential guard, and website scheme validation"
date: 2026-07-14
status: "Accepted"
summary: "`component_make` slice 1: an `official` boolean (not an `origin` enum) for consistency with the type registries; the in-use referential delete guard deferred to the `component_model` slice (nothing references a make yet); `website` scheme-validated to `http`/`https`, client and server, against stored XSS"
---

- **Date:** 2026-07-14 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/), [Vendors guide](/guides/admin/vendors/)
- **Decision:** Three calls on the first slice of the `component_make` manufacturer registry (id,
  display_name, icon, support_phone, website), lands ahead of the rest of the make/model catalog.
  **(1) `official boolean`, not an `origin` enum.** The design sketch (below) proposed
  `origin official | seed | custom` on make and model, matching the model layer's eventual needs.
  Slice 1 ships a plain `official` boolean instead, because `component_type` and the other
  registries already distinguish seed-owned from operator rows with a boolean, and a
  two-value distinction gains nothing from a three-value enum until a real `seed` (installed,
  mutable) tier exists to fill it; `origin` can still land on `component_model` if that tier turns
  out to be real. **(2) The in-use / referential delete guard is deferred.** `component_type`,
  `location_type`, and `system_type` all refuse a delete while a location, system, or component
  still references the row (409). `component_make` ships **no equivalent guard**: nothing
  references a `component_make` yet (`component_model`, the referencing entity, does not exist),
  so a custom make deletes unconditionally (an official row is still refused, 422, the seed-owned
  rule). The guard is added when `component_model` lands and gives the registry something to be
  in-use by, rather than building an unused check now. **(3) Website URL scheme validation, client
  and server.** The create/edit form renders `website` as a live anchor; an operator-entered value
  with no scheme check is a stored-XSS vector (`javascript:`/`data:` executing on click). A
  `validWebsiteScheme` guard on the API (`http`/`https` only, empty allowed, else 422) and a
  matching `safeUrl` guard on the console (render a live link only when safe, else plain text,
  never a dead or unsafe anchor) close it in both places: server-side so a non-browser caller
  (CLI/curl) cannot persist a dangerous scheme, client-side so a value written before the
  server-side check existed (or by any path that bypassed it) still renders safely.
- **Context:** `docs/superpowers/specs/2026-07-14-component-make-model-catalog-design.md` sketches
  the full make/model catalog (`component_make`, a `component_type` genus tree, `component_model`,
  and `component.model_id`) as four independent vertical slices; this is slice 1, make alone, with
  no dependency on the tree or the model layer. A review pass on the first cut of the console page
  (Task 4) found the missing website-scheme check as a stored-XSS gap before this shipped, closed
  in the same slice rather than carried as a follow-up.
- **Divergences logged:** the design sketch's `origin official | seed | custom` enum is not what
  shipped; `official boolean` did, per (1) above. The design's delete-refused-while-referenced rule
  is not enforced yet; per (2), it is deferred to the `component_model` slice that gives it
  something to check.
- **Lands:** [epic #254](https://github.com/hyperscaleav/omniglass/issues/254), issue
  [#255](https://github.com/hyperscaleav/omniglass/issues/255). Design:
  `docs/superpowers/specs/2026-07-14-component-make-model-catalog-design.md`. Plan:
  `docs/superpowers/plans/2026-07-14-component-make-registry.md`.
