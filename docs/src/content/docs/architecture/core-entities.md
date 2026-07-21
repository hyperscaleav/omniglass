---
title: Core entities
description: "The estate model: component, system, location, and node as the structural entities, the variable-depth trees, and the exclusive-arc owner."
sidebar:
  badge:
    text: Partial
    variant: note
---

Core entities are the things an operator actually manages, the component, system, location, and node, and giving each its own identity is what lets every datapoint, event, alarm, and config name exactly one of them as its owner. This page covers the structural entities, how they
nest, and how everything else names one of them as owner. The shapes these entities pin are [templates](/architecture/templates/); the data they own is
[datapoints](/architecture/datapoints/); the physical tables are [storage](/architecture/storage/).

:::note[Partial]
Built today: `component`, `system`, and `location` as name-addressable, variable-depth (`parent_id`)
trees with full scoped CRUD; a delete is refused while a structural child remains. `system` and
`location` each keep a `*_type` registry, while a **component's shape comes from its `product`**
(the `component_type` registry retired,
[ADR-0047](/architecture/decisions/#adr-0047-the-fields-fold-product_property-and-property_value)). The
**exclusive-arc** owner columns are now real, carrying the datapoint sinks, the **`event`** log sink
(see [The event sink](#the-event-sink-the-first-arc-owned-occurrence) below), and **`property_value`**
(see [Declared properties](#declared-properties-the-product-contract-and-the-value-store) below);
`alarm` is the remaining arc-owned sink still `Design`. Still `Design`: template pinning, `system_member`
composition, operational mode, and decommission / purge. See [implementation status](/architecture/status/).
:::

## The estate: four structural entities

Three nouns describe what you operate, plus the edge process that collects for them.

- A **component** is a deployed device, app, or service: a display, a codec, a DSP, a control
  processor, a cloud UCC service. It owns datapoints, pins a `component_template_version`, and points
  at the **`product`** it is, which is where its shape comes from (its vendor, its driver, the
  capabilities it provides, and the properties it declares). The pointer is optional: a productless
  component is legal, it simply carries no contract.
- A **system** is a set of components that work together to do one job. A meeting room is a system.
  So is a classroom, a video wall, a broadcast chain. The word is deliberately universal: a system
  is the unit you actually care about, whatever shape it takes. It pins a `system_template_version`,
  is located at a location, and is classified by `system_type`.
- A **location** ties systems and components to a physical place (campus, building, floor, room).
  It is classified by `location_type` and, unlike component and system, has **no template**: for a
  location the type is the only shape-definer. The official `location_type` set ships seeded and is
  readable at `GET /types/location` (alphabetically by display name), which is what the type picker on the location form
  lists so a location is classified by a known type rather than a free-typed string. Each type also
  carries an `icon` (a glyph key like `building` or `landmark`) that the console renders as the
  leading glyph on every location of that type, so a campus reads differently from a building at a
  glance in the tree; an unknown key falls back to `map-pin`. Each type also carries
  `allowed_parent_types` (a set of `location_type` ids and/or the reserved `root` sentinel), the
  placement constraint: empty means unconstrained, a non-empty set is enforced on create and move,
  and the four official types ship it seeded (`campus={root}`, `building={root,campus}`,
  `floor={building,campus}`, `room={floor,building,campus}`).
- A **node** is the edge process (`omniglass --mode node`) that pulls work, reaches components over
  interfaces, and ships results ([nodes](/architecture/nodes/)). It is structural because it is a
  first-class **owner**: a node owns its own self-health telemetry and can carry a node-owned alarm.

A component belongs to a system; a system sits in a location.

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

Above the four sits the singleton **`global`** estate root: the top owner above every location where
estate-wide health and KPIs roll up, and the top of the [cascade](/architecture/cascade/). One per
deployment, no FK.

| Entity | What it is | Key columns |
|---|---|---|
| `component` | a deployed instance (`dsp-boardroom-3`) | name (unique), **parent_id** (self-ref tree), display_name; pins a `component_template_version`; carries **`product_id`** (optional, `on delete restrict`), the source of its shape |
| `system` | a composition of components / subsystems (the service tree) | name (unique), type, **parent_id** (self-ref tree), display_name; pins a `system_template_version`; carries `location_id`; classified by `system_type` |
| `location` | a place tree | name (unique), type, **parent_id** (self-ref tree), display_name; no template (the `location_type` is the only shape-definer) |
| `node` | the edge process | name (the identity); carries labels, last_heartbeat_at, and its bound credential ([identity and access](/architecture/identity-access/)) |

### Catalog reference data: vendor, driver, capability

The **component-classification catalogs** are flat, seed-and-custom registries, not structural
entities: each names a reusable fact the `product` layer pins, on the same official/custom
pattern as the `*_type` registries ([Types guide](/guides/admin/types/)). Three leaf catalogs ship
here; the **`product`** catalog (below) sits above them as the concrete SKU a `component` points at:

- A **`vendor`** (Crestron, Biamp, QSC, ...) names an organization in the estate model, carrying a
  **`kind`** of `manufacturer`, `integrator`, or `developer` (default `manufacturer`). It is the
  generalization of the former manufacturer-only `component_make`.
- A **`driver`** (Generic SNMP, Cisco xAPI, ...) names the implementation that gets, emits, or sets
  a product's signals, carrying an optional **`version`**.
- A **`capability`** (Microphone, Display, ...) names what a component can do.

See the [Vendors guide](/guides/admin/vendors/) for the operator surface.

### Catalog reference data: `product`

A **`product`** (Cisco Room Bar, Samsung QM55, ...) is the concrete **SKU** that ties the three leaf
catalogs together: a stable `id` and `display_name`, a **`kind`** (`device` / `app` / `service` / `vm`,
default `device`, a fixed enum checked in the DB and at the API edge), an optional **`vendor_id`** (who
makes it) and **`driver_id`** (what talks to it), an optional **`parent_product_id`** (a self-reference:
a variant points at its base product), and the `official` boolean. The capabilities a product provides
are a many-to-many set carried in the **`product_capability`** join (`product_id`, `capability_id`); a
video bar provides microphone, speaker, camera, and codec. The vendor, driver, and parent FKs are
**`on delete set null`** (deleting a vendor nulls the pointer, it does not block).

A **`component`** points at the product it **is** through **`component.product_id`**
(**`on delete restrict`**): the product is the source of a component's shape (its vendor, driver, and
capabilities), replacing the old `component_type`-as-shape notion. The restrict FK is the referential
guard the leaf catalogs deferred, so a product still referenced by a component cannot be deleted (409).
See the [Products guide](/guides/admin/products/) for the operator surface.

### Declared properties: the product contract and the value store

A product does not only classify, it **declares what its instances expose**. That declaration is
**`product_property`**, the product's **contract**: one row per property the product declares
(`product_id`, `property_name` referencing the [`property` catalog](/architecture/variables/), an
optional `default_value` in jsonb, and a `required` flag), unique per `(product, property)`. Type and
validation are deliberately **not** repeated here: they live on the property, which stays the single
source for what a name means.

The value lives in **`property_value`**, which carries the **same owner exclusive-arc** as the datapoint
sinks and `event` (`owner_kind` plus `component_id` / `system_id` / `location_id` / `node_id`, one-set
CHECK), plus the property name, an `instance` discriminator, a **`provenance`**, and the jsonb `value`.
Provenance is what makes the fold work: the same table holds a value an operator **declared**, a value a
device **observed**, a value a rule **calculated**, and a value config **intended**, and only the
provenance says which. Today the write path fills `owner_kind=component` with `provenance=declared`; the
rest of the arc and the other three provenances are seats for the producers that come later.

The read is **`EffectiveProperties`**, one query with two arms:

- the **contract arm**: every property the component's product declares, resolved to
  `coalesce(the component's declared value, the contract default)`, marked `from_contract`;
- the **off-contract arm**: the values set directly on the component for properties its contract does
  not declare.

A **productless** component has only the off-contract arm, so it still resolves. This pair replaces the
retired `field_definition` / `field_value` feature: a "field" was always a property with **declared**
provenance, and the catalog it hung off is now the product's contract
([ADR-0047](/architecture/decisions/#adr-0047-the-fields-fold-product_property-and-property_value)). See
the [Properties guide](/guides/admin/properties/) for the operator surface.

## The variable-depth trees

`component`, `system`, and `location` are each a **variable-depth tree**: a `parent_id` self-reference
that nests to arbitrary depth (campus -> building -> floor -> room; parent system -> subsystem; chassis
-> card). The trees are the structural backbone of the [cascade](/architecture/cascade/): resolution
runs over an entity's containment path and the **deepest node wins**, weight-free, pure depth.

```d2
direction: right
classes: { node: { style.border-radius: 8 }; key: { style: { border-radius: 8; bold: true } } }
component: component { class: node }
product: product { class: node }
location: location { class: node }
system: system { class: node }
component -> product: is a (N:1)
component -> component: parent (tree)
system -> location: located at (N:1)
system -> system: parent (tree)
location -> location: parent (tree)
```

A non-leaf node in a tree (a chassis, a floor, a parent system) contributes its **instance**
bindings down the cascade, not its template: a chassis hands a card its chassis-wide credential while
the card keeps its own template ([cascade](/architecture/cascade/)).

### Sub-components and sub-systems

The `parent_id` self-reference is **same-kind nesting**: a **system may have a parent system**
(sub-system nesting) and a **component a parent component** (sub-component nesting), both over the same
variable-depth `parent_id` trees. A chassis with line cards is a parent component over child
components; a building-wide AV system composed of room subsystems is a parent system over child
systems.

This nesting feeds two mechanisms. It feeds the **cascade**, where the **deeper node wins** down the
component and system trees (a sub-system's bindings override its parent's, a sub-component's override
the chassis'). And it feeds the **health rollup**: a **sub-system's health rolls into its parent
system**, and a **sub-component's into its parent component**, the same role-aware composition that
runs up the rest of the tree.

The **practical starting depth is 3 levels** (parent / child / grandchild) for both trees, a guidance
default, **not a hard cap**: the `parent_id` trees support arbitrary depth, and we revisit the
guidance if a use case needs more. The depth-resolution and rollup semantics themselves live in
[cascade](/architecture/cascade/) and [health](/architecture/health/).

## Ownership: the exclusive-arc

Everything observed, asserted, or set in Omniglass attaches to exactly one structural entity, through
the **exclusive-arc**. Every datapoint table, plus `event`, `property_value`, `alarm`, and `variable`,
carries:

- an **`owner_kind`** enum, plus
- the **matching typed FK** (`component_id` / `system_id` / `location_id` / `node_id`, or none for the
  singleton `global`), plus
- a **CHECK** that exactly the column matching `owner_kind` is set (or all null for `global`).

This makes **system-, location-, node-, and global-level datapoints first-class** (e.g. `health` is a
`state_datapoint` owned by a system, estate-wide availability is owned by `global`, and a node's
self-health is owned by the node), the fix for a monitoring tool that can only put state on a single
host. The same arc owns the `event` and `alarm` rows a datapoint produces, so a system-owned datapoint
yields a system-owned alarm. The full pattern and the storage DDL are on [storage](/architecture/storage/).

### The event sink: the first arc-owned occurrence

The arc's first built sink beyond the datapoint tables is **`event`**, the **log-kind sink** of the
collection pipeline. Where a `metric_datapoint` or `state_datapoint` records a **sampled present value**
(a reading that has a value *now*, `last()` is meaningful), an `event` records a **past occurrence**: a
device log line, something that *happened* and whose "what is it now?" is meaningless (the
[has-a-value-now razor](/architecture/datapoints/#the-has-a-value-now-razor-datapoint-vs-event)). A
collected datapoint whose property is **log**-kind (the seeded starter is `syslog.line`) is no longer
dropped at ingest: it routes to `event` as an occurrence, carrying a **`message`** (its text, from
`string_value`) and optional structured **`attributes`** (jsonb, from `json_value`).

An `event` row carries the **identical owner exclusive-arc** as a datapoint (`owner_kind` plus the
matching `component_id` / `system_id` / `location_id` / `node_id`, under the same CHECK that exactly one
is set) and the **same provenance** vocabulary (`observed` / `calculated` / `intended` / `declared`,
default `observed`), so a log occurrence is owned, addressed, and confined exactly like the value
datapoints beside it: the **same** owner-confinement and reject-not-project gates apply at ingest. This
is why the ownership pattern above already named `event` alongside the datapoint tables; it is the same
arc, now with a built sink on it.

Closing the loop, the reserved **`event_id`** columns on `metric_datapoint` and `state_datapoint` are
now **real foreign keys** to `event(id)` (`on delete set null`): an **intended**-provenance datapoint (a
value a command set) references the `event` that produced it, so a datapoint's lineage can point at the
occurrence behind it. See [datapoints](/architecture/datapoints/) for the value sinks and
[events](/architecture/events/) for the normalized-occurrence model layered above the raw log sink.

## Structural multi-membership (a component in N systems)

A shared device legitimately belongs to more than one system, which would make the system layer a DAG.
Keep it a **tree with a primary-system pointer** (which system chain feeds the cascade); a truly shared
device **skips the system layer**. The genuine "config differs per system" case is answered by
**per-system effective views** on demand, not by merging chains into the resolution
([cascade](/architecture/cascade/)).

The binding itself is the **`system_member`** table: the **instance assignment** that ties a
`component` to a `system` under a specific role, satisfying a `system_template_member` from the frozen
`system_template_version` (key columns: `system_id`, `component_id`, `role`, plus the pin to the
`system_template_member` it satisfies).

A `system_template_member` declares, per role, a **requirement** (the canonical datapoints and commands a
member must provide) plus its `health_role`; any component whose template meets the requirement can fill
the role, validated on assignment. Detailed on [templates](/architecture/templates/).

## Operational mode: active, maintenance, disabled

Every entity has an **operational mode**, a cascade-resolved state that says how much the platform backs
off, set on the entity or inherited from a parent ([cascade](/architecture/cascade/)):

- **active** (default): collect, evaluate, act, enforce drift, count toward SLA. Normal.
- **maintenance**: **keep collecting**, but **suppress the consequences** of what is seen. Planned work:
  watch, but do not act.
- **disabled**: **stop collecting and interacting** with the device entirely (the Zabbix host-disable).
  The entity stays in the model, dormant, until re-enabled.

Maintenance and disabled are the **same suppression**, differing on one knob, **collection**: maintenance
suppresses consequences but keeps watching (so an operator can verify the work); disabled also goes dark
(no polling, no commands). Both suppress the same four consequences:

- **action dispatch is held**: an alarm may still open (you see it), but no `action_rule` pages or opens a
  ticket ([alarms and actions](/architecture/alarms-actions/)).
- **drift is observed, not enforced**: the set function never fires, so a tech mid-swap is not fought
  ([config](/architecture/variables/)). The device-swap case (a brief declared-identity authority,
  [datapoints](/architecture/datapoints/)) is just maintenance suppressing drift.
- **health rolls up no impact**: a member in maintenance or disabled does not sink its parent's
  [health](/architecture/health/); it surfaces as "down (maintenance)", the truth plus the mode, not a
  fifth health value.
- **SLA does not count it**: the window is excluded from availability and the SLO.

Maintenance is **time-bound**: a window (start / end, [time](/architecture/time/)) that **auto-exits**, or
open-ended until cleared, with a recurring window expressed as a schedule. Disabled is held until
re-enabled. Entering or exiting either is an **audited** operator action ([audit](/architecture/audit/)),
so "no page at 2am, it was in the patch window" is always explainable. Because the mode is
cascade-resolved, maintenance on a system covers its components.

## Decommission and delete

"Delete" is **decommission** by default, a **soft delete**: the entity is tombstoned, drops out of
placement, worklists, and default views, but its **history is retained** (datapoints, events, alarms,
audit), attributed to the tombstone and aging out by [retention](/architecture/storage/). An observability
and control plane must not let "remove this projector" erase the incident record, and a decommissioned
entity can be **re-commissioned** if the device returns. **Purge** is the privileged, audited hard erase
for a genuine mistake (a test component); for a high-volume entity it runs as a background job over the
partitions, while the cheap path is decommission plus letting retention age the firehose out.

Decommissioning runs the **in-flight cleanup**, reusing mechanisms that already exist: collection stops
(the worklist drops it, sessions close), **open alarms auto-resolve** (reason "decommissioned"),
**pending commands and running flows cancel** (the durable command queue, and flows are already gated on
their alarm staying open), and config / tag / credential / group bindings drop. The entity leaves its
parent's health rollup.

The cascade is **not** "delete everything below", because containers do not own their members:

- a **component** (leaf) decommissions as above;
- a **system** delete **unbinds its members** (the `system_member` rows) but **does not delete the
  components**; they become standalone, re-homeable;
- a **location** delete is **refused by the API while occupied** (it returns what is placed there); the
  console offers re-homing before the delete (move the systems and components, then delete the empty
  location);
- a **node** delete **re-places its tasks** (to the server or another node, or surfaces the components as
  uncollected) and revokes the node credential ([identity and access](/architecture/identity-access/));
  `node.*` history is retained.
