---
title: "A map marker reads like its row, and anchors inward at an edge (#1074)"
date: 2026-10-26
refs: [1074, 1072]
---

Each occupied position on the system map is now its component type's icon in its verdict's hue, with
the component's label under it, read through one member context the Components rows also use, so a
marker and its row cannot disagree: the same display label, the same glyph, the same verdict. The
role, product, health word, and any alarm ride a tooltip on hover or keyboard focus; an empty position
is a dashed ring named for its role, and it takes focus too. A marker near a side anchors inward and one
near the bottom puts its label above the icon (`markerAnchor` in `lib/system_map.ts`), which eases
#1072's edge clipping at desktop widths; a narrow map can still spill a label (the thresholds are
fractions, the label is pixels), and label collision in a crowded room stays open on #1072. Markers are
keyed by their declaration slot, so a health poll updates them in place rather than remounting them
under an open tooltip. `verdictHue` replaces #1071's `verdictDot` and the outline's own hue map, so one
primitive tones every glyph that stands for a verdict
([ADR-0154](/architecture/decisions/#adr-0154-healthy-reads-green-tiered-so-a-calm-fleet-stays-calm)).

This work first merged on 2026-10-23 as #1075, but into its stacked base branch after that branch had
already landed, so it never reached main; this entry records the re-land. Proven by the component tests
that pin a marker's label and glyph to its row's, its verdict hue across a polled read, the tooltip's
detail on focus, the marker node surviving a poll, and edge markers anchoring inward, plus unit tests
of `verdictHue` and `markerAnchor`.
