---
title: "The create form locks the generated name and label until you override"
date: 2026-08-11
refs: []
form: bullet
---

- **The create form locks the generated name and label until you override**
  ([#699](https://github.com/hyperscaleav/omniglass/issues/699), the tenth slice of
  [#657](https://github.com/hyperscaleav/omniglass/issues/657),
  [ADR-0104](/architecture/decisions/#adr-0104-a-create-form-shows-the-name-it-can-know-and-never-mints-one-to-preview-it)
  amended). Slice 8 showed the generated name as a hint line above two empty editable fields, which
  put the platform-owned path in the position of the fallback and the operator's in the position of
  the default, exactly backwards, and said nothing at all about the label. Both fields now open
  **locked**, each filled with the value the platform will actually use, each with its own Override.

  The ruling that made the label showable is that **a render is not a mint**, which ADR-0104 turned on
  without naming. Both of its refusals are about ALLOCATING: a minted ordinal is provisional because
  another create can take it before the commit, and a rolled-back mint takes the bucket's advisory
  lock, so previewing per picker change would serialise the estate's creates. Neither reaches an
  operation that allocates nothing. `POST /<collection>:renderLabel` resolves the rule through the
  same tiers, builds the same closed data map, executes it with the same one engine, and writes the
  token `n` where the ordinal would go. The TypeScript refusal is untouched and is what forces this
  shape: the label is shown because the Go engine rendered it, not because the browser learned to.
  The no-allocation claim is proven rather than asserted, by reading back every SQL statement the
  render issued (#650's counting instrument) and by a create five renders later still taking ordinal 1.

  It is gated by the entity's own `:create`, the permission the create it precedes needs. The
  PLACEMENT is a separate question, because the rendered string can carry a location's label and a
  system type's label, so those refs resolve within the caller's `location:read` and `system:read`
  scopes and a placement out of scope is refused rather than rendered; the test that holds it drives a
  principal scoped to one wing, not an owner. A location draft injects no scope, because a location's
  data map reads no other estate row.

  Each field has **three** states, and the third is not a loading one. Derived from the refusals in
  the code rather than from a list, they are: a `component_type` chain with no stem, a system with no
  `system_type`, a `system_type` chain with no stem (which the console's copy had folded into the
  previous one), a `location_type` with no name rule, and, on the label side, no rule resolving at any
  tier. That last is the DEFAULT state of every location create form in a fresh estate, since no
  location label rule ships at any tier, so the locked field shows the NAME (the read ladder's third
  rung) and says why, rather than sitting locked and empty.

  The wire contract is structural rather than remembered. A `Pen` clears its value when the lock
  closes, so "locked" and "posts nothing" are one state and a page's create body stays the `value ||
  undefined` it already was, and the draft body mirrors the create body field for field, including
  the omitted-means-generate name.
