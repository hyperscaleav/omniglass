---
title: "Membership is the attachment, and a role is what it does"
date: 2026-07-21
status: "Accepted"
summary: "Membership is the attachment, and a role is what it does"
---

- **Date:** 2026-07-21 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/)
- **Context:** a component's relationship to a system was **two unrelated facts that could silently
  disagree**. `component.system_id` was a single pointer, set once at create with no path to change it,
  which no authorization and no health path ever read; `role_assignment` was many-to-many and carried what
  the component actually does. Nothing reconciled them, and the console rendered the first under the heading
  "Components" while the panel directly below listed the second, so a fully staffed system displayed
  **`0 components`**. The contradiction was visible to operators before it was understood by us.
- **Decision:** membership is a **first-class binding**, `system_member (system_id, component_id,
  is_primary)`, and a role attaches to it. **Staffing a role creates the membership**, because a component
  filling a job in a system that the system does not count as a member is a contradiction. The reverse is
  **not** symmetric: giving up a role leaves the membership, because the device is still in the room, and a
  member carrying no role (a power conditioner, a spare) is ordinary.
- **Why membership cannot simply replace the pointer:** the cascade seeds its system band from **one** row
  and ranks with `row_number() over (partition by ... order by band desc, depth asc)`, which has **no
  tiebreaker after depth**. A many-valued seed would make an effective tag, variable, or secret resolve
  nondeterministically for precisely the shared-device case. Membership is therefore many-valued while
  **`is_primary`** keeps a single answer for callers with **no system in hand**. It is a **default, not a
  resolution rule**: anything naming a system resolves against that system, and a component's first
  membership takes the default with nobody asking, so the single-system case never meets the concept.
- **Cascade from both ends, and deliberately no restrict on the component.** A binding is meaningless once
  either side is gone. `role_assignment` keeps its `on delete restrict` because deleting a component that
  fills a job would silently break a system's health; duplicating that restrict on membership would add a
  step to every component removal while protecting nothing new.
- **Backfill reads both of the old places.** The role table alone drops every component that belonged to a
  system without filling a declared role; the pointer alone drops the shared device's other systems. The old
  pointer seeds `is_primary`, since answering which system chain feeds a component's config is exactly what
  it used to do. A component left with several memberships and no pointer gets **no** default, because there
  is no honest way to guess which one was meant.
- **Thin cut:** resolution behaviour does not move in this slice. `component.system_id` stays and keeps
  feeding the four cascade resolvers unchanged, so this ships and is verified on its own.
- **Supersedes:** [core-entities](/architecture/core-entities/)'s "a truly shared device **skips the system
  layer**", which was the best available answer while the only binding was a single pointer. A shared device
  is now a member of every system it serves. It also narrows that page's `system_member` design: the shipped
  row is the binding alone, without the role column or the pin to a frozen `system_template_version`, so a
  member can exist without a role.
- **Tracked under** epic [#324](https://github.com/hyperscaleav/omniglass/issues/324), slice
  [#325](https://github.com/hyperscaleav/omniglass/issues/325).
