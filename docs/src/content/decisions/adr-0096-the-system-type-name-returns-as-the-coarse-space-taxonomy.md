---
title: "The `system_type` name returns as the coarse space taxonomy"
date: 2026-08-09
status: "Accepted"
summary: "A nested, universally seeded **`system_type`** registry lands beside `standard` (not inside it): the coarse taxonomy of what kind of space a system is (`av / room / {board, class, ...}`, `av / sign / {...}`), with `stem`, `abbrev`, and `icon` inherited down `parent_id` and overridable at any node, and `system.system_type_id` nullable for now. The identifier is reused deliberately (it was ADR-0048's retired column name for `standard_id`), so its docs-lint denylist entry is removed on ADR-0085's precedent"
---

- **Date:** 2026-08-09 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [storage](/architecture/storage/), [API](/architecture/api/), [glossary](/architecture/glossary/)
- **Decision:** A new **`system_type`** table lands: a nested, universally seeded registry saying what
  **kind of space** a system is (`av / room / {board, class, meeting, training, conference, huddle}`,
  `av / sign / {video-wall, interactive-sign}`), exactly parallel to the way `component_type`
  classifies a product. It carries `name`, `display_name`, `stem`, `abbrev`, `icon`, `parent_id`, and
  `official`; `stem`, `abbrev`, and `icon` are nullable and **inherited from the nearest ancestor that
  sets one**, resolved by walking `parent_id` in Go. `system.system_type_id` points at it, **nullable
  for now**; a floor waits until the shipped tree has proven out. Both foreign keys are `ON DELETE
  RESTRICT`, and the delete path pre-counts **both** sides, so a type that still parents another type
  or still classifies a system is refused with the registry's own in-use error rather than a raw
  constraint failure.

  It does **not** carry `default_tags`, the one column `component_type` has that this does not: a
  product's instances start from its type's tag set, while a system's effective tags come from the
  platform, its location, and its own system tree, so the column would have no reader.

  `standard` is untouched. The two answer different questions and both stay: the **type** is what a
  system **is**, the **standard** is the blueprint it is **built to**.
- **Decision (the identifier reuse):** `system_type` was the **old column name** for what
  [ADR-0048](#adr-0048-the-standard-blueprint-and-the-template-fork-seed-model) promoted to
  `system.standard_id`, and it sat on the docs-lint denylist. A **table** by that name, with a
  `system.system_type_id` pointing at it, is a different object from the retired column, so the reuse
  is safe at the schema level; the collision is in prose, where a reader who greps finds both. The
  denylist entry is therefore **removed**, not exempted, on exactly the precedent
  [ADR-0085](#adr-0085-the-component_type-registry-returns-as-the-device-class-genus) set for
  `component_type`: an entry cannot express "banned, except in its own reintroduced meaning", and
  every sentence teaching the new registry would otherwise need an escape. The retired sense survives
  in ADR-0048's own prose under the standing retirement-marker exemption. One fossil is left in place
  deliberately and now labelled: `internal/storage/systems.go` maps the constraint name
  `system_system_type_fkey` (the pre-rename name of the **standard** foreign key) alongside
  `system_standard_id_fkey`, and the new key is `system_system_type_id_fkey`, a distinct string that
  maps to its own error.
- **Context:** `standard` is already nested (`parent_standard_id`), so using standard inheritance as
  the taxonomy was on the table and is the wrong axis. Standard inheritance expresses **design forks**,
  two ways to build the same kind of room; the coarse classifier is a different question, it wants a
  universal shipped seed, and one estate can hold ten signage standards and six classroom standards
  under a single coarse type. That makes the system side exactly parallel to the component side, where
  `component_type` is the coarse nested taxonomy with a universal seed and `product` is the specific
  artifact.

  The forcing function is naming. A generated system name needs a stem in the space vocabulary
  (`boardroom-2`) and a label render needs a compact form (`br`), and nothing in the estate model
  spoke either: a standard is a blueprint name (`meeting-room-v2`), a location type is where the room
  sits rather than what it is for, and neither is universally seeded. Every identity fact the console
  reached for turned out to live at the same missing level, the same tell that made ADR-0085's case.
  The shipped tree is deliberately not filler: its display names, stems, and abbrevs are the strings
  the whole naming arc will render from.
- **Tracked under** [#656](https://github.com/hyperscaleav/omniglass/issues/656), the second
  prerequisite of epic [#657](https://github.com/hyperscaleav/omniglass/issues/657).
