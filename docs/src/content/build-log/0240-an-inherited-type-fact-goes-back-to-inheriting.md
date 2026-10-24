---
title: "An inherited type fact goes back to inheriting"
date: 2026-08-14
refs: []
form: bullet
---

- **An inherited type fact goes back to inheriting.** `stem`, `abbrev` and `icon` on
  [`component_type` and `system_type`](/architecture/core-entities/) are nullable and inherited, NULL
  meaning "walk to the nearest ancestor that sets one", and until now the console could destroy that
  state but never restore it. Two earlier slices stopped both edit blades writing `""` over a NULL,
  which was ending the walk for a node and every descendant under it; what neither could then spell
  is the opposite move, a node with a fact of its own edited back to inheriting its parent's.
  Emptying the box sent nothing, the coalescing patch kept the old value, and the console silently
  retained a value the operator had just deleted.

  The instrument was already in both handlers, one column over. `label_rule` has honoured the house
  **three-state string sentinel** since it landed (omitted is unchanged, `""` clears to NULL, a value
  sets), so all four columns are now the same line in both `UPDATE`s, and the **fork leg decodes it
  too**, since a shadow image is read back as the row. The mask was the wrong tool and
  [ADR-0114](/architecture/decisions/#adr-0114-an-inherited-registry-fact-clears-with-the-empty-string-and-the-pattern-is-what-admits-it)
  records why: it clears a nullable **object**, which has no empty value to overload, where a string
  has one already.

  **The hard part was validation, and it was on the wire rather than in the handler.** `stem` carried
  `minLength: 1` and the name pattern on the PATCH body, so an empty box was a 422 in the validator
  before any handler ran and no client could reach the capability at all. The `minLength` goes and
  the character rule is wrapped in an optional group, `^([a-z0-9][a-z0-9-]*)?$`, the spelling a name
  rule's own stem already ships: exactly one new string is admitted, and `Bad Stem`,
  `-leading-hyphen`, `UPPER` and a trailing space are still refused on both registries, with an e2e
  that says so. Create keeps its `minLength`, because a row that does not exist yet has nothing to
  clear. A **root** still cannot clear its stem: there is no ancestor behind it, so create's refusal
  now guards the second path that reaches the same broken row.

  Proven by testcontainer integration on both registries (the clear, the walk resuming at the
  ancestor's value, one fact clearing without disturbing the two beside it, the root refusal) and at
  the wire, where the fork leg is tested through a **consequence** rather than a column: `abbrev` is
  `omitempty`, so a shadow holding `""` and a shadow holding nothing read back identically, and only
  the name generator can tell them apart. Forking a stem onto shipped `ceiling-mic`, clearing it, and
  watching the drafted component name go back to the parent's `mic-1` is what the assertion is.
