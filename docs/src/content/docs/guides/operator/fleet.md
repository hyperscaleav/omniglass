---
title: Explore your fleet
description: "Explore: every place in the fleet as one outline, and the detail view for each system, with its place as context."
screenshots:
  # Component names repeat across rooms (every huddle has a videobar-1), so the
  # component is reached the way an operator reaches it: from the degraded
  # auditorium's view through its Why to the component the alarm names. The
  # dispatch walk, exactly.
  - id: fleet-component
    path: /web/systems/auditorium
    steps:
      - action: click
        selector: "[data-testid=alarm-strip] button"
      # The alarm now opens the component's blade (#799); Expand promotes the
      # walk to the component view this shot teaches.
      - action: click
        selector: "aside[data-blade] button[aria-label=Expand]"
    alt: "A component's view: its place as a card saying the place is its own, the verdict with its since-line, why, what it is, the systems it serves with its role in each, and collection."
    # The since-line and the alarm ages count from the capture's own clock.
    mask:
      - "[data-testid=since-line] >> xpath=ancestor::div[1]"
      - "text=/unacknowledged/ >> xpath=ancestor::div[1]"
      - "text=/acknowledged/ >> xpath=ancestor::div[1]"
  - id: fleet-configure
    path: /web/systems/huddle?tab=configure
    alt: "The Configure tab: Identity with the label pen and the name precheck, Classification, Placement, roles, properties and Tags, edited in place on the system's view."
    mask:
      - "[data-testid=since-line] >> xpath=ancestor::div[1]"
  - id: fleet-system
    path: /web/systems/huddle
    alt: "A system's view: the place as a card of context, the system's header (verdict, since, standard, size), then Overview: its components as rows naming each one's role, roles grouped only where they say something."
    # The since-line ages with the capture; the history strip is Activity's
    # now (#872), so nothing else on Overview moves.
    mask:
      - "[data-testid=since-line] >> xpath=ancestor::div[1]"
  - id: fleet-map
    path: /web/systems/huddle
    alt: "The map inside Overview: the standard's declared layout rendered top-down, one marker per role position, solid where staffed and hollow where not."
    # The header's since-line ages with the capture.
    mask:
      - "[data-testid=since-line] >> xpath=ancestor::div[1]"
  # The auditorium carries the fleet's live critical alarm, so the history
  # tab has something real to say. Its room holds that one system, so the
  # room's address lands on it.
  - id: fleet-history
    path: /web/locations/auditorium
    steps:
      - action: click
        selector: "role=tab[name='Activity']"
      - action: click
        selector: "[data-testid=incident-0] button"
    alt: "The Activity tab, statuspage style: the window's uptime, the timeline, the ongoing incident expanded to the alarm that explains it, then the events and the logs."
    mask:
      - "[data-testid=since-line] >> xpath=ancestor::div[1]"
      - "[data-testid=uptime-kpi]"
      - "text=/ongoing/ >> xpath=ancestor::li[1]"
      - "text=/\\u2192/ >> xpath=ancestor::li[1]"
      - ".og-statestrip"
      # The logs under the incidents render seed-run stamps.
      - "[data-testid=logs-tab]"
  - id: fleet-data
    path: /web/systems/huddle
    steps:
      - action: click
        selector: "[data-testid=metric-row-room-temperature]"
    alt: "The data section inside Overview: every declared metric stacked with a sparkline and its latest value, the temperature row expanded to the full chart."
    # The since-line ages with the capture, and every chart x-position
    # divides seed-to-shoot latency by the window (CI proved it crosses a
    # pixel boundary), so the plots mask in the BASELINE while the docs
    # embed them live (the two-render pipeline).
    mask:
      - "[data-testid=since-line] >> xpath=ancestor::div[1]"
      - "[data-testid=timeseries-chart]"
      - "[data-testid=sparkline]"
  - id: fleet-location
    path: /web/locations/east
    alt: "A place holding no system of its own: its card with its type and tags, its tabs, and the outline of what is beneath it, counted by systems."
    # Nothing on a folder's view moves with the clock: no mask.
  # A room holding two systems keeps a view of its own: a brief card per
  # system.
  - id: fleet-place-systems
    path: /web/locations/media-lab
    alt: "A room holding two systems: its card, then a brief card per system with its verdict, standard and size, each opening that system's view."
    mask:
      - "[data-testid=summary-since]"
  # Opened down to one room so the shot shows every rule at once: folded
  # chains, contents lines, health counts, and a place wearing its system
  # with the components beneath it.
  - id: fleet
    path: /web/explore
    steps:
      - action: click
        selector: "role=button[name='Expand West Building']"
      - action: click
        selector: "role=button[name='Expand Level 2']"
      - action: click
        selector: "role=button[name='Expand Huddle Room']"
    alt: "Explore: the outline of places, Headquarters and West Building folded into one row and opened down to the Huddle Room, which wears its system with the components beneath it; every collapsed place says what it holds and counts its systems by health."
