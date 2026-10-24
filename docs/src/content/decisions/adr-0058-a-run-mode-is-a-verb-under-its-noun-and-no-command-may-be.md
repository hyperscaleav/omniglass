---
title: "A run mode is a verb under its noun, and no command may be shadowed"
date: 2026-07-22
status: "Accepted"
summary: "A run mode is a verb under its noun, and no command may be shadowed"
---

- **Date:** 2026-07-22 | **Status:** Accepted | **Pages:** [CLI guide](/guides/cli/)
- **Decision:** the edge run mode becomes **`omniglass node run`**, a leaf under the generated
  `node` group, rather than a top-level `node`. A **guard test walks the assembled command tree
  and fails on any duplicate name**, so the hand-written and generated command sets can no longer
  collide silently.
- **Context:** the hand-written run mode and the generated API group both registered as `node`.
  Cobra does not treat that as an error: both are added and lookup returns the first, so **every
  generated node command was unreachable**. `omniglass node list` resolved to the daemon and
  failed asking for `--token`, while the CLI guide documented it as working.
- **Why a guard rather than a rename alone.** This is the third instance: `members` under the
  principal groups (#326), `type list` (#319), and now `node`. Each was found by a person typing
  it. The two command sets compose on one root, so no single file owns the namespace and no review
  of either set can catch it. The tree is the only place they meet, so it is the only place the
  check can live. The guard was written first and **found two more nobody had reported**: `grant
  create` and `grant delete`, where the principal-group variants shadow the principal ones, so
  granting a role to a principal has no CLI path at all (#357).
- **The known collisions are an explicit list that may only shrink.** The guard fails on any name
  **not** on it, so a new collision cannot land, and it also fails on an entry that has **stopped**
  colliding, so a fix must delete its entry rather than leave that name unwatched. It is a ratchet,
  not an allow-list.
- **Root cause, left for #357:** `commandWords` derives the group from a single path segment, so
  `/principals/{id}/grants` and `/principal-groups/{id}/grants` both become `grant`. Fixing that
  renames documented commands and is a naming decision, not a mechanical one.
- **Cost accepted:** `omniglass node` is a documented invocation and it changes. At v0.0.0 that is
  a docs edit, and a mode reads as a verb anyway, beside `node list` and `node enroll`.
- **Tracked as** [#354](https://github.com/hyperscaleav/omniglass/issues/354).
