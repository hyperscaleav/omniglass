---
title: "An update_mask says which fields a PATCH writes"
date: 2026-08-09
status: "Accepted"
summary: "A `PATCH` body may carry an optional `update_mask` with AIP-134 semantics exactly (absent is the implied mask of populated fields, present writes exactly what it names so a zero value clears, `[\"*\"]` is full replacement, an unknown field is a 422 naming it); it rides in the body, the three-state string sentinel stays, the other 108 `PATCH` routes are not retrofitted, and the role declarations convert from `PUT` to `PATCH` as its first consumer"
---

- **Date:** 2026-08-09 | **Status:** Accepted | **Pages:** [api](/architecture/api/)
- **Decision:** a `PATCH` body may carry an optional `update_mask`, with AIP-134's semantics exactly:
  an **absent** mask is the implied mask of the fields the body populated (a non-empty value), which
  is what every `PATCH` in the tree already did; a **present** mask writes exactly the fields it
  names, populated or not, so a named field carrying its zero value **clears**; **`["*"]`** is full
  replacement, the equivalent of a `PUT`, and cannot be combined with named fields; and a mask naming
  a field the resource does not patch is a **422 naming the field**, never a silent no-op. Top-level
  field names only, spelled as the wire body spells them. It rides in the **request body**, not the
  query string. It is built as a primitive (`internal/updatemask`, a pure `Resolve` over three lists)
  and consumed by one caller, the role declarations, which are converted from `PUT` to `PATCH` in the
  same change.
- **Context:** the API could set a field and could narrow one, but for anything that is not a string
  it could never unset one. `system_role.capacity` is an integer with no empty-string sentinel
  available, so once an operator set a cap, raw SQL was the only way back to unbounded
  ([#638](https://github.com/hyperscaleav/omniglass/issues/638)). That is not a `capacity` quirk, it
  is a property of every non-string optional field the API will ever have, and AIP-134 has no answer
  for it through the implied mask, whose whole definition is "fields that are populated". The
  mechanism it gives is the explicit mask.

  **Why the body, not the query.** Google puts the mask in the query because gRPC transcoding binds
  the request body to the resource itself, leaving nowhere else for it. There is no gRPC here, Huma
  models a body as a typed struct, and a body field generates cleanly into the OpenAPI document, the
  typed SPA client, and a CLI flag with no hand editing. Recorded because it is expensive to reverse
  once clients exist.

  **The three-state string sentinel stays.** An omitted field unchanged, an explicit `""` clears, a
  value sets (`emptyPtrToNil`, `internal/api/products.go`) is already the house convention for an
  optional string reference, taught by the CLI reference on `:move` and the system patch. The mask
  generalizes clearing to everything that is not a string; it does not retire the sentinel, which is a
  larger ripple across the docs and the CLI reference and should not ride along with the mechanism
  that would eventually replace it. Where the two meet, the sentinel wins the definition of
  "populated": a pointer to `""` counts as populated (the caller said something, and what they said
  was "clear it"), which is what keeps the role alternate's explicit detach path working.

  **No retrofit.** The other 108 `PATCH` registrations accept no mask and are not changed. They stay
  correct for free: an absent mask IS the implied mask, so their behavior is byte-identical, proven by
  the suite staying green with no expectation edited. Retrofitting is a per-route decision about which
  fields are patchable, not a mechanical sweep, and doing it under one slice would have made the
  primitive impossible to review.

  **The roles conversion is the proof, and it changes wire behavior.** The role declarations were a
  `PUT` carrying two semantics at once: `capacity` and `alternate` preserved on omit while
  `display_name`, `quorum`, `impact`, `position_labels`, `accepted_types` and `pinned_products`
  replaced wholesale, so a write carrying only `display_name` silently reset the role's impact to
  `degraded` and dropped its labels and its typed slot
  ([#639](https://github.com/hyperscaleav/omniglass/issues/639)). Under the implied mask the whole
  body reads one way. Two consequences are deliberate rather than incidental: an **empty list is not a
  populated field**, so `[]` now means "unchanged" where it used to clear, and clearing a list means
  naming it in the mask (the console's role editor therefore names every field it owns, and nothing it
  does not); and the declaration routes become `PATCH`, which AIP-134 requires and which matters
  because `PUT` "becomes a backwards-incompatible change to add fields to the resource", and fields are
  about to be added. The `PATCH` still CREATES the role when it is absent, which AIP-134 would gate
  behind `allow_missing`: there is no other create path for a declaration (the role is addressed by
  name within its owner, so declaring and revising are the same idempotent write), and a flag whose
  only legal value is `true` teaches nothing. The binding-style `PUT` routes (`members`, the
  `*_properties` and `*_metrics` association routes) are untouched: they set an association wholesale
  rather than updating a resource.
- **Tracked under** [#666](https://github.com/hyperscaleav/omniglass/issues/666), with
  [#638](https://github.com/hyperscaleav/omniglass/issues/638),
  [#639](https://github.com/hyperscaleav/omniglass/issues/639) and
  [#640](https://github.com/hyperscaleav/omniglass/issues/640).