---

The fleet has one door in the sidebar: **Explore**. It is an outline of every place you
can read, nested the way your own tree is nested, with each system on the place where it
sits and each component beneath its system. From any row you open a side panel, and from
the panel the **detail view** at the entity's own address. The old `/fleet`, `/locations`,
`/systems` and `/components` addresses all land on Explore.

## Explore

::screenshot{#fleet}

Explore opens with only the top-level places showing, every row collapsed, so the first
screen is the whole fleet in a handful of lines. You open what you want to look into.

**A place wears its system.** An operator thinks of a room as its system, so a place where
one system sits shows that system on its own row: the **Standard** column names the
[standard](/guides/admin/standards/) it conforms to, and **Health** its verdict. Open it and
the system's components are directly beneath, each with its product and health, and a
component's active alarm reads in the **Detail** column. A place holding two systems lists a
row per system, with its components under each. A component two systems share appears under
both, marked **also in** the other; a component in no system sits directly under its place.

**A collapsed row says what it holds.** The Detail column counts what is inside in your
registry's own words (**2 buildings, 1 floor**, **19 components**, **Empty**), and the Health
column counts the systems beneath it by verdict, in four fixed slots: healthy, incomplete,
degraded, outage. Healthy stays grey and only trouble has a colour, so a red count stands out
down the page without being read. The counts include everything beneath the row, so a
collapsed branch cannot hide an outage. They are counts rather than one worst-case colour
because at any real failure rate almost every large place holds one fault somewhere, and a
worst-wins light would be red everywhere and say nothing.

**A chain of one folds.** A place whose only content is one child place folds into it:
**Headquarters / West Building** is one row rather than two levels to click through. Its
**+** offers each place it joins, so giving that campus a second building is the same click
as giving the building a room.

None of this knows the shipped type names. Campus, building, floor and room are defaults,
and a fleet of plots, sectors and coordinates, or any hierarchy your
[location types](/guides/admin/location-types/) allow, nests, folds and counts by the same
rules and is described in its own words.

**Placed nowhere you can see** collects what has no place on the page: a system at a
location your grants do not reach, and a component created with no system and no place.
Nothing disappears because its place is out of view.

### Moving around

- A row's chevron opens it, and **Alt**-click opens its whole branch. Clicking the row itself
  opens its side panel: a place holding one system opens that system, any other place the
  location, a component itself. The panel's **Expand** opens the detail view.
- From the keyboard, the arrows move up and down, Right opens a row and Left closes it or
  moves to its parent, `*` opens the whole branch, Enter or Space opens the side panel, and
  `+` opens the row's add menu.
- What you have opened is remembered in this browser, so coming back lands where you left off.
- On a narrow window the columns give way in order, **Standard or product**, then **Type**,
  then **Detail**, so the name and its health always fit. A folded row's outer path shortens
  before the place's own name does, and at the narrowest it steps aside (hover the name for
  the whole path).

### Finding one thing

The bar above the outline is a **chip filter**. Type a field name, then an operator, then a
value; each commit becomes a chip.

- Within one chip, several values are **OR** (match any); across chips, the filters are
  **AND** (match all). Click a chip's operator to cycle it, its value to re-edit it, and the
  **x** to remove it.
- A bare term matches a name, or any fragment of the path above it, so typing a building's
  name finds everything in that building. `verdict:`, `type:`, `standard:`, `product:` and
  `path:` narrow precisely.
