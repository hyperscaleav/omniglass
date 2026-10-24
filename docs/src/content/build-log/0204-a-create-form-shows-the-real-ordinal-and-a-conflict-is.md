---
title: "A create form shows the real ordinal, and a conflict is refused rather than renumbered"
date: 2026-08-12
refs: []
form: bullet
---

- **A create form shows the real ordinal, and a conflict is refused rather than renumbered.** A create
  form showed the token `n` where a generated name's ordinal would go (`display-n`) and the row then
  landed `display-1`, so the operator was never shown the name they actually got, which is the promise
  the whole locked-field design rests on. ADR-0104 had refused a preview because previewing meant
  MINTING, and a mint takes the bucket advisory lock real creates need: previewing per picker change
  would have serialised the estate's creates behind a UI affordance.

  Reading the lowest free ordinal is not minting it. It is the computation the allocator already runs
  over the sibling names in the placement bucket, with no lock, no write transaction and no
  allocation, so `:renderLabel` now answers with the whole drafted **name** beside the label and the
  form shows `display-3`. That the read allocates nothing is held by the counting instrument reading
  back every statement the render issued, and by a create five renders later still taking ordinal 1.

  The answer is provisional by nature, and that is answered rather than hidden: the form posts the
  number back as the create's `expected_ordinal`, the create allocates under its own lock exactly as
  before, and one that would land a different name is refused with a 409 naming the number that moved.
  The form re-reads and shows the new name. It posts a number and never a name, because a locked field
  posting a name would claim the pen and invert the feature; the API refuses the pair (422) where an
  operator supplied the name, since nothing is allocated there for the expectation to be about.
  (Corrected in the review below: the field is `expected_name` and carries the drafted name, which is
  the claim the locked field is actually making. It is still not the name field.)

  A create form is the one caller that cannot treat a refusal as "show it and stop", so the two
  refusals it has to act on carry a machine-readable location rather than only a sentence: the
  conflict on `body.expected_ordinal` (`body.expected_name` after that review) and "the platform will
  not name this row" on `body.name`. That is the RFC 9457 `errors` array the error model already
  publishes.

  Returning the name also ended a duplication: the console walked the type chain in TypeScript to
  resolve a stem, and the gateway walked it in Go, held together only by one browser end-to-end test
  (#695). The browser no longer knows what a mint looks like, and the three placement pickers feed the
  draft body because the ordinal is read from a BUCKET, which a draft that ignored the parent would
  have got wrong.
