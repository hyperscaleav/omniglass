---
title: Explore your fleet
description: "The Fleet page: every system as a cluster of dots grouped by root location, with zoom pages for locations, systems, and components."
screenshots:
  - id: fleet-component
    path: /web/components/service-1?zoom=1
    alt: "The component leaf: product and driver, memberships with the primary marked, and the collection card naming the node."
  - id: fleet-system
    path: /web/systems/huddle?zoom=1
    alt: "The system zoom: one card per role with the reported arithmetic, choices grouped with the build in use marked, and the no-role list."
  - id: fleet-location
    path: /web/locations/east?zoom=1
    alt: "The location zoom: a band per child location, system cards with slot strips, and the allowed child types."
  - id: fleet
    path: /web/fleet
    alt: "The fleet zoom: a band per root location, system dot clusters outlined by system verdict, dashed holes, the zoom ladder, and the rail."
---

**Fleet** shows everything you can read on one page: a band per root location, a cluster
of dots per system, one dot per component. The [inventory pages](/guides/operator/inventory/)
list rows; this page shows status at a glance.

::screenshot{#fleet}

## Reading the canvas

- **A band is a root location.** Its chip shows the location's recorded verdict, the same
  value its detail page shows. The subtitle counts systems, components, and how many
  levels deep its tree goes.
- **A cluster is a system; a dot is a component.** A dot's colour is that component's
  verdict: green healthy, or incomplete, degraded, outage. The outline around a cluster is
  the **system's** verdict, quiet when healthy and tinted when not. Two colours, two facts:
  the outline says how the room reads, the dots say which boxes.
- **Incomplete is not failure.** It means hardware was never installed, not that installed
  hardware broke.
- **A ringed dot is shared.** A component in more than one system draws solid once, in its
  primary system, with a ring; other clusters show a ghost outline. It is counted once.
- **A dashed card is a location with no system.** It is inert for now.

Hover a dot for its name and verdict. Click a band to open that location; the browser back
button returns here.

## The ladder and the rail

The four chips above the canvas are the zoom levels: fleet, location, system, component.
Chips resolve from the current address, so a shared deep link arrives with the chips
already correct.

The right-hand rail is the same on every zoom: totals for what is in scope (a shared
component counted once), a bar showing the share of components at each verdict, the
location types present, a worst-first list of what needs attention (click a row to open
it), and a footer counting locations with no system.

The dashed **+** cards mark where a location or a system would go. They do nothing yet.

## Zoom into a location

Clicking a band opens the location at its own address with `?zoom=1`. Without the param,
the same address shows the inventory detail.

::screenshot{#fleet-location}

One band per direct child, whatever its type, plus a **placed here** band for systems
attached to this location itself. Each system is a card: a status dot and border, the room
and standard, a **slot strip** (one square per slot the standard wants: filled squares in
the occupant's state, empty squares outlined), and a line saying how many required slots
are empty, how many are down, or that all are filled. Locations in the subtree with no
system render as **+ System** cards under the child that contains them. The **+ Location**
card names which location types this one may contain. The breadcrumb is the ancestor
chain; each crumb is a link that keeps the zoom.

## Zoom into a system

One card per role: what it accepts (and any pinned products), who fills it (with position
labels where the role declares them), and the reported arithmetic.

::screenshot{#fleet-system}

- A role nobody staffed reads **incomplete**. A role whose occupant is down shows the
  impact the role declared. Same arithmetic, different cause.
- Roles group by **choice**, and only the build in use is shown, named ("built as
  all-in-one"). The alternate a room did not choose is a configuration fact for the standard
  editor, not something an operator watches.
- A shared occupant is tagged with the other system it serves. Members filling no role
  are listed at the bottom; that is a normal state.

## The component leaf

The leaf shows **what it is** (product, vendor, driver) and **where it sits** (the ancestor
chain, each level a link; the type path; the primary system). **Slots it fills** lists one
row per system membership with the room beside it and the primary marked. When there is
more than one membership, the location shown above comes from the primary.

::screenshot{#fleet-component}

**Collection** shows the node, its state, and the last sample age. A stale sample under a
healthy node points at the device or the network path, not at collection. An offline node
says nothing about the device. This card is the one place a node appears on these pages.

## Scope

The page shows only what you can read. Without system read access you see the location
tree with no contents; with no fleet access at all you see an empty page, not an error.
An out-of-scope root is absent.
