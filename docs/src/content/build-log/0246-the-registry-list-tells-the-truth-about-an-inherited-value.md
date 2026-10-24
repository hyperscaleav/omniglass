---
title: "The registry list tells the truth about an inherited value"
date: 2026-08-14
refs: []
form: bullet
---

- **The registry list tells the truth about an inherited value**
  ([#743](https://github.com/hyperscaleav/omniglass/issues/743)). #716 and #742 taught the type
  registries' BLADE to show the value a row inherits and name the ancestor it came from. The LIST
  went on rendering an em dash in the Stem and Abbrev cells for exactly those rows, so the console
  asserted a value was absent on one surface and showed it on another, two clicks apart. Both cells
  now render the value the row takes.

  No new query: `inherited_stem`, `inherited_abbrev` and their `_source` fields already ride the
  listing (ADR-0115), which is also the blade's read, so this is a rendering change over rows already
  in hand.

  **The value only: the provenance mark stays a blade and detail affordance.** A table is for
  scanning values, and the blade is where a value's origin is explained; a reader running down the
  Stem column is answering "what is this type's stem", and a per-row attribution would charge them
  for a question they did not ask. So a stated value and an inherited one render **identically** in a
  table, which is the treatment rather than an omission and is asserted as such. It settles the
  dimming question with it: every value in these columns is `text-base-content/60` already, and
  nothing separates them further.

  The **Icon** cell is the precedent the other two MATCH. It has rendered `resolved_icon` since #695
  with no attribution at all, which is the house treatment for a table; Stem and Abbrev join it, and
  the Icon column changes only in that its fallback chain now lives in the shared cell. One
  `InheritedCell` holds all six cells across the two registries, with one chain (the row's own value,
  else what the server resolved for it, else the em dash), and the em dash keeps its one meaning for
  a row that states nothing with nothing above it.

  The first cut of this slice put the mark in the cells, trailing the value, and was reversed on
  review before merge; the entry above records what shipped, and ADR-0115's third amendment records
  the reasoning.
