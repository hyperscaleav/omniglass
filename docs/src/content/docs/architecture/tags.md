---
title: Tags
description: A governed key vocabulary and per-entity value bindings that resolve union-on-key, override-on-value down the cascade.
sidebar:
  badge:
    text: Partial
    variant: note
---

:::note[Partial]
The first slice ([ADR-0021](/architecture/decisions/#adr-0021-tag-slice-1-a-governed-key-registry-with-entity-update-gated-bindings)) is
**built**: the governed **`tag`** key registry, the per-entity **`tag_binding`** cell on the exclusive arc, and the
union-on-key / override-on-value **cascade** resolver, over the API and the generated CLI. On top of it, the **console
Tags directory** (mint, edit governance fields, delete a key), the per-entity **binding editor** (apply and remove
values from an entity's detail blade), **value-domain enums** that constrain a key's values ([#190](https://github.com/hyperscaleav/omniglass/issues/190)),
and the directory **Tags column** with per-key **tag filtering** (narrow a directory by any tag's effective value,
plus **is set** / **is absent**) are built too ([#189](https://github.com/hyperscaleav/omniglass/issues/189),
[#226](https://github.com/hyperscaleav/omniglass/issues/226)). Deferred to later slices: the winner-plus-shadowed
**cascade provenance** panel on an entity's detail (the rest of [#189](https://github.com/hyperscaleav/omniglass/issues/189)),
binding through [groups](/architecture/groups/) and a `template`-scoped binding (the shared-resolver work in
[#184](https://github.com/hyperscaleav/omniglass/issues/184)), binding onto a [file](/architecture/files/)
([#191](https://github.com/hyperscaleav/omniglass/issues/191)), and a stored per-key color override. Those divergences
are logged in the [decision log](/architecture/decisions/).
:::

A **tag** is an operator **`key: value`** label attached to an entity to organize, filter, and scope by dimensions
Omniglass does not model natively (`category: audio-dsp`, `environment: prod`, `cost_center: 4021`). A tag is not a
signal and carries no lifecycle; it rides the same [cascade](/architecture/cascade/) as
[config, secrets, and variables](/architecture/variables/), but with a **union-on-key, override-on-value** combinator
rather than a single most-specific value.

## Two layers: the key vocabulary and the value binding

- **`tag`** is the **governed key vocabulary**: one row per key (`category`, `environment`), shared across the whole
  tenant (one registry per database, which is the tenant boundary). It owns no value.
- **`tag_binding`** is the **value cell**: it sets a value for a key at one owner on the exclusive arc
  (`platform | location | system | component | node`), exactly the arc a [variable](/architecture/variables/) or a secret is
  owned at.

Splitting them is what keeps the vocabulary **normalized**: the key `environment` is minted once and reused, so no one
invents `env` beside `environment` beside `Environment`. The key name is validated as a lowercase identifier in a pure
`internal/tag` package, so the canonical spelling is enforced on the way in, not by convention.

## The governance split is the point

Minting a key and setting a value are **two different permissions**, and that split is deliberate:

- **Minting a key is a tenant-wide governance action**, gated by an all-scope **`tag:create`** grant (an admin or
  curator, [identity and access](/architecture/identity-access/)). Editing a key's governance fields is `tag:update`
  and deleting it is `tag:delete`, both all-scope.
- **Setting a value is the ordinary entity write.** Binding `environment: prod` onto a component is a
  **`component:update`**, onto a system a `system:update`, onto a location a `location:update`, the same write an
  operator already holds on the entity. Binding needs **no new permission**: an operator who may edit a component may
  tag it, using the keys the vocabulary already governs. A **platform** binding (the install-wide value for a key) has no
  owning entity to defer to, so it is gated by `tag:update` plus the install-wide
  [`platform:update`](/architecture/identity-access/#install-wide-authority-is-not-estate-scope).

So the vocabulary stays curated while tagging stays routine. Reading the vocabulary and an entity's tags rides the
viewer floor (`tag:read`, `component:read`).

## A key governs where it applies and whether it cascades

Two fields on the key shape how its bindings behave:

- **`applies_to`** narrows a key to a subset of entity kinds (`component`, `system`, `location`, `node`). An empty set is
  **universal** (the key applies everywhere); a non-empty set rejects a binding on any other kind at write time (a
  `rack_position` key that `applies_to: [location]` cannot be bound onto a component). The vocabulary carries its own
  scoping rules, so a key means the same thing wherever it is legal.
- **`propagates`** says whether a bound value cascades. A **propagating** key (the default) resolves down the tree:
  tag a location once and every system and component beneath it inherits the value. A **non-propagating** key binds as
  a **flat per-entity set**: it resolves only from a binding on the entity itself, never from an ancestor. That flat
  case is the shape a [file](/architecture/files/) needs (a file is not on the structural arc, so it has no parent to
  cascade from); the same key model serves both, toggled by this one field.

## Values resolve union-on-key, override-on-value

The effective tags for a component resolve down the structural cascade (`platform -> location tree -> system tree ->
component tree`), the same walk the variable and secret resolvers use, but with a different combinator:

- **Keys union.** An entity surfaces **every** key bound at or above it. Two different keys set at two different scopes
  both appear; they do not compete.
- **Values override.** For a **single** key set at several scopes, the **most-specific** binding wins (highest band,
  then nearest depth), exactly like a variable. The shadowed candidates come back too, so the surface can teach the
  override.

A non-propagating key is admitted into the resolve only from the target entity itself, so its ancestor bindings never
leak downward. The `GET /components/{name}/effective-tags` route returns the resolved set (winner plus shadowed
candidates); `GET /components/{name}/tags` returns only the bindings set **directly** on the component.

**Systems and locations resolve too.** A component walks the full arc, but every entity has an effective set. A
**location** resolves `platform` plus its own location tree. A **system** resolves `platform`, its own system tree, and
**the location it is placed at** (its `location_id` tree): a system in a PCI building surfaces `compliance: pci`, the
same way a component picks up its own location's tags. A **node** is estate-wide, not a scope tree, so it resolves `platform` plus its own direct bindings only (no inheritance). This is the read behind the directory **Tags column**: the list
routes (`GET /components`, `/systems`, `/locations`, `/nodes`) each carry an **`effective_tags`** map (key to winning value,
winners only) per row, resolved for the whole page in **one batched query** (a `Gateway.EffectiveTags` per kind, no
per-row fetch). Provenance (which scope a value came from) stays in the per-entity effective-tags detail view, not the
column.

**A key may constrain its values to an enum.** A key carries an **`allowed_values`** set: empty (the default)
leaves it free text, and a non-empty set is the enum a bound value must belong to, so `environment` can be
declared as one of `prod`, `staging`, `dev`. The binding write enforces membership (a value outside a key's
allowed set is a 422), and the add control renders a strict dropdown for an enum key. A **free** key instead
autocompletes the **distinct values already in use** for it (a `GET /tags/{name}:values` read), so an operator
reaches for `prod` instead of retyping it, without the key having to declare the set up front
([ADR-0024](/architecture/decisions/#adr-0024-a-tag-key-may-constrain-its-values-to-an-enum)).

:::caution[Open question]
The rest of value-domain governance. Beyond the string enum, whether a key may carry a typed **`value_type`**
(int, bool, date, validated like a [`datapoint_type`](/architecture/datapoints/) domain) and whether it may
**normalize** values on input (lowercase, trim, fold synonyms, so `Prod`, `prod `, and `PROD` resolve to one
value) stay open. The enum is the first, most-asked-for slice; the rest is deferred.
:::

## Storage

The key vocabulary and the value cell; the physical layout (the owner arc, the cascade key) lives on
[storage](/architecture/storage/).

| Table | Key columns | Notes |
|---|---|---|
| `tag` | name, applies_to, propagates, allowed_values | **Built.** The tenant-wide governed key vocabulary; minting a key needs `tag:create`. `applies_to` narrows a key to entity kinds (empty = universal); `propagates` toggles cascade versus flat per-entity binding; `allowed_values` is the value enum (empty = free text), enforced on the binding write |
| `tag_binding` | (tag_id, **owner arc**), value | **Built.** The `key: value` binding at one owner on the exclusive arc (`platform / location / system / component / node`); resolves **union on key, override on value** down the [cascade](/architecture/cascade/). Setting a value is the owner's own `update` write, except at `platform`, which takes `tag:update` plus `platform:update` |
