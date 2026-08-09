---
title: Architecture
description: "The architecture told as one journey, following a single reading from the gear through its whole life to the answer and the action on it."
---

Monitoring, stripped down, is one shape: **collect the data, evaluate it, see it, act on it.** The
point is to **know your systems**, and the question that matters most:

> Is this system working right now?

This page follows a **single reading through its whole life**, gear to answer to action. Each
**bold word** is an official term; the linked ones open their deep dive, and every one is defined
in the [glossary](/architecture/glossary/).

:::note[A proposed architecture]
This is a **proposed, forward-looking architecture**: the target design, written in present tense,
not a promise that every detail ships unchanged. Each page carries a status badge, **Design**
(specified, little or none built), **Partial** (some capabilities shipped), **Built** (all shipped
and tested), or **Diverged** (built, but differing from this design, see the page's note); the
badge is the page's floor. The per-capability breakdown lives on
[implementation status](/architecture/status/); undecided design points are flagged inline as
`Open question` asides; how a call was made or reversed, and where the build diverges, lives in the
[decision log](/architecture/decisions/); the epics and the arc ahead are indexed on the
[roadmap](/architecture/roadmap/). Unbuilt prose inside a page sits in a marked **design fence**,
and the pages that are *entirely* unbuilt live in the sidebar's own **Design sketches** group, a
working queue that empties as subsystems land, so a page's place in the nav tells you whether any
of it exists. Every prose architecture page is also published machine-readable at
[/llms-full.txt](/llms-full.txt) (curated index: [/llms.txt](/llms.txt)); the interactive `.mdx`
pages are not included.
:::

## The estate

Three nouns describe what you operate.

- A **[component](/architecture/core-entities/)** is a deployed device, app, or service: a display,
  a DSP, a cloud UCC service.
- A **system** is a set of components that work together to do one job: a meeting room, a
  classroom, a broadcast chain. The word is deliberately universal: a system is the unit you
  actually care about, whatever shape it takes.
- A **location** ties systems and components to a physical place (campus, building, floor, room).

```d2
direction: down
classes: { node: { style.border-radius: 8 }; key: { style: { border-radius: 8; bold: true } } }
location: location { class: node }
system: system { class: key }
c1: component { class: node }
c2: component { class: node }
c3: component { class: node }
location -> system
system -> c1
system -> c2
system -> c3
```

## Collect

AV gear is **agentless**: nothing can be installed inside it, so the reading comes from outside.
Sometimes the component **pushes** it to Omniglass; usually Omniglass **polls** on an interval. Either way, a **[node](/architecture/nodes/)** running close to the gear reaches a
component over an **[interface](/architecture/collection/)** (whatever the device speaks: SNMP, HTTP,
SSH, a control processor's own command language) and reads.

How to reach a class of device, and what to read from it, is declared once in the component's
**[template](/architecture/templates/)**, the reusable device shape. The node runs that and
**parses the answer at the edge**, turning a vendor's raw response into a normalized reading on
the spot.

That normalized reading is a **sample**.

## The sample

A **[sample](/architecture/properties/)** is one reading of one **canonical signal** (`power-state`,
`audio-level`), in one of two lanes: a **metric** is a **quantity** (a number that aggregates: an
average fan speed means something), a **property** is a **value** (what something *is*, including a
number used as a name: input 3, zone 4; values do not aggregate, they have **duration**). It is
owned by exactly one entity through the **exclusive arc**: a component or a system
or a location, never more than one. It carries a **provenance** (how we know it: **observed**
from the device, **[calculated](/architecture/calculations/)** by Omniglass, **intended** by a
**[command](/architecture/commands/)** we sent, or **declared** by an operator) and a **source**
(which sensor or path told us).

The meaning of each signal (its value type, its unit, its validation) lives in a governed
**catalog** per lane (`metric_type`, `property_type`), and a template *references* a registered
signal rather than inventing one, so two displays from
different manufacturers answer the same question the same way: the **measurement** is named, not
the device.

## What it should be

Not every value is measured. Some are **declared**, set by an operator rather than read from a
device. A declared signal value (this input should be HDMI1) is an ordinary **sample row** with
`provenance=declared` in the same series its observed side lands in, so its edit history is the
series itself; a plain **[variable](/architecture/variables/)** just rides down the tree (this
system polls every 30 seconds),
resolved down a **[cascade](/architecture/cascade/)**: set once high, overridden exactly where it
matters. The same
cascade resolves **[tags](/architecture/tags/)** (the governed label vocabulary), encrypted secrets,
and platform **[settings](/architecture/settings/)**, and **[files](/architecture/files/)** attach
alongside them as searchable handles over a content-addressed blob store. A declared signal has an
observed side,
so the gap between intent and reality is **drift**, a signal you can alarm on or a fix you can push
back.

## Detect

An **[event_rule](/architecture/alarms-actions/)** watches a sample and fires when its condition, an
**[expression](/architecture/expressions/)**, is met, recording an
**[event](/architecture/events/)**: our assertion that something happened. Pair a fire
with a clear and the two events open and resolve an **alarm**, the stateful incident, one row per
occurrence, the thing an operator works and a ticket binds to. An alarm impairs its **component's
own verdict** wholesale, turning a detection into a verdict on the system.

## Model health

A single alarm is rarely the point. The headline is **[health](/architecture/health/)**: a verdict on
the **system**, carried as a calculated sample. The chain: a critical alarm takes a component's own
verdict to outage, so it no longer **occupies** the **role** it was filling (a lesser alarm degrades
it but leaves it in place); a role below its **quorum** is
impaired and contributes its declared **impact** (outage, degraded, or none); the system takes the
worst contribution, and a location the worst of its systems. A target on that verdict over time is a
real uptime **SLA**.

The other half is "since when". Health is recorded as a **transition**, written by the change that
caused it rather than by whoever opens a page, so the edges are exact weeks later.

The rollup ships **opinionated by default**, a first-class model rather than a byproduct of the rules
engine, with an escape hatch for the systems the defaults get wrong.

## Act

An **action_rule** subscribes to events and alarms and runs an **[action](/architecture/alarms-actions/)**.
An action can be one step (notify the right person) or many (remediate, wait, re-check the real
sample, escalate if it did not take; or open and close a ticket as the alarm opens and clears).

## See it

The operator never queries raw tables. Reads go through **[views](/architecture/views/)** (a named
query returning a uniform `{columns, rows}`), rendered in the **[console](/architecture/ui/)**: the
fleet-health grid, the "why did this value win" cascade explainer. The console
is one client of the **[API](/architecture/api/)**, the same contract the generated CLI and the
**[AI](/architecture/ai/)** seams (MCP included) drive.

## The journey, end to end

```d2
direction: right

# Shape colors are deliberately omitted: the inline SVG is themed from the site's
# brand tokens in custom.css so it follows Starlight's light/dark toggle. Only
# structure (rounding, dashes, the highlighted key node) lives here.
classes: {
  node: { style.border-radius: 8 }
  key: { style: { border-radius: 8; bold: true } }
}

gear: gear { class: node }
sample: "sample\ncanonical signal" { class: key }
event: event { class: node }
alarm: alarm { class: node }
health: "health\nrolls up the system" { class: node }
action: "action\nnotify · remediate · ticket" { class: node }
declared: "declared\noperator-set" { class: node }
views: "views → console" { class: node }

gear -> sample: collect (node + edge parse)
sample -> event: event_rule
event -> alarm: fire / clear
alarm -> health: impairs the component
alarm -> action: action_rule
action -> gear: command { style.stroke-dash: 4 }
declared -- sample: drift { style.stroke-dash: 4 }
sample -> views
alarm -> views
health -> views
```

## Underneath

The journey rides on a few foundations:

- the **[Storage Gateway](/architecture/storage/)** is the one door to the database; every read and
  write goes through it, which is where **scope** ([identity and access](/architecture/identity-access/))
  is enforced: a permission on every route, a visibility filter on every query. A grant can target a
  **[group](/architecture/groups/)** of principals as well as a single one.
- the **[workers](/architecture/workers/)** are one machinery: durable JetStream consumers over the
  **[messaging](/architecture/messaging/)** subject contract. The built one is the telemetry
  consumer; the rule engine, the clock, and reconcile follow the same shape; no bespoke loops.
- the **[audit](/architecture/audit/)** trail and the operational logs are immutable, append-only
  ground truth: the record of who changed what and what the platform did.
- **[time](/architecture/time/)** is the one primitive that turns the passage of time into events, so
  the rest of the pipeline stays purely event-driven.
- **[scaling and deployment](/architecture/scaling/)**: the single binary is a modular monolith with run
  modes, deployed as one container for a small estate or scaled out on Kubernetes with a distributed
  edge.

Samples are parsed and emitted at the edge, not re-derived from a raw store. Raw payloads are a
debugging aid (a dev raw mode plus failure logging on collection); how much of that to persist, and
for how long, is still being settled.

## The invariants

A handful of patterns hold everywhere:

- **Identity is three columns**: an immutable uuid **`id`** every foreign key stores, a renameable
  operator-typed **`name`** that URLs, CLI arguments, and topics carry, and an optional
  **`display_name`** a human reads. A rename is a custom method (`POST /components/{name}:rename`
  and its siblings) gated on its own, so it moves one column and nothing else
  ([ADR-0076](/architecture/decisions/#adr-0076-a-renameable-human-typed-identifier-stays-in-the-url-and-the-write-returns-the-uuid)).
- **Exclusive-arc ownership**: every sample, event, and alarm names exactly one owner (component,
  system, location, or node), so system- and location-level signals are first-class.
- **Templates fork, nothing pins**: creating from a [template](/architecture/templates/) is a
  one-time clone with no back-pointer, so a template can be rewritten in any release without
  migrating an install ([ADR-0071](/architecture/decisions/#adr-0071-a-template-is-a-clonable-example-not-a-versioned-shape-an-instance-pins)).
- **On-row lineage**: a derived row carries its own evidence; there is no separate execution table.
- **The `official` boolean**: every registry (`metric_type`, `property_type`, `event_type`,
  `command_type`, and the
  catalogs) and rule row carries an `official` boolean: the curated ship-with set, seeded at boot
  and authoritative; the rest is operator-authored and local to a deployment.
- **Views by default**: current-state reads are plain views, materialized only when a profile proves
  it necessary.
- **Not event-sourced**: stateful entities (alarm, action) hold their state directly.
- **Per-database isolation**: there is no tenant column; a tenant is a database.

## Look up any term

Every official term is defined once in the **[glossary](/architecture/glossary/)**, and the deep
pages in the sidebar follow this same journey. The physical schema lives in
[storage](/architecture/storage/), and the generated ERD over it in the
[data model](/architecture/data-model/).
