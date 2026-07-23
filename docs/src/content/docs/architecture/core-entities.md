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
trees with full scoped CRUD; a delete is refused while a structural child remains. Each of the three
carries an **optional classifier** that declares what its instances expose: a component points at its
**`product`**, a system conforms to a **`standard`**, a location is typed by its **`location_type`**
(the `component_type` registry retired,
[ADR-0047](/architecture/decisions/#adr-0047-the-fields-fold-product_property-and-property_value); the
`system_type` registry was promoted to `standard`,
[ADR-0048](/architecture/decisions/#adr-0048-the-standard-blueprint-and-the-template-fork-seed-model)). The
**exclusive-arc** owner columns are now real, carrying the datapoint sinks, the **`event`** log sink
(see [The event sink](#the-event-sink-the-first-arc-owned-occurrence) below), and **`property_value`**
(see [Declared properties](#declared-properties-the-classifier-contracts-and-the-value-store) below); a
component, system, and location each own a recorded **[health](/architecture/health/)** series on that
same arc. A system also declares the **roles** it needs filled, staffed by components whose **resolved
capabilities** cover what the role requires, each role carrying the **impact** an impaired one has on the
system (see [System roles](#system-roles-the-slots-a-system-needs-filled) below). Still `Design`: template
pinning, `system_member` composition, operational mode, decommission / purge, and the **`alarm` arc** (an
alarm is component-local today, so a system- or location-owned alarm is not yet a thing).
See [implementation status](/architecture/status/).
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
  is located at a location, and **conforms to a `standard`**, the blueprint it is built against. The
  pointer is optional, mirroring `component.product_id`: a one-off system conforms to no standard and
  simply carries no contract.
- A **location** ties systems and components to a physical place (campus, building, floor, room).
  It is classified by `location_type` and, unlike component and system, has **no template**: for a
  location the type is the only shape-definer. A starter `location_type` set ships seeded (and is
  operator-owned, see [the seed model](#the-seed-model-forked-templates-versus-canonical-catalogs)),
  readable at `GET /location-types` (alphabetically by display name), which is what the type picker on the location form
  lists so a location is classified by a known type rather than a free-typed string. Each type also
  carries an `icon` (a glyph key like `building` or `landmark`) that the console renders as the
  leading glyph on every location of that type, so a campus reads differently from a building at a
  glance in the tree; an unknown key falls back to `map-pin`. Each type also carries
  `allowed_parent_types` (a set of `location_type` ids and/or the reserved `root` sentinel), the
  placement constraint: empty means unconstrained, a non-empty set is enforced on create and move,
  and the four shipped types carry it seeded (`campus={root}`, `building={root,campus}`,
  `floor={building,campus}`, `room={floor,building,campus}`).
- A **node** is the edge process (`omniglass --mode node`) that pulls work, reaches components over
  interfaces, and ships results ([nodes](/architecture/nodes/)). It is structural because it is a
  first-class **owner**: a node owns its own self-health telemetry and can carry a node-owned alarm.

A component belongs to one or more systems (see
[membership](#membership-what-a-role-attaches-to)); a system sits in a location.

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

Above the four sits the singleton **`global`** estate owner: the top owner above every location where
estate-wide health and KPIs roll up. One per deployment, no FK. It is an **owner**, nothing else: it is
not a location (the location tree is a forest of N unparented tops with no root) and it is not the
[cascade](/architecture/cascade/)'s least-specific binding tier, which is **`platform`**
([ADR-0057](/architecture/decisions/#adr-0057-the-cascades-least-specific-tier-is-platform-and-a-default-is-not-a-tier)).

| Entity | What it is | Key columns |
|---|---|---|
| `component` | a deployed instance (`dsp-boardroom-3`) | name (unique), **parent_id** (self-ref tree), display_name; pins a `component_template_version`; carries **`product_id`** (optional, `on delete restrict`), the source of its shape |
| `system` | a composition of components / subsystems (the service tree) | name (unique), **parent_id** (self-ref tree), display_name; pins a `system_template_version`; carries `location_id`; carries **`standard_id`** (optional), the blueprint it conforms to |
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
- A **`capability`** (Microphone, Display, ...) names what a component can do. It is claimed in two
  places, a **product** (the default for its instances) and a **component** (its own facts, which add to
  or suppress the product's), and it is what a
  [system role](#system-roles-the-slots-a-system-needs-filled) requires.

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

### Catalog reference data: `standard`

A **`standard`** (Huddle Room, Classroom, Auditorium, ...) is the **blueprint a system conforms to**: the
system-side counterpart of `product`. It carries a stable `id` and `display_name`, an optional
**`parent_standard_id`** (a self-reference: a variant points at its base standard, exactly as
`parent_product_id` does), and the `official` boolean. It is the promotion of the former `system_type`
label registry, which had nothing to declare
([ADR-0048](/architecture/decisions/#adr-0048-the-standard-blueprint-and-the-template-fork-seed-model)).

A **`system`** conforms to a standard through **`system.standard_id`**, and the pointer is **optional**,
mirroring `component.product_id`: a **one-off system** that matches no blueprint is first-class and simply
carries no contract. Conformance is **not** a copy: the standard's contract defaults resolve **live** for
every system that conforms, until that system overrides one. See the
[Standards guide](/guides/admin/standards/) for the operator surface.

### Declared properties: the classifier contracts and the value store

A classifier does not only classify, it **declares what its instances expose**. Three tables carry that
declaration, one per classifier, all the same shape: **`product_property`** (a component's), **`standard_property`**
(a system's), and **`location_type_property`** (a location's). Each is one row per declared property
(`<classifier>_id`, `property_name` referencing the [`property` catalog](/architecture/variables/), an
optional `default_value` in jsonb, and a `required` flag), unique per `(classifier, property)`. Type and
validation are deliberately **not** repeated here: they live on the property, which stays the single
source for what a name means.

The value lives in **`property_value`**, which carries the **same owner exclusive-arc** as the datapoint
sinks and `event` (`owner_kind` plus `component_id` / `system_id` / `location_id` / `node_id`, one-set
CHECK), plus the property name, an `instance` discriminator, a **`provenance`**, and the jsonb `value`.
Provenance is what makes the fold work: the same table holds a value an operator **declared**, a value a
device **observed**, a value a rule **calculated**, and a value config **intended**, and only the
provenance says which. Today the write path fills `provenance=declared`; the other three provenances are
seats for the producers that come later.

The read is **`EffectiveProperties(ownerKind, ownerID)`**, one query with two arms:

- the **contract arm**: every property the instance's classifier declares, resolved to
  `coalesce(the instance's declared value, the contract default)`, marked `from_contract`;
- the **off-contract arm**: the values set directly on the instance for properties its contract does
  not declare.

**One resolver serves all four owner kinds.** The three classifier/instance pairs differ only in *where*
the instance names its classifier and *which* table that classifier declares into, so those five
identifiers (instance table, classifier column, contract table, contract key, arc column) are data in an
`ownerContract` table and the SQL is written **once**:

| Owner kind | Classifier column | Contract table |
|---|---|---|
| `component` | `component.product_id` | `product_property` |
| `system` | `system.standard_id` | `standard_property` |
| `location` | `location.location_type` | `location_type_property` |
| `node` | (none) | (none) |

An instance whose classifier is unset (a **productless** component, a **one-off** system) resolves to its
off-contract arm alone, and a **node** has no classifier at all, so it always does. Writing the resolution
three times would let the three drift; writing it once is the primitive-first move
([ADR-0048](/architecture/decisions/#adr-0048-the-standard-blueprint-and-the-template-fork-seed-model)).
Every arc is **scope-checked on the write**, so setting a value on a system or location outside your scope
is a non-disclosing 404, not a silent success.

This pair replaces the retired `field_definition` / `field_value` feature: a "field" was always a property
with **declared** provenance, and the catalog it hung off is now the classifier's contract
([ADR-0047](/architecture/decisions/#adr-0047-the-fields-fold-product_property-and-property_value)). See
the [Properties guide](/guides/admin/properties/) for the operator surface.

### System roles: the slots a system needs filled

A contract says what a system **carries**. A **system role** says what it **needs filled**: a table
microphone, a main display, a confidence monitor. A role is a **slot**, not a component, so the system can
state its shape before anything is assigned to it, and an **unfilled slot is visible** rather than absent.

:::note[Not the IAM role]
A **system role** is a slot in a system. An **[IAM role](/architecture/identity-access/)**
(viewer / operator / admin / owner) is a capability set granted to a principal. They share only the word;
nothing crosses between them. The storage methods are deliberately `ListSystemRoles` / `SetSystemRole` /
`DeleteSystemRole` so they cannot be mistaken for RBAC's `ListRoles` / `UpsertRole`.
:::

Five tables carry it:

| Table | Key columns | Notes |
|---|---|---|
| `system_member` | (`system_id`, `component_id`, **`is_primary`**) | **membership**: the binding a role attaches to, many-valued, cascading from both ends |
| `system_role` | `owner_kind` + `standard_id` / `system_id` (the arc), `name`, `display_name`, **`quorum`**, **`impact`** | the slot itself; the arc is the one `property_value` uses, with a one-set CHECK and a `unique nulls not distinct` key over the arc plus name |
| `system_role_capability` | (`role_id`, `capability_id`) | what the role requires, **conjunctive**: a component must provide **every** listed capability |
| `component_capability` | (`component_id`, `capability_id`, **`present`**) | the component's **own** capability facts, layered over its product's |
| `system_role_assignment` | (`system_id`, `role_id`, `component_id`) | who fills the role here; the component FK is **`on delete restrict`** |

### Membership: what a role attaches to

**A component that does a job in a system is a member of it, and the role is what that membership does.**
Belonging and job are one attachment seen at two levels, not two facts that can drift apart.

Membership is **many-valued on purpose**. A shared device belongs to every system it serves: a rack DSP
feeding three rooms is a member of all three, and each of them depends on it. That a component is in this
system *and two others* is exactly the kind of thing an operator needs to know before touching it.

**Staffing a role creates the membership.** A component filling a job in a system that the system does not
count as a member is a contradiction, so assignment binds it rather than asking an operator to say it twice.
The reverse is deliberately **not** symmetric: giving up a role does not end membership, because the device
is still in the room. A member carrying no role at all is ordinary and useful, and it is how the power
conditioner, the rack shelf, and the spare on the shelf are accounted for.

**`is_primary`** marks which membership answers a question asked *without a system in hand*. It is a
**default for context-free callers, not a resolution rule**: anything that names a system resolves against
that system. A component's first membership takes the default with nobody asking, so a component in exactly
one system, which is nearly all of them, never meets the concept. A partial unique index makes a second
primary impossible rather than merely unlikely.

Membership **cascades from both ends**, since a binding is meaningless once either side is gone. It
deliberately does not restrict the component the way `system_role_assignment` does: that restrict is load-bearing,
because deleting a component that fills a job would silently break a system's health, but a membership
carrying no role is an inventory fact, and refusing the delete for it would add a step to every component
removal while protecting nothing the role table does not already protect.

**A role is declared on a standard or on a system**, the same exclusive arc the property value store uses.
Declared on a **standard**, every conforming system **inherits it live**, exactly as a contract default does:
add a role to Meeting Room and every meeting room is short one component until it is staffed. Declared on a
**system**, it is ad-hoc, which is both how a **one-off system** gets roles at all and how a conforming system
adds what its standard does not cover. A role also carries a **`quorum`**, how many components should fill it
(a room wanting two ceiling mics has one role with quorum 2, not two roles), at least one, because a role no
component need fill is not a role.

**A capability is a fact about a component, not only about its product.** `component_capability` layers over
the product's set: `present=true` adds a capability the product does not claim (a unit with a mic pod nobody
modeled), `present=false` suppresses one it does (a bar whose camera is dead or removed). The resolver
**`EffectiveCapabilities(component)`** is then:

> the **product's** capabilities, UNION the component's **`present=true`** rows, MINUS its **`present=false`**
> rows.

A **productless** component resolves to just its own declarations. This is the single definition of "what this
component can do" for the whole platform, and it is the same **contract-plus-override** shape the declared
properties above already use, applied to capabilities instead of values.

The read side is **`EffectiveRoles(system)`**: the roles its standard declares (marked `from_standard`) UNION
those declared on it directly, each with its required capabilities, its quorum, and the components filling it
**here**. It serves **assigned** and **understaffed** (quorum minus assignments, floored at zero) rather than
leaving each surface to do the arithmetic, so an under-staffed room reads the same way in the console, the
CLI, and the API.

**Assignment is refused when the component cannot fill the role**, with the missing capabilities **named**:

```
component "panel-1" cannot fill role "table-mic": missing microphone, speaker
```

This is a refusal on **modeled** grounds, the same class as the location placement constraint, and naming the
gap is the point. A bare refusal leaves an operator guessing; a named one tells them to either fix the
component's capabilities or pick a different component. It is also why capabilities had to become a
**resolved** set at all: `product` is optional, so a strict guard over a product-only fact would have locked
every productless component out of every role
([ADR-0049](/architecture/decisions/#adr-0049-the-system-role-capability-gated-staffing-and-the-resolved-capability-set)).

**A role also declares its `impact`**: `outage`, `degraded`, or `none`, what an **impaired** role (fewer
satisfying components than its quorum) means for its system. It lives on the role rather than on the
component or the alarm because the same broken box matters differently depending on the slot it was filling:
a dead confidence monitor is not a dead main display. Impact is the one input the
**[health](/architecture/health/)** rollup takes from the declaration side, and **quorum is the redundancy
knob** underneath it: a role wanting one component with two assigned tolerates a failure, a role wanting two
with two assigned does not.

Staffing stays readable **without** any of that: a role wanting two components with one assigned is
under-staffed **today**, on data the operator entered, with nothing collecting. Health adds the second
question (of the components that **are** assigned, how many can currently do the job) and routes the answer
up the tree
([ADR-0050](/architecture/decisions/#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain)).

### The seed model: forked templates versus canonical catalogs

Not everything the binary ships with is the same **kind** of thing, and conflating the two kinds is what
makes seeded defaults painful. Omniglass splits them:

- **Example content** (a `standard`, a `location_type`) is created by **forking an in-code template**. The
  fork is **one-time, with no inheritance**: nothing in an estate points back at the template, so the
  template can be improved in any release without touching a single tenant. What lands is an ordinary
  **operator-owned** row (`official: false`), freely editable and deletable, and the boot seed installs it
  **only if absent** (`ON CONFLICT DO NOTHING`). Re-seeding never reasserts over an edit; if it did, an
  operator's rename of "Huddle Room" would silently revert on the next restart. The **roles** a shipped
  standard declares ride the same lane, so retuning a seeded role's quorum survives the next boot.
- **Canonical vocabulary** (the [`property` catalog](/architecture/variables/), and later `command` and
  `event_type`) is the shared namespace a driver maps onto, so it must stay identical install to install.
  It seeds as `official: true` through an **authoritative upsert** (`ON CONFLICT DO UPDATE`), read-only in
  the API, so a release can correct it. The classification catalogs (`vendor`, `driver`, `capability`,
  `product`) sit on that same authoritative path today.

The distinction is worth internalizing: **forking applies to template -> row, never to classifier ->
instance**. A system does not fork its standard; it **conforms** to it and inherits **live**, so revising a
standard's default moves every conforming system that has not overridden it.

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
standard: standard { class: node }
component -> product: is a (N:1)
component -> component: parent (tree)
system -> standard: conforms to (N:1)
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
`state` owned by a system, estate-wide availability is owned by `global`, and a node's
self-health is owned by the node), the fix for a monitoring tool that can only put state on a single
host. It is also what carries a recorded [health](/architecture/health/) verdict: a component, a system,
and a location each own their own transition series under the same `health` key, and the **owner** is what
gives a reading its level. The same arc owns the `event` rows a datapoint produces, and is the design for
`alarm` (component-local today), so a system-owned datapoint will yield a system-owned alarm. The full
pattern and the storage DDL are on [storage](/architecture/storage/).

### The event sink: the first arc-owned occurrence

The arc's first built sink beyond the datapoint tables is **`event`**, the **log-kind sink** of the
collection pipeline. Where a `metric` or `state` records a **sampled present value**
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

Closing the loop, the reserved **`event_id`** columns on `metric` and `state` are
now **real foreign keys** to `event(id)` (`on delete set null`): an **intended**-provenance datapoint (a
value a command set) references the `event` that produced it, so a datapoint's lineage can point at the
occurrence behind it. See [datapoints](/architecture/datapoints/) for the value sinks and
[events](/architecture/events/) for the normalized-occurrence model layered above the raw log sink.

## Structural multi-membership (a component in N systems)

A shared device legitimately belongs to more than one system, and
[`system_member`](#membership-what-a-role-attaches-to) says so directly: it is many-valued, and a rack DSP
serving three rooms holds three memberships.

What stays single-valued is **which system chain feeds the cascade**, because the resolver ranks candidates
by band and then depth with no tiebreaker after that, so a many-valued seed would resolve
nondeterministically. That is the job `is_primary` does, and only for callers with no system in hand. The
genuine "config differs per system" case is answered by **per-system effective views** on demand, not by
merging chains into the resolution ([cascade](/architecture/cascade/)).

:::note[Superseded]
This page previously said a truly shared device **skips the system layer**, which was the best available
answer while the only binding was a single pointer on the component. It no longer holds: a shared device is
a member of every system it serves.
:::

The binding itself is the **`system_member`** table, which is **built**: `(system_id, component_id,
is_primary)`, described under [membership](#membership-what-a-role-attaches-to). The design below layered
the role onto that same row and pinned it to a frozen template BOM; what shipped keeps the row to the
binding alone and lets `system_role_assignment` carry the role, so a member can exist without one.

A `system_template_member` declares, per role, a **requirement** (the canonical datapoints and commands a
member must provide) plus its `health_role`; any component whose template meets the requirement can fill
the role, validated on assignment. Detailed on [templates](/architecture/templates/).

:::note[What is built is the role model above]
`system_member` is **built** as the plain binding above; the **frozen template BOM** on it is still
`Design`. The **built** slot is
[`system_role`](#system-roles-the-slots-a-system-needs-filled), declared on a standard or a system rather
than frozen into a `system_template_version`, requiring a **capability** set rather than canonical
datapoints and commands, and assigned through `system_role_assignment`. Its **`impact`** also replaces the
`health_role` tag above: `required` / `redundant` / `informational` are expressed by quorum plus impact,
with no fourth vocabulary
([ADR-0050](/architecture/decisions/#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain)).
The two models are reconciled when template pinning is built
([ADR-0049](/architecture/decisions/#adr-0049-the-system-role-capability-gated-staffing-and-the-resolved-capability-set)).
:::

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
  [health](/architecture/health/); it surfaces as "outage (maintenance)", the truth plus the mode, not a
  fourth verdict value.
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
