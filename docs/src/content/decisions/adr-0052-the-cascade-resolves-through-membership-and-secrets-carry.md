---
title: "The cascade resolves through membership, and secrets carry no system band"
date: 2026-07-21
status: "Accepted"
summary: "The cascade resolves through membership, and secrets carry no system band"
---

- **Date:** 2026-07-21 | **Status:** Accepted | **Pages:** [cascade](/architecture/cascade/)
- **Context:** [ADR-0051](#adr-0051-membership-is-the-attachment-and-a-role-is-what-it-does) made
  membership explicit but deliberately left resolution alone: the tag, variable, and secret cascades
  still seeded their system band from `component.system_id`, the write-once pointer. That left the
  pointer alive for one reason only, and left the "config differs per system" case unanswerable.
- **Decision:** the system band is seeded from **`system_member`**. Tag resolution **takes the system
  to resolve against**, and resolves against it only if the component is a member: naming a system it
  has no binding to must not lend it configuration. With **no system given** it falls back to the
  **primary** membership, which is the entirety of what `is_primary` is for. `GET
  /components/{name}/effective-tags?system=` exposes the first case.
- **The seed stays single-valued, as a correctness requirement.** The rank orders by band then depth
  with no tiebreaker after that, so two seeds in one band resolve nondeterministically. Membership is
  many-valued; the chain it feeds is not. This is the same fact that made the pointer worth keeping
  under ADR-0051 and is now satisfied without it.
- **Secrets lose the system band entirely**, on ownership rather than determinism: an interface
  belongs to a component, a shared device has one password, and the room it serves is the wrong owner
  for a credential. It also removes the one case where an ambiguous inheritance would have been
  dangerous rather than merely wrong.
- **`component.system_id` is dropped.** With nothing reading it, the column, its API field, and its
  console consumers go. The component body now reports `system` (the primary, by **name**) and
  `system_count`, which also retires one of the three places the API emitted a raw uuid for a field it
  accepts by name ([#328](https://github.com/hyperscaleav/omniglass/issues/328)).
- **Written test-first because the failure mode is silent.** A mis-seeded `sys_chain` is still valid
  SQL that returns fewer rows: a system-owned tag would simply stop reaching its components, with no
  error and no 500, and the resolution blade would show the location winner as though the system band
  never had a candidate.
- **Supersedes** [cascade](/architecture/cascade/)'s "the primary-system pointer is the single system
  chain that feeds the cascade", which described the mechanism when a pointer was the only binding
  available.
- **Tracked under** epic [#324](https://github.com/hyperscaleav/omniglass/issues/324), slice
  [#327](https://github.com/hyperscaleav/omniglass/issues/327).
