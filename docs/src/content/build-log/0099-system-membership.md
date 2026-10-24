---
title: "System membership"
date: 2026-07-21
refs: []
form: bullet
---

- **System membership** (`system_member`): a component's binding to a system is now a row rather than a
  write-once pointer nobody read, and a role attaches to it. Membership is **many-valued**, so a rack DSP
  serving three rooms is a member of all three, which the old single pointer could not say at all, and
  **`is_primary`** keeps one answer for a question asked with no system in hand (a default for context-free
  callers, not a resolution rule; a partial unique index makes a second primary impossible). **Staffing a
  role creates the membership**, so the two can never disagree, while giving up a role leaves it, because a
  member carrying no role (a power conditioner, a spare) is ordinary. A one-time backfill reads **both** of
  the places membership used to be implied, the role table and the old pointer, since each holds components
  the other drops. The API adds `GET /systems/{name}/members`, `GET /components/{name}/memberships`,
  `PUT`/`DELETE /systems/{name}/members/{component}` and
  `POST /systems/{name}/members/{component}:setPrimary`; the CLI adds `omniglass system
  members|add-member|remove-member|set-primary-member` and `omniglass component systems`. Proven by the
  storage integration suite (the shared device in two systems, the first membership taking the default
  without being asked, a later one not stealing it, the default moving rather than duplicating, assignment
  creating the binding, removal refused while a role is still filled, the cascade from both ends, and the
  out-of-scope non-disclosing 404) and by a backfill test that executes the **shipped migration file** rather
  than a copy, covering the shared device, the pointer-only component, and both-at-once, and asserting a
  second run changes nothing. Resolution behaviour is a **deliberate thin cut**: `component.system_id` stays
  and keeps feeding the four cascade resolvers unchanged, so this slice ships and is verified on its own.
  Slice 1 of epic [#324](https://github.com/hyperscaleav/omniglass/issues/324)
  ([ADR-0051](/architecture/decisions/#adr-0051-membership-is-the-attachment-and-a-role-is-what-it-does));
  [core entities](/architecture/core-entities/) and [API](/architecture/api/) stay `Partial`.
