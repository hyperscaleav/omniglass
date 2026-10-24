---
title: "A `PATCH` can say which fields it writes, so it can clear one"
date: 2026-08-09
refs: []
form: bullet
---

- **A `PATCH` can say which fields it writes, so it can clear one**
  ([#666](https://github.com/hyperscaleav/omniglass/issues/666), with
  [#638](https://github.com/hyperscaleav/omniglass/issues/638),
  [#639](https://github.com/hyperscaleav/omniglass/issues/639),
  [#640](https://github.com/hyperscaleav/omniglass/issues/640)). The API could set a field and narrow
  one, but for anything that was not a string it could never unset one: `system_role.capacity` is an
  integer, preserved on omit and with no empty-string sentinel available, so once an operator set a cap
  raw SQL was the only way back to unbounded. `internal/updatemask` is the primitive that closes it, a
  pure `Resolve` over three lists with AIP-134's rules exactly and unit tests written as the
  specification of them: an absent mask is the implied mask of the fields the body populated (what
  every `PATCH` in the tree already did, which is why none of the other 108 registrations change or
  need to), a present mask writes exactly the fields it names so one carrying its zero value clears,
  `["*"]` is full replacement and refuses to be combined with named fields, and a mask naming a field
  the resource does not patch is a 422 naming the field rather than a silent no-op. It rides in the
  request body, not the query string ([ADR-0091](/architecture/decisions/#adr-0091-an-update_mask-says-which-fields-a-patch-writes)),
  and generates into OpenAPI, the typed client and a CLI flag with no hand editing.

  The role declarations are its first consumer and the reason it is provably a primitive rather than a
  helper: they were a `PUT` carrying two semantics at once, `capacity` and `alternate` preserving on
  omit while everything else replaced wholesale, so a save carrying only a display name silently reset
  the role's `impact` to `degraded` (moving its system's verdict rollup with no visible field and no
  warning) and dropped its position labels and its typed slot. Both arcs, standard-owned and
  system-owned, become `PATCH` and consume the mask; `SetSystemRole` builds its `ON CONFLICT` set list
  from the resolved write set, so a field outside it is read and rewritten from the stored column
  inside the one upsert rather than merged in from a row read a moment earlier. Two wire behaviors
  changed deliberately: an empty list is not a populated field, so `[]` means "unchanged" where it used
  to clear (clearing means naming the field in the mask), and the console's role editor therefore
  declares every field it owns in `update_mask`, and pointedly not `alternate`, which it has no control
  for. A role's `alternate` also gained a read side: it was writable and returned by nothing, so an
  operator could join a role to an alternate and had no API way to confirm it landed; both role reads
  and the write's own echo now carry it as the same `choice/alternate` address the write body takes,
  asserted as a round trip.