- Every [tag](/architecture/tags/) in use is a field too: choose the tag's key, then a value,
  to match its **effective** value (a component matches a tag it inherits from its system or
  location, not only one set on it directly). **is set** and **is absent** take no value and
  find the rows that carry the tag at all, or lack it.

While a filter is on, the outline becomes a flat list of matches, each with the path above
it. **Enter** on a match shows it in the tree, opened down to it and selected, with the filter
cleared; **Space** or a click opens its side panel. The counts line's **need attention** is
itself the filter: it keeps what is in outage, degraded or incomplete, and it is the same
verdict chip the bar shows, so the two can never disagree.

What the filter bar does **not** offer is a location facet. Filtering on live state (a
verdict, a standard, a type, a name) is exploring; naming the part of your fleet to include
is choosing a scope, which is what a dashboard owns.

The address carries what you were looking at. `?chips=` carries the filter, so a link hands
somebody exactly the matches you saw, and `?node=` reveals one row: a location, system or
component by its id, or by a name that names exactly one thing, so `?node=huddle` lands on
the huddle room, opened down to it and selected.

**Create where you stand.** A row's **+**, or **New** above the outline, offers a location,
a system or a component: the same create [form](/guides/operator/entities/), with the
placement already filled in. It offers a location only where your location types let one
sit, and only what you hold the create permission for.

