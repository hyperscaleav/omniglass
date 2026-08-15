---
title: Explore your fleet
description: "The fleet zoom: the whole fleet on one canvas, every system a cluster of dots under its root location, coloured by what is wrong."
screenshots:
  - id: fleet
    path: /web/fleet
    alt: "The fleet zoom: bands per root location, system dot clusters, dashed holes, the zoom ladder, and the inspector."
    # The dot fields are masked because their colours derive from system
    # uuids (ADR-0123 over hueFor), and every capture run seeds a fresh
    # database: the hues are honest per-install identity and can never be
    # byte-stable across captures. The chrome, the holes, and the counts
    # are the shot's deterministic content. Unmask when the diff gate grows
    # its perceptual threshold (#774).
    mask:
      - "canvas"
---

**Fleet** is the whole of what you operate, on one canvas: every root location as a band,
every system under it as a cluster of dots, one dot per component, coloured by what is
wrong. The [inventory pages](/guides/operator/inventory/) answer "what rows exist"; this
page answers "what is my fleet, and where is it hurting", which is the question you
actually arrive with.

::screenshot{#fleet}

## Reading the canvas

- **A band is a root location.** Its chip is the location's own recorded verdict, the same
  one its detail page shows, so the two can never disagree. The subtitle counts its
  systems, its components, and how many levels deep its tree goes: a campus running
  four levels and a depot running two are both normal, because the place tree has no
  fixed ladder.
- **A cluster is a system; a dot is a component.** A healthy dot wears its system's own
  identity colour, so a healthy fleet reads as coloured wallpaper and the clusters tell
  themselves apart. Anything not healthy takes over the dot with a status colour:
  incomplete, degraded, or outage. Failures are the only status-coloured pixels on the
  canvas, which is what makes them pop.
- **Incomplete is not failure.** A dot or a room can read incomplete because hardware was
  never installed, not because installed hardware broke. Most of a real fleet is
  mid-commissioning for months; the canvas keeps that state visibly distinct from an
  outage instead of painting it red.
- **A ringed dot is shared; a ghost is the same box seen from elsewhere.** A component
  serving two systems draws solid exactly once, in its primary system's cluster, with a
  ring; every other cluster shows a ghost outline. One box, never double-counted.
- **A dashed card is a hole**: a room that exists and holds nothing. Naming the gap is
  half of what the canvas is for; the card is inert until commissioning workflows land.

Hover a dot for its name and verdict. Click a band to zoom into that location; the
browser back button returns to the fleet.

## The ladder and the inspector

The four chips above the canvas are the **zoom ladder**: fleet, location, system,
component. At this zoom only Fleet is live; the deeper chips light up as the later zooms
land, resolved from wherever the page's address points rather than from anything the
console remembers, so a deep link arrives with every chip already correct.

The right-hand **inspector** carries the headline: how many systems, components (a shared
component counted once), and roots you can read, and how many locations hold no system.

## What you see is what you may read

The canvas is scoped tier by tier. If you may read the place tree but not its systems,
you get the shape of your fleet with no contents; with no fleet scope at all you get an
empty canvas, not an error. An out-of-scope root is absent, never blank.
