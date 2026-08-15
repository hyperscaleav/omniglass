---
title: Explore your fleet
description: "The fleet zoom: the whole fleet on one canvas, every system a cluster of dots under its root location, coloured by what is wrong."
screenshots:
  - id: fleet-system
    path: /web/systems/huddle?zoom=1
    alt: "The system zoom: one card per typed slot with the server's arithmetic, choices grouped with the active alternate marked, and the no-role strip."
  - id: fleet-location
    path: /web/locations/east?zoom=1
    alt: "The location zoom: child bands whatever their type, the placed-here systems as cards with the server's quorum arithmetic, and the allowed child types beneath."
    # Canvas dot strips masked for the reason fleet's are: the hues are
    # per-install identity (ADR-0123) and every capture seeds fresh.
    mask:
      - "canvas"
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

## Zoom into a location

Clicking a band lands the **location zoom** at the location's own address with `?zoom=1`
(the plain address keeps showing the inventory detail; the param may become the default as
the medium settles). One level down, the same canvas: a band per **direct child, whatever
its type** (a campus holding a building beside an open-air area is two bands, because the
place tree has no fixed ladder), and a **placed-here** band first for the systems attached
to this location itself.

::screenshot{#fleet-location}

Systems render as **cards** here, where there is room for arithmetic: the recorded verdict,
the component dot strip, and each impaired role's shortfall in the server's own figures
("1 of 2 satisfying"). An unstaffed role reads **incomplete**, never its failure impact:
nothing has failed, something is missing. Holes in the subtree render dashed under the
child that contains them, and the footer names which location types this one may contain.
The breadcrumb walks the real ancestor chain; every crumb is a live link carrying the zoom.

## Zoom into a system

Clicking a system card lands the **system zoom**: the typed slots this system needs filled,
one card per role, each naming what it accepts (and any pinned products), who occupies it
(with position labels where the role declares them), and the arithmetic exactly as the
server reported it.

::screenshot{#fleet-system}

- **A gap tells you which kind it is.** A role nobody staffed wears **incomplete**: a box
  nobody installed. A role whose occupant is down wears the failure the role declared. Same
  arithmetic, different cause, different colour.
- **Choices render as the builds they are.** A role may belong to an alternate within a
  choice, and only the best-satisfied alternate answers it. The answering build is marked;
  the build this room did not choose renders quiet and dashed, its figures never presented
  as reasons for the verdict, because they did not contribute.
- **A shared occupant is chipped** with the other system it serves, and the members filling
  no role sit in their own strip at the bottom: in the room, accounted for, and that is a
  state, not an error.

## What you see is what you may read

The canvas is scoped tier by tier. If you may read the place tree but not its systems,
you get the shape of your fleet with no contents; with no fleet scope at all you get an
empty canvas, not an error. An out-of-scope root is absent, never blank.
