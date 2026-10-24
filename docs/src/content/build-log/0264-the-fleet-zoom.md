---
title: "The fleet zoom"
date: 2026-08-19
refs: []
form: bullet
---

- **The fleet zoom**
  ([#633](https://github.com/hyperscaleav/omniglass/issues/633)). The first pixels of the one
  canvas: `/fleet` renders one band per root location (recorded-verdict chip, type, counts with
  the subtree depth), each system a wrapping cluster of dots painted on a per-band canvas, the
  holes as dashed inert cards, the four-chip zoom ladder, and the inspector aside. The pure core
  (layout, state buffer, paint grouping, hit test, ladder resolution, breadcrumb arithmetic)
  decides everything an operator could dispute and unit-tests with no DOM; the canvas shell does
  I/O in one paint function. On the canvas, health colour is exceptional and identity colour is
  the ground ([ADR-0125](/architecture/decisions/)); a band chip renders the location's recorded
  verdict, never a client fold ([ADR-0125](/architecture/decisions/)). The lab layout ported
  with its three placement bugs fixed and pinned; its transient heat lanes are cut until an
  event channel exists to feed them (#631, #632, #633, #634).
