---
title: "A draft does not preview a bucket its create refuses, and the precondition binds the name"
date: 2026-08-12
refs: []
form: bullet
---

- **A draft does not preview a bucket its create refuses, and the precondition binds the name.** Two
  more findings against the create form's identity render.

  The draft resolved an empty parent to "the parentless bucket" with no gate, and every create
  refuses that bucket without an all-scoped grant. The answer is not inert: it carries the lowest
  free ordinal, read from the bucket's sibling NAMES, so it reports which of that bucket's names are
  taken, and the stem asked about is the caller's to choose, since writing one into a forked type's
  name rule needs only `location_type:create`. Driven, the root bucket answered `secret-region-2` to
  a principal whose create there is a 403. One seam (`draftParentID`) now applies the create's own
  gate for all three tiers.

  The precondition bound the ordinal, and the ordinal is not the claim a locked field makes. A name
  is the stem, the suppression rule and the number together, so `PATCH /component-types/display
  {"stem":"monitor"}` under an open form left the number untouched and the row landed `monitor-1`
  where the field showed `display-1`, precondition met. `expected_name` replaces `expected_ordinal`
  on all three creates and carries the whole claim by construction, so no future input to a mint has
  to be remembered here. It is still a precondition and not the name field: the create leaves `name`
  empty, the row is still `name_generated`, and posting it beside a supplied name is the same 422.
  The 409's machine-readable detail moves to `body.expected_name` and carries the name the create
  would have produced, which is what the form shows next.
