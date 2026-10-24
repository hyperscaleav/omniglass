---
title: "The standard declares the room map, normalized and display-only"
date: 2026-08-20
status: "Accepted"
summary: "A standard may declare a room map: one validated jsonb value (aspect + normalized 1-based role positions) on `standard`, display-only, rendered by every conforming system's Map tab; JSON null clears; the visual editor stays out of v1"
---

- **Date:** 2026-08-20 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/), [ui](/architecture/ui/)
- **Decision:** A standard may declare a room map: one jsonb value (`map` on `standard`)
  holding an aspect ratio and one normalized position per role position (`{role, position,
  x, y}`, coordinates in [0, 1], positions 1-based to match `position_labels`). Data, not
  code: every system conforming to the standard renders the same room for free, the join of
  "where this position sits" and "who holds it" made client-side from the health read.
  Validated in Go on write (positive aspect, bounds, no duplicate pair); a JSON null clears
  it; nothing queries into the blob, which is why it is a value and not a table. Render-only
  in v1: authored through the standard PATCH (or seed YAML), the visual editor deliberately
  out.
- **Context:** The system workspace epic (#788) ruled a top-down room view onto the system
  zoom ("Mic 1 · Shure MXA910" at its wall position, live status on each marker). The map is
  a property of the DESIGN, not of one room, so it lives on the standard, the same
  data-not-code guardrail the driver arc carries; a relational table per position would buy
  queryability nothing displays and cost a migration per shape change.
