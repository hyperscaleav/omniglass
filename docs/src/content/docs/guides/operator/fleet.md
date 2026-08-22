---
title: Explore your fleet
description: "The Fleet page: every system as a cluster of dots grouped by root location, with zoom pages for locations, systems, and components."
screenshots:
  # Component names repeat across rooms (every huddle has a videobar-1), so the
  # leaf is reached the way an operator reaches it: from the location zoom into
  # the degraded auditorium, then through the alarm strip to the component the
  # alarm names. The dispatch walk, exactly.
  - id: fleet-component
    path: /web/locations/east
    steps:
      - action: click
        selector: "text=Auditorium"
      - action: click
        selector: "[data-testid=alarm-strip] button"
    alt: "The component leaf: the verdict with its since-line, the active alarm, product and identity properties, the memberships, and the collection card."
    # The since-line and the alarm ages count from the capture's own clock.
    mask:
      - "[data-testid=since-line]"
      - "text=/unacknowledged/ >> xpath=ancestor::div[1]"
      - "text=/acknowledged/ >> xpath=ancestor::div[1]"
  - id: fleet-system
    path: /web/systems/huddle
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
  - id: fleet-map
    path: /web/systems/huddle?tab=map
    alt: "The Map tab: the standard's declared room rendered top-down, one marker per role position, solid where staffed and hollow where not."
    # The header's since-line ages with the capture (baseline only; the docs
    # embed the clean render).
    mask:
      - "[data-testid=since-line]"
  # The auditorium (reached through its band) carries the fleet's live
  # critical alarm, so the history tab has something real to say.
  - id: fleet-history
    path: /web/locations/east
    steps:
      - action: click
        selector: "text=Auditorium"
      - action: click
        selector: "role=tab[name='History']"
      - action: click
        selector: "[data-testid=incident-0] button"
    alt: "The History tab, statuspage style: the window's uptime, the timeline, and the ongoing incident expanded to the alarm that explains it."
    mask:
      - "[data-testid=since-line]"
      - "[data-testid=uptime-kpi]"
      - "text=/ongoing/ >> xpath=ancestor::li[1]"
      - "text=/\\u2192/ >> xpath=ancestor::li[1]"
      - ".og-statestrip"
      - "text=/\\(\\d+[smh] ago\\)/ >> xpath=ancestor::div[1]"
      - "text=/\\d+[smh] and counting/ >> xpath=ancestor::div[1]"
      - "text=/held \\d+[smh]/ >> xpath=ancestor::div[1]"
  - id: fleet-data
    path: /web/systems/huddle?tab=data
    steps:
      - action: click
        selector: "[data-testid=metric-row-room-temperature]"
    alt: "The Data tab: every declared metric stacked with a sparkline and its latest value, the temperature row expanded to the full chart."
    # The since-line ages with the capture, and every chart x-position
    # divides seed-to-shoot latency by the window (CI proved it crosses a
    # pixel boundary), so the plots mask in the BASELINE while the docs
    # embed them live (the two-render pipeline).
    mask:
      - "[data-testid=since-line]"
      - "[data-testid=timeseries-chart]"
      - "[data-testid=sparkline]"
  - id: fleet-location
    path: /web/locations/east
    alt: "The location zoom: the breadcrumb, the verdict header with the needs-attention chip, a band per child location, system cards with slot strips, and the allowed child types."
    # The since-line ages with the capture.
    mask:
      - "[data-testid=since-line]"
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

- **The summary rail** on top, the same shape as the inventory pages: a verdict mix bar,
  how many need attention (click to show only those), and the counts. Expand it for the
  verdict donut with a clickable legend and the count tiles. **The summary reflects the
  page it is on**: the fleet page counts the fleet, a location its own subtree, a system
  its own components (slots, alarms, shared members), the leaf itself.
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

## The list view

The toggle at the top right swaps density, not data. On the fleet it replaces the canvas
with the index tables under three kind tabs, Locations, Systems, and Components: the same
tree, chip filter, and blades the [inventory guide](/guides/operator/inventory/) teaches.
Inside a location it lists the subtree one row per system, verdict first; a row opens the
system full screen, exactly as its card would. The view rides the address (`?view=list`),
so a pasted link lands on the same face, and the old `/locations`, `/systems`, and
`/components` addresses land on their tabs.

## Zoom into a location

