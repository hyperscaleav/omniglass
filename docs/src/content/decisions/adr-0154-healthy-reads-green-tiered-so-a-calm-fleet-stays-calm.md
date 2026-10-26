---
title: "Healthy reads green, tiered so a calm fleet stays calm"
date: 2026-10-23
status: "Accepted"
summary: "Healthy reads green, reversing the grey healthy of [ADR-0137](#adr-0137-explore-is-the-fleets-door-and-it-is-an-outline-of-places) and [ADR-0139](#adr-0139-a-detail-view-is-one-per-system-its-place-a-card-of-context) (#1067): one `--og-healthy` token from the success hue, full weight on the health pill and the history strip, muted (`text-healthy`) on the outline's slots and the uptime figure; unknown keeps the grey chip"
---

- **Date:** 2026-10-23 | **Status:** Accepted | **Pages:** [health](/architecture/health/), [design system](/contributing/design-system/), [fleet guide](/guides/operator/fleet/)
- **Decision:** A healthy verdict wears green, reversing the "healthy is grey, only trouble carries
  a hue" rule of [ADR-0137](#adr-0137-explore-is-the-fleets-door-and-it-is-an-outline-of-places) and
  [ADR-0139](#adr-0139-a-detail-view-is-one-per-system-its-place-a-card-of-context). One token,
  `--og-healthy` (the theme's success hue), in three tiers: `badge-healthy` (the health pill, the
  simulator's running pill) and `bg-healthy` (the history strip's healthy stretch) at the trouble hues'
  weight, and `text-healthy`, a muted green, on the dense views (the outline's healthy slot and glyph,
  a good uptime figure). Unknown keeps the grey `badge-ghost` chip.
- **Context:** In use, the grey healthy chip and the grey unknown chip read as one state, so an
  operator could not tell a system known to be fine from one never read (#1067). The concern #872 was
  answering, that a fleet of loud green would bury the one amber branch, holds for the dense views, so
  they take the muted tier; the single-subject surfaces (a pill, a strip) take the full hue, where there
  is nothing for green to drown out. The word and the glyph still carry the state for a colour-blind
  reader. **Amended (#1071):** the system map's occupied-position dot, missed in the first pass,
  wears its component's own verdict through one `verdictDot` primitive: healthy the muted tier
  (`bg-healthy-muted`, the fill twin of `text-healthy`), incomplete indigo, degraded amber, outage red,
  read through the same lookup the component rows use so the two never disagree. A known verdict always
  wins; only an unknown one falls back to the staffing read (red when down, else grey), and an empty
  position keeps its hollow grey ring. **Amended again (#1074):** the dot gives way to the component
  type's icon, itself in the verdict's hue (`verdictHue`, the text twin, which the outline's slots now
  share), with the component's label under it and its role, product, health and alarm in a tooltip;
  `verdictDot` and `bg-healthy-muted` retire with the dot. An empty position is a dashed ring named for
  its role.
