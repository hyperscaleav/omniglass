# Omniglass Console — UI handoff (reference)

The original Claude Design handoff that seeded the console UI. **The port has
shipped**; this directory is kept only as historical design reference. It is
excluded from the Tailwind content scan and the build.

## What's here

- `ui-guide.md` — the original architecture + ListView-contract + conventions writeup.
- `design-system.html` — the daisyUI 5 / Tailwind 4 / Kobalte token + theme + component spec.

(The runnable React `prototype/` that this handoff originally referenced was scratch
material and was never committed.)

## The current, authoritative docs

This reference is superseded by what shipped. For the live contract use:

- [docs/contributing/design-system.md](../../docs/src/content/docs/contributing/design-system.md)
  — the build/dev guide: the SolidJS + daisyUI stack, the generated typed client, the
  `ListView` shell and its primitives.
- the **`add-inventory-view`** skill (`.claude/skills/add-inventory-view/`) — the recipe for
  a new inventory page (data layer, config, route, nav, tests, invariants).