Clicking a band opens the location at its own address: the zoom **is** the identity
route's face. The classic detail face stays reachable at `?view=detail` while editing
still lives there; it retires when edit moves into the blades.

::screenshot{#fleet-location}

One zoom down, the header repeats the shape the system zoom set: the location's verdict,
since when, and a **needs-attention count** for this subtree that applies the worst-first
filter on click. Marks are **square components** inside a **system outline** (the outline
is the system's verdict, quiet when healthy). One band per direct child, whatever its type, plus a **placed here** band for systems
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
- **KPI tiles**, when the system's standard declares metrics: the latest sample per
  series, or the contract default until one lands.
- **History** is the verdict over the recorded window; the model it teaches is on the
  label's tooltip, like every explainer on these pages.
- Then the room itself, **components first**: one card per component (name, product, its
  state) with its role as a **badge**. Click a card to open the leaf.

::screenshot{#fleet-system}

- **Role chrome appears only where it says something a badge cannot.** A role that wants
  more than one occupant, is short, or is unstaffed renders as a grouped outline with its
  arithmetic ("1 of 2 + 1 spare") and its occupants inside; empty slots draw dashed. The
  common room (one role, one healthy occupant) is a flat row of cards.
- **A deployed room fills every role**, so slot arithmetic appears only while hardware is
  missing. An unstaffed role wears **incomplete** (a commissioning gap); a down occupant
  wears the impact its role declared. Same arithmetic, different cause.
- The build a room did not choose never renders; choices are the standard editor's
  vocabulary, not an operator's.
- A shared occupant is badged with the other system it serves; a member filling no role
  is a card with a "no role" badge, and that is a normal state.

## The map

A standard may declare the room's layout: where each role position sits, top-down. Every
system built to that standard gets the **Map** tab for free, one marker per declared
position: solid in the occupant's state (click it to open the leaf), hollow where nobody
is staffed. The label is the role, its position number when the role wants several, and
the component holding it.

::screenshot{#fleet-map}

## The history

Every system carries the **History** tab, read the way a status page reads: the window's
**uptime** up top (the health KPI over time), the timeline beside it with one marker per
alarm raise, then **incidents**: one entry per contiguous stretch away from healthy,
ongoing first, each expanding to the verdict changes inside it and the alarms that explain
them. An alarm the room absorbed without going unhealthy lists under **other alarms**. A
room that flaps weekly and a room that failed once look different here, which is the
point.

::screenshot{#fleet-history}

## The events and the logs

The **Events** tab is the room's story on the event lane: the system's own events and its
members', newest first, each row labeled by the owner that raised it. The **Logs** tab is
the members' raw lines merged, each naming the component that wrote it. Both cover the
last 24 hours, capped; both scope to what you can read.

## The data

The **Data** tab stacks every metric the standard declares: a sparkline of the last 24
hours beside the latest value, one row per series, so the room's numbers read together.
Click a row for the full chart, newest at the right, the latest sample's value floating
on its dot. Raw samples, capped; a series still on its contract default has nothing to
chart yet, and says so.

::screenshot{#fleet-data}

## The component leaf

The leaf opens the way every zoom does: the verdict, and since-when. A component has no
recorded edges, so its since-when is what took it down: the worst active alarm and its
age, with the **active alarms** listed under the header (severity, message, age, and
whether anyone has acknowledged them). A cleared alarm is history, not a state, and does
not appear.

Below, **what it is** (product, vendor, driver, and every property the contract resolved
to a value: model, serial, firmware, the RMA facts) and **where it sits** (the ancestor
chain, each level a link with its type as the tooltip; the primary system). **Vitals**
lists the effective metrics that carry a value, the latest sample per series, a dot
marking the device speaking rather than a contract default standing in. **Slots it
fills** lists one row per system membership, the room beside it when the room says
something the system's name does not, and the primary marked. When there is more than one
membership, the location shown follows the primary system.

::screenshot{#fleet-component}

**Collection** shows each interface with its layer rungs (ping answers the path, the port
answers the service), the node, its state, and the last sample age. A stale sample under a
healthy node points at the device or the network path, not at collection. An offline node
says nothing about the device. This card is the one place a node appears on these pages.

## Scope

The page shows only what you can read. Without system read access you see the location
tree with no contents; with no fleet access at all you see an empty page, not an error.
An out-of-scope root is absent.
