---
title: Explore your fleet
description: "The Fleet page: every system as a cluster of dots grouped by root location, with zoom pages for locations, systems, and components."
screenshots:
  # Component names repeat across rooms (every huddle has a videobar-1), so the
  # leaf is reached the way an operator reaches it: from the system zoom, by
  # clicking the occupant.
  - id: fleet-component
    path: /web/systems/huddle?zoom=1
    steps:
      - action: click
        selector: "[data-testid=slot-conf-bar] button:has-text('videobar-1')"
    alt: "The component leaf: product and driver, the location chain, the slot it fills with the primary marked, and the collection card."
  - id: fleet-system
    path: /web/systems/huddle?zoom=1
    alt: "The system zoom: one card per role with the reported arithmetic, choices grouped with the build in use marked, and the no-role list."
    # The since-line and the history rows age with the capture (edge stamps
    # are seed-run times, the relative ages count from the capture's own
    # clock), so they mask: the header line whole, and each history row via
    # its own moving text, the entity blade's exact selectors. The strip
    # masks too: its span weights divide seed-time gaps by the capture's own
    # age, the breathing #780 measured as a 40px flap on the blade.
    mask:
      - "[data-testid=since-line]"
      - ".og-statestrip"
      - "text=/\\(\\d+[smh] ago\\)/ >> xpath=ancestor::div[1]"
      - "text=/\\d+[smh] and counting/ >> xpath=ancestor::div[1]"
      - "text=/held \\d+[smh]/ >> xpath=ancestor::div[1]"
  - id: fleet-location
    path: /web/locations/east?zoom=1
    alt: "The location zoom: the breadcrumb, a band per child location, system cards with slot strips, and the allowed child types."
  - id: fleet
    path: /web/fleet
    alt: "The fleet zoom: the summary rail, the filter bar, a band per root location with one round mark per system, and dashed holes."
---

**Fleet** shows every system you can read on one page: a band per root location, one round
mark per system, coloured by the system's verdict, worst first. The
[inventory pages](/guides/operator/inventory/) list rows; this page answers which systems
need you and how many.

::screenshot{#fleet}

## Reading the page

- **The summary rail** on top, the same shape as the inventory pages: badges for systems (with
  a verdict mix bar), how many need attention (click to show only those), gaps, components,
  and roots. Expand it for the verdict donut with a clickable legend and the count tiles.
  The summary is fleet-wide on every zoom.
- **The filter bar** narrows the canvas by verdict, system name, or room, with the same chips
  the inventory pages use.
- **A band is a root location.** Its chip shows the location's recorded verdict. The subtitle
  counts systems, components, and levels.
- **A round mark is a system.** Its colour is the system's verdict: green healthy, or
  incomplete, degraded, outage. Marks order worst first inside a band. **Round means system;
  square means component**, one zoom down.
- **Incomplete is not failure.** It means hardware was never installed, not that installed
  hardware broke.
- **A dashed card is a location with no system.** It is inert for now.

Hover a mark for the system and its room. **Click a mark to open the system in the blade**:
its health panel, and buttons to open the system or its location. Click a band to open the
location; the browser back button returns here.

## Moving between zooms

Every zoom keeps the same layout: the summary rail, the filter bar, then the zoom's own
content. Click a band or a card to go deeper; the breadcrumb above the title walks back out
(Fleet, then each location, then the system a component belongs to). Right-hand drawers open
only as detail blades.

The dashed **+** cards mark where a location or a system would go. They do nothing yet.

## Zoom into a location

Clicking a band opens the location at its own address with `?zoom=1`. Without the param,
the same address shows the inventory detail.

::screenshot{#fleet-location}

One zoom down, marks are **square components** inside a **system outline** (the outline is
the system's verdict, quiet when healthy). One band per direct child, whatever its type, plus a **placed here** band for systems
attached to this location itself. Each system is a card: a status dot and border, the room
and standard, a **slot strip** (one square per slot the standard wants: filled squares in
the occupant's state, empty squares outlined), and a line saying how many required slots
are empty, how many are down, or that all are filled. Locations in the subtree with no
system render as **+ System** cards under the child that contains them. The **+ Location**
card names which location types this one may contain. The breadcrumb is the ancestor
chain; each crumb is a link that keeps the zoom.

## Zoom into a system

The header answers the first two operator questions: the verdict, and **since when** (the
last recorded change and its age). Below it, cause before arithmetic:

- **Active alarms** lead, worst first: severity, message, the down component (click it to
  open the leaf), the role it impairs, and how long it has been raised.
- **History** is the verdict over the recorded window, one span per change, never a
  sample.
- Then one card per role: what it accepts (and any pinned products), who fills it (with
  position labels where the role declares them), and the reported arithmetic, including
  spares beyond quorum ("1 of 1 + 1 spare").

::screenshot{#fleet-system}

- **A deployed room fills every role**, so slot arithmetic appears only while something is
  missing; a full house says nothing about slots. A role nobody staffed reads
  **incomplete** (a commissioning gap); a role whose occupant is down shows the impact the
  role declared. Same arithmetic, different cause.
- Roles group by **choice**, and only the build in use is shown, named ("built as
  all-in-one"). The alternate a room did not choose is a configuration fact for the standard
  editor, not something an operator watches.
- A shared occupant is tagged with the other system it serves. Members filling no role
  are listed at the bottom; that is a normal state.

## The component leaf

The leaf shows **what it is** (product, vendor, driver) and **where it sits** (the ancestor
chain, each level a link with its type as the tooltip; the primary system). **Slots it
fills** lists one row per system membership, the room beside it when the room says
something the system's name does not, and the primary marked. When there is more than one
membership, the location shown follows the primary system.

::screenshot{#fleet-component}

**Collection** shows the node, its state, and the last sample age. A stale sample under a
healthy node points at the device or the network path, not at collection. An offline node
says nothing about the device. This card is the one place a node appears on these pages.

## Scope

The page shows only what you can read. Without system read access you see the location
tree with no contents; with no fleet access at all you see an empty page, not an error.
An out-of-scope root is absent.
