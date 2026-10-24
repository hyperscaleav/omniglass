---
title: "settings resolve as a cascade over principals with a broader-wins lock"
date: 2026-07-17
status: "Accepted"
summary: "Settings resolve down the principal hierarchy reusing the cascade primitive, with per-key provenance and a top-down broader-wins lock"
---

- **Date:** 2026-07-17 | **Status:** Accepted | **Pages:** [settings](/architecture/settings/), [cascade](/architecture/cascade/)
- **Decision:** A setting's effective value resolves down the **principal** hierarchy (global to group to user),
  reusing the same [cascade](/architecture/cascade/) primitive the estate uses down location to system to
  component: ordered layers deep-merged in JSON map-space (most-specific-wins by key presence), with per-key
  **provenance** (the winning level) reported alongside the value. Layered on top is a **top-down lock**: an admin
  locks a key at a level, pinning that level's value and forbidding any more-specific level from overriding it, and
  when two levels lock the same key the **broader level wins** (a `global` lock supersedes a `group` lock, so
  top-down admin authority is absolute). Slice-0 ships the global rung; group and user are a fast-follow.
- **Context:** Omniglass already had one cascade resolver (the estate's secrets / variables / tags / config,
  [config and credentials](/architecture/variables/)). Rather than write a second resolver for settings, the engine
  points the same primitive at the identity axis (doctrine 5, primitive-first): a value defined once at a broad
  scope inherits below, which is exactly the reuse a variable-reference model (Windmill-style) would buy, provided
  here by inheritance for free. The **lock** is the piece the estate cascade did not need: settings are governance
  (an admin enforcing an org default a user cannot escape), so the engine adds a per-key lock with a broader-wins
  conflict rule, the inverse of the most-specific-wins value rule, applied to the enforcement axis. Provenance
  reuses the estate's effective-values vocabulary (the winning level per key), extended from three estate bands to
  five principal levels plus a lock chip. The pure `settings` package is the primary unit-test target; the DB
  override is supplied through a narrow function seam so the package never imports storage.
- **Closes:** issue [#271](https://github.com/hyperscaleav/omniglass/issues/271) (settings engine slice-0), under
  epic [#270](https://github.com/hyperscaleav/omniglass/issues/270).
- **Amended by [ADR-0057](#adr-0057-the-cascades-least-specific-tier-is-platform-and-a-default-is-not-a-tier):** the model is unchanged, the level names are not: `code` is now `default` (off the axis, the setting's own declaration) and `global` is now `platform` (the install-wide rung).
