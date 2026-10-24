---
title: "The system map's dots wear each component's verdict (#1071)"
date: 2026-10-23
refs: [1071]
---

The map filled every occupied, up position with the retired grey and only a down one red, so a
degraded component and a healthy one looked alike, beside coloured glyphs on the same page. Each dot
now wears its component's own verdict through a `verdictDot` primitive in `lib/health.ts` (healthy the
muted `bg-healthy-muted`, incomplete indigo, degraded amber, outage red, unknown grey), read through
the one lookup the component rows also use, recorded as an amendment to
[ADR-0154](/architecture/decisions/#adr-0154-healthy-reads-green-tiered-so-a-calm-fleet-stays-calm).
The theme guard covers the new fill with the other three.
