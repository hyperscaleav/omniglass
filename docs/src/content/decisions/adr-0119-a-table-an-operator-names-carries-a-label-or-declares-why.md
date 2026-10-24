---
title: "A table an operator names carries a label, or declares why it does not"
date: 2026-08-14
status: "Accepted"
summary: "The identity declaration (`internal/storage/identity_shape.go`) grows the third column of the triad: a table an operator NAMES carries a `label`, or declares in `TableIdentity.NoLabel` why it does not, and a guard checks the claim against the generated schema both ways (a declared label the schema lacks fails; a reason on a table that has the column fails as stale). A table nobody names carries none at all, there being no name for an unset one to fall back to. Written RED it named exactly `tag`, `variable`, `secret` and `interface`, the four key-bearing tables that had gone without one, which is why the declaration is the fix rather than the four columns: the gap existed because the shape said what the identifier was and nothing about the friendly string beside it. All four gain the column (text, nullable, no default, unset is SQL NULL per [ADR-0118](#adr-0118-the-friendly-string-an-operator-reads-is-a-label-and-unset-is-sql-null)), with no backfill and no uniqueness, pattern or reserved words. **`interface` is included** ([D2](https://github.com/hyperscaleav/omniglass/issues/613)) and is the strongest of the four: its name is SERVER-derived from its type, so an interface's only string says which protocol it speaks and nothing about what it is FOR, and the label is settable **at create** rather than only on a following patch, an interface labelled by a following call being unlabelled at the moment it is made. The premise D2 was argued from is corrected here and pinned by a test: a component holds at most ONE interface per protocol today (the unique index is `(component, name)` and the name IS the protocol), so the three-`ssh`-interfaces case is not reachable, and the narrower fact carries the decision on its own, every SSH interface in the estate reading `ssh`. Its declared name exemption in `KeyProvedElsewhere` STAYS: the name really is derived, which is the argument for the label rather than something the label replaces. Four exemptions are declared with reasons: `interface_type` (retires with the `interface.type` FK, [ADR-0073](#adr-0073-a-driver-consumes-transports-and-the-interface-type-table-retires)), `file` (its name is already the label; [#755](https://github.com/hyperscaleav/omniglass/issues/755) may revisit), `service` (`principalIdent` resolves a service principal to `service.name`, which a label may not be) and `blob` (no operator surface of its own). Lists order by the rendered label, `order by label nulls last, name` ([D4](https://github.com/hyperscaleav/omniglass/issues/613)), matching the console's one comparator; the two CASCADE projections keep ordering by name, where the name is the cascade key grouping a winner with the candidates it shadows rather than a display order. `LabelledTables()` makes the declaration the only copy of the list the schema guard and the unset sweep read"
---

- **Date:** 2026-08-14 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/), [tags](/architecture/tags/), [config, secrets, and variables](/architecture/variables/), [data collection](/architecture/collection/), [storage](/architecture/storage/)
- **Decision:** `TableIdentity` gains `NoLabel`, the declared reason a table an operator names carries
  no label. Empty means it carries one. A guard checks that against the generated schema facts in
  both directions, and a table nobody names (`ShapeIDOnly`, `ShapeFixedKey`) must carry no label at
  all. `tag`, `variable`, `secret` and `interface` gain the column through the full slice.

**The declaration is the fix; the four columns are the consequence.** Four key-bearing tables had no
label, nothing failed, and the omission survived long enough to become an epic. It survived because
the declaration was complete about one half of the identity triad and silent about the other: every
table has a declared SHAPE, and the guard fails on a table missing from the map, so a new table
cannot arrive unclassified. Nothing asked whether the friendly string was there. Writing the new
guard against the schema as it stood named exactly the four, which is the evidence that the
declaration is load-bearing rather than decorative.

**The four were not interchangeable, and the differences are where the work was.**

- `tag` is the only NAME-addressed one (`/tags/{name}`), so its proof is that the label and the
  address do not disturb each other. Its name is fixed at creation and there is no `:rename` custom
  method, so the rename half is proved the way this table permits: the update that exists lands on
  the name it started with, and a governance edit that says nothing about the label leaves it.
- `variable` is uuid-addressed, and its collision is not with the address but with the other thing
  its PATCH carries. The route's `value` becomes optional so a label-only edit is expressible; the
  cost is that a `json`-typed variable can no longer be set to JSON null through it, an explicit
  null and an omitted field being the same thing on the wire.
- `secret` carries the most projections, so the work was the sweep: the directory (both scope arms),
  the create and update returns, the audit image, and the per-component cascade. The reveal and the
  copy carry no identity at all, only the decrypted field map, and that is pinned rather than
  assumed. A relabel is not a rename, which matters here more than anywhere: the sealed fields are
  bound to `(owner, name, field)` as AAD, so the test reveals the secret again afterwards.
- `interface` is the strongest case rather than the weakest. Its name is derived from its type, so
  the label is the ONE identity string an operator types, and it is settable at create because an
  interface labelled by a following call is unlabelled at the moment it is made. The epic argued
  this from "three `ssh` interfaces on one component", which the schema does not permit: the unique
  index is `(component, name)` and the name is the protocol, so the second is a 409. That conflict
  is now pinned by a test, and the narrower true statement carries the decision anyway, since every
  SSH interface in the estate reads `ssh` and nothing says which device role it plays.

**Ordering follows the label, and it is a decision that had already gone wrong once.** The lists say
`order by label nulls last, name`. `nulls last` is redundant on an ASC sort and written anyway,
because [#756](https://github.com/hyperscaleav/omniglass/issues/756) found seven registries already
shipping their unlabelled rows at the TOP of the picker, from an `order by display_name, name` over a
column storing the empty string. A new list is guilty until a test says otherwise, and the console's
one comparator (`byLabel`) is the same rule on the other side of the wire. The two CASCADE
projections deliberately keep ordering by NAME: there the name is the cascade key that groups a
winner with the candidates it shadows, not a display order.

**The exemptions are declared, not remembered.** `interface_type` retires with the `interface.type`
FK, so a column added there would be dropped with the table. `file.name` is already the label, and a
second string would be a second label rather than the identifier a label sits beside. `service` is a
username analogue whose name IS what every surface shows, since `principalIdent` cannot resolve to
something optional and non-unique. `blob` has no operator surface at all. `cmd/identitygen` renders
these into the docs, so the published list is generated rather than hand-kept, and
`LabelledTables()` makes the declaration the only copy of the list that the schema guard and the
unset-label sweep read.

**What is deliberately not here.** No backfill: existing rows come out unset and every surface
renders their name **verbatim**, never re-cased or prettified, which is the contract this epic was
corrected on twice. No uniqueness, no pattern, no reserved words, and a 200-character ceiling, which
is what makes it a label rather than a second identifier. Nothing derives a name from a label on the
interface create form, since that name is the platform's to mint.