Everything on the page is already filtered to [your scope](#scope). Verdicts here are a
glance; monitoring lives on the detail views and, later, the dashboards.

## One view per system

Omniglass monitors **systems**. A place is a folder and its facts; a component is a piece of
a system. So the fleet has one detail view per system, and a room does not have a second,
thinner view of its own: a place holding exactly one system (nearly every room) opens **as
that system**. Its address lands there, and so does the panel's Expand.

::screenshot{#fleet-system}

The view reads top-down:

- **The place card**: where the system sits, as context. It names the place's type in your
  registry's words and its tags, and **Place details** opens the place's own panel (its
  form, its Edit) without leaving the view. The view is titled by the place, so the card
  does not name it again, and the breadcrumb ends at the place's parent: the room is named
  once.
- **The system card**: the verdict and **since when** (the last recorded change and its
  age), the standard it is built to, how many components it holds, and slot arithmetic only
  while hardware is missing. Each fact is said once; there is no counts line above it.
- **Three tabs.** **Overview** is the system itself. **Activity** is what happened to it.
  **Configure** is the [form](/guides/operator/entities/) that edits it in place.

A place holding a system and places beneath it lists those places under the system, as the
outline rooted there.

A system's label is its place's by default, so a room and its system read as one name. A
second system of the same kind in the same place adds its kind and number ("Media Lab
Classroom 2"), and a system with no place reads its kind.

## A place's own view

A place holding no system (a campus, a building, a floor) or more than one keeps a view of
its own. Its card is the subject, with its own **Overview** and **Configure** tabs.

::screenshot{#fleet-location}

Overview lists what is beneath the place as the outline rooted there: the same columns,
contents lines, health counts, chip filter, **need attention** and **+** as Explore, counted
over this subtree. A place holding two or more systems shows a brief card per system first
(verdict, since when, standard, size, why), each opening that system's view.

::screenshot{#fleet-place-systems}

An address that asks to configure the place itself (`?tab=configure`, or `?edit=1`, the
handoff from creating one) stays on the place's view even when it holds one system, since
that edit is the place's.

## Overview: why, then the components

Cause before arithmetic:

- **Why** leads, worst first: severity, message, the down component (click it to open its
  panel), the role it impairs, and how long it has been raised.
- **Components** follow, as rows in the outline's idiom: the component, the **role** it
  fills, its product, the alarm behind a red light, and its health (healthy grey, only
  trouble in colour). Click a row to open its panel.
- **Role chrome appears only where it says something a column cannot.** A role that wants
  more than one occupant, is short, or is unstaffed heads a group with its arithmetic ("1
  of 2, 1 spare") and its occupants beneath; a seat nobody staffs draws as an **empty
  slot**. A down occupant still holds its seat: that is a failure the alarms explain, not a
  gap.
- **A deployed system fills every role**, so slot arithmetic appears only while hardware is
  missing. An unstaffed role reads **incomplete** (a commissioning gap); a short staffed
  role reads the impact it declared. Same arithmetic, different cause.
- The build a system did not choose never renders; choices are the standard editor's
  vocabulary, not an operator's.
- A shared component is marked **also in** the other system; a member filling no role reads
  **no role**, and that is a normal state.

## Overview: the map and the data

A standard may declare the system's physical layout (a room, a stage, a vehicle, whatever
the system occupies): where each role position sits, top-down. Every
system built to that standard draws the **map** inside Overview, under the components:
one marker per declared position, solid in the occupant's state (click it to open its
panel), hollow where nobody is staffed. The label is the role, its position number when
the role wants several, and the component holding it.

::screenshot{#fleet-map}

Below the map, when the standard declares metrics, the **data** section stacks every one
of them, the only place a metric's latest value is said: a sparkline of the last 24 hours beside the latest value, one row per series, so
the system's numbers read together. Click a row for the full chart, newest at the right,
the latest sample's value floating on its dot. Raw samples, capped; a series still on its
contract default has nothing to chart yet, and says so.

::screenshot{#fleet-data}

## Activity: the history, the events, the logs

**Activity** reads the way a status page reads: the window's **uptime** up top (the
health KPI over time), the timeline beside it with one marker per alarm raise, then
**incidents**: one entry per contiguous stretch away from healthy, ongoing first, each
expanding to the verdict changes inside it and the alarms that explain them. An alarm the
system absorbed without going unhealthy lists under **other alarms**. A system that flaps
weekly and a system that failed once look different here, which is the point.

::screenshot{#fleet-history}

Under the incidents, the **events** are the system's story on the event lane: the system's
own events and its members', newest first, each row labeled by the owner that raised it;
and the **logs** are the members' raw lines merged, each naming the component that wrote
it. Both cover the last 24 hours, capped; both scope to what you can read. A component's
Activity tab carries its own events the same way.

## Configure

Editing lives on the detail view (#800): the **Configure** tab renders for anyone holding an
edit verb on the entity, and inside it the sections gate one by one. **Identity** carries
the label (with the platform's pen) and the name with its advisory precheck; renaming
breaks bookmarks and integrations by design, so the check and the consequence line sit
beside the field. **Classification** is the standard and type selects (a component's
product is fixed at creation). **Placement** moves a location under a new parent, its own
permission and its own audit verb; systems and components read where they sit.
**Tags** edit in place. One save model everywhere: Edit stages drafts, Save commits them
together (the rename last, so a refusal leaves the rest saved), Cancel reverts. A
`?edit=1` address lands already editing. A place's and a component's views carry the same
tab. The side panel carries the same form's identity, classification and tags; roles,
properties and their cascade are configuration, and live here.

::screenshot{#fleet-configure}

## A component

A component is a piece of a system, so its view opens with its **place card** too, saying
whether the place is the component's own (**set here**) or its system's standing in for one
it does not have (**from its system**). Then the component card: the verdict and since when
(a component has no recorded edges, so since-when is what took it down: the worst active
alarm and its age), its product, and its tabs.

Overview reads **why** (severity, message, age, and whether anyone has acknowledged it; a
cleared alarm is history, not a state), **what it is** (product, vendor, driver, and every
property the contract resolved to a value: model, serial, firmware, the RMA facts), and the
**systems it serves**, each with the role it fills there and the primary marked when there
is more than one. **Vitals** lists the effective metrics that carry a value, a dot marking
the device speaking rather than a contract default standing in. Each section's explainer is
on its label's tooltip.

::screenshot{#fleet-component}

**Collection** shows each interface with its layer rungs (ping answers the path, the port
answers the service), the node, its state, and the last sample age. A stale sample under a
healthy node points at the device or the network path, not at collection. An offline node
says nothing about the device. This card is the one place a node appears on these pages.

## Scope

The page shows only what you can read. Without system read access you see the location
tree with no contents; with no fleet access at all you see an empty page, not an error.
An out-of-scope root is absent.
