---
title: "A tag, a variable, a secret and an interface carry a label"
date: 2026-08-14
refs: []
form: bullet
---

- **A tag, a variable, a secret and an interface carry a label**
  ([#613](https://github.com/hyperscaleav/omniglass/issues/613), ADR-0119). The four key-bearing
  tables that had no label get one, and the identity declaration grows the expectation that produced
  them, so the gap cannot reopen. It closes the epic.

  **The declaration is the slice; the four columns are what it found.**
  `internal/storage/identity_shape.go` declared every table's SHAPE and was complete by construction
  about it: a table missing from the map fails the guard. It said nothing about the friendly string
  beside the identifier, so `tag`, `variable`, `secret` and `interface` went without a label, no test
  failed, and an operator who could no longer write `cost_center` (the tightening ADR-0076 made,
  citing PagerDuty freezing a name and routing renaming pressure to the label beside it) had nowhere
  to write "Cost Center" instead. `TableIdentity` now carries `NoLabel`, the declared reason a table
  an operator names carries none, and the guard checks it against the generated schema both ways.
  Written first, against the schema as it stood, it named exactly those four.

  **Four tables, four different problems.** `tag` is the only name-ADDRESSED one, so it proves the
  label and the address leave each other alone; its name is fixed at creation and there is no
  `:rename`, so the rename half is proved the way that table permits. `variable` is the simplest
  until you look at its PATCH, where the label had to become an instruction beside the value rather
  than a second meaning of it: the route's `value` is optional now, so a relabel is not a value
  write. `secret` is a sweep rather than a column, since it is read through the directory, the create
  and update returns, the audit image and the per-component cascade; the reveal and the copy carry no
  identity at all, which is pinned rather than assumed. `interface` is the strongest of the four and
  the reason D2 went the way it did: its name is SERVER-derived from its type, so its only string
  says which protocol it speaks and nothing about what it is for, and the label is settable at create
  rather than on a following edit, since an interface labelled by a second call is unlabelled at the
  moment it is made. Its declared name exemption stays exactly where it was: the name really is
  derived, which is the argument FOR the label rather than something the label replaces.

  **One premise of that argument turned out to be false, and is corrected rather than repeated.** The
  epic argued from "a component with three `ssh` interfaces", which the schema does not permit: the
  unique index is `(component, name)` and the name IS the protocol, so the second is a 409. The
  conflict is now pinned by a test, and the narrower statement carries the decision on its own, every
  SSH interface in the estate reading `ssh` with nothing to say which device role it plays.

  **The ordering was treated as guilty.** Every new list orders `by label nulls last, name`, with a
  test, because #756 had just found seven registries shipping their unlabelled rows at the top of the
  picker from precisely this shape. The console's `byLabel` is the same rule on the other side of the
  wire. The two cascade projections keep ordering by NAME on purpose: there the name is the cascade
  key that groups a winner with the candidates it shadows, not a display order.

  **Normalization was not rebuilt.** `labelOrNull` and `labelPatch` (ADR-0118) already decide that
  unset is SQL NULL and nothing else, so the four join the sweep that drives every write path with a
  whitespace label and reads the raw column, rather than acquiring a forty-first hand-rolled binding.
  That sweep is complete by construction, and its list of labelled tables is now READ from the
  declaration instead of hand-kept beside it.

  No backfill: existing rows come out unset and render their name verbatim, never re-cased. No
  uniqueness, no pattern, no reserved words. `file` keeps its exemption
  ([#755](https://github.com/hyperscaleav/omniglass/issues/755)), and `interface_type`, `service`
  and `blob` gain declared ones, rendered into the docs by `cmd/identitygen` rather than written out
  by hand.
