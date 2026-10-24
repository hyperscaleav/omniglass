---
title: "The console reads a system two ways, review round"
date: 2026-08-08
refs: []
form: bullet
---

- **The console reads a system two ways, review round.** A review pass on the slice above found
  twelve confirmed defects, all fixed. `EffectiveRoleBody` gains `positions`: index-for-index with
  `assigned_to`, each entry's own 1-based position, since drag-to-reorder cannot address a specific
  occupant's slot by assuming index `i` sits at position `i + 1` once an unassign has left a gap
  (#626, never compacted); `swapPath` (the drag decomposition) reads real positions instead. The
  system-colour hue bands were HSL degrees guarding an OKLCH-rendered dot, leaving three of five
  semantic tokens open (about 28% of systems wore a status-coloured dot); recomputed in OKLCH, with
  the fixed-step escape (which clustered on `--color-primary`) replaced by a golden-angle step. Two
  committed D2 diagrams were stale against schema this epic's own earlier tasks changed
  (`data-model-0.svg` still drew the five dropped capability tables and omitted every table this
  epic added) or hand-patched without their generated geometry (`index-1.svg`'s corrected label
  text shipped without the mask and position a d2 SVG computes from it, so the edge line struck
  through the relabelled word); both regenerated. `RolesPanel` reintroduced the exact
  active-flag contradiction the slice above had just fixed on `HealthPanel` one panel over, and
  went one step further, discarding cache invalidation on a failed write (leaving a partial
  multi-swap reorder undetectable) and never invalidating the health read at all on any write
  (freezing its own short/spare/impact badges, now its primary readout, at their pre-write values).
  `api.md`'s roles section, silent on `capacity`, `position_labels`, `positions`, and
  `:swapPositions`, and still calling every assignment refusal "a 422" when a component
  double-staffing another role or a role at capacity are both 409, is corrected to state the same
  409-versus-422 rule ADR-0087 does. `storage.md`'s three-buckets section is where ADR-0087's
  boot-seed carve-out belongs and had never landed; added there (and mirrored in `CLAUDE.md` and
  the `storage-schema-change` skill) rather than existing only in the ADR and this file.
