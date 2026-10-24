---
title: "settings persist only the override level; base layers are recomputed in memory"
date: 2026-07-17
status: "Accepted"
summary: "The settings engine persists only the override level; the `code` and `file` base layers are recomputed in memory each boot, so restore is a delete (diverges from scaling.md's \"materialized in Postgres\")"
---

- **Date:** 2026-07-17 | **Status:** Accepted | **Pages:** [settings](/architecture/settings/), [scaling and deployment](/architecture/scaling/)
- **Decision:** The settings engine stores **only the override level** in Postgres (`setting_override`). The two
  base layers, the embedded `code` defaults and the operator `file`, are **recomputed into memory on every boot**
  and never written to the table, so the effective document is the in-memory base layers merged with the live DB
  override. Restore is therefore a `DELETE`: dropping a namespace's override row (or truncating the scope)
  re-exposes the base defaults, with no separate reset column and no re-seed of the file into the store.
- **Context:** The [scaling](/architecture/scaling/) page sketched a single settings store "materialized in
  Postgres ... seeded declaratively from a settings file reconciled on every boot" (an `ON CONFLICT DO UPDATE` of
  the file into the table). Building the engine showed that materializing the file into the DB is the wrong shape:
  it duplicates the GitOps source into a second authoritative copy that can drift, and it conflates ship-with
  defaults (a compile-time asset) with operator changes (the only thing worth persisting). Keeping the base layers
  in memory makes the file always-fresh (a ConfigMap change lands on restart), keeps the store lean (it holds only
  what an operator actually changed), and makes restore fall out of the model as a delete rather than a re-seed.
  This diverges from the scaling page's "materialized in Postgres" wording; the settings page carries the corrected
  model and the scaling page moves to `Partial`.
- **Closes:** issue [#271](https://github.com/hyperscaleav/omniglass/issues/271) (settings engine slice-0), under
  epic [#270](https://github.com/hyperscaleav/omniglass/issues/270). Design:
  `docs/superpowers/specs/2026-07-17-settings-engine-design.md`.
- **Amended by [ADR-0057](#adr-0057-the-cascades-least-specific-tier-is-platform-and-a-default-is-not-a-tier):** the model is unchanged, the level names are not: `code` is now `default` (off the axis, the setting's own declaration) and `global` is now `platform` (the install-wide rung).
