---
title: "A system carries a coarse type"
date: 2026-08-09
refs: []
form: bullet
---

- **A system carries a coarse type.** The **`system_type`** registry lands, the system-side counterpart
  of `component_type` and the second prerequisite of the generated-names epic
  ([ADR-0096](/architecture/decisions/#adr-0096-the-system_type-name-returns-as-the-coarse-space-taxonomy)):
  a nested, universally seeded taxonomy of what kind of space a system is, with `system.system_type_id`
  pointing at it (nullable for now; the floor waits until the shipped tree has proven out). The shipped
  tree is `av` over `room` over `board` / `class` / `meeting` / `training` / `conference` / `huddle`,
  and `av` over `sign` over `video-wall` / `interactive-sign`. `stem`, `abbrev`, and `icon` inherit down
  `parent_id` and are overridden at whichever node needs its own, so `board` states `boardroom` and
  `br` while taking its icon from `room`, which overrides `av`'s. Those strings are not filler: they
  are what the naming and label rules will render from, which is why they were chosen here rather than
  left for the rules to invent.

  Using `standard`'s own `parent_standard_id` inheritance for this was considered and rejected. A
  standard fork expresses **design forks**, two ways to build the same kind of room; the coarse
  classifier is a different question, wants a universal shipped seed, and one estate holds ten signage
  standards and six classroom standards under a single coarse type. So the system side ends up exactly
  parallel to the component side, and `standard` is untouched: no columns added, no inheritance
  semantics changed.

  The identifier is deliberately reused. `system_type` was the old column name for what became
  `system.standard_id`, and it sat on the docs-lint denylist; a **table** by that name is a different
  object from the retired column, so the entry is removed rather than exempted, on exactly the
  precedent `component_type` set, and the ADR records the reuse so a reader who greps and finds both
  has somewhere to land. The fossil left in `mapSystemWriteErr` (the constraint name
  `system_system_type_fkey`, which points at the **standard** key) now sits beside
  `system_system_type_id_fkey`, which is the new one, each labelled with which is which.

  The load-bearing test is the inheritance walk, built so it can fail: a three-level fixture with each
  fact set at a different depth (stem at the root, icon at the MIDDLE node, abbrev at the leaf), so
  resolving the leaf has one right answer per fact and each discriminates a different defect. Proven by
  mutation four ways: letting the last non-null win returns the root's icon, refusing to walk returns
  an empty stem, skipping the starting row returns the root's abbrev, and giving `board` its own icon
  in the seed trips the shipped-tree twin's own premise guard. The delete path pre-counts **both**
  reference sides, so a type that still parents another type and a type that still classifies a system
  both come back as the registry's clean in-use 409 rather than whichever raw foreign-key error fired
  first; `deleteTypeRow` became variadic to allow it.

  Surfaces: `GET/POST /system-types` and `PATCH/DELETE /system-types/{id}` under
  `system_type:read|create|update|delete`, the generated CLI verbs, a **System Types** console page
  under Catalog > Systems beside Standards, and a tree-indented type picker on the system create form
  and edit blade. `system.system_type_id` follows the house three-state patch convention, so an
  omitted field leaves the classification alone and an explicit empty string un-classifies.
