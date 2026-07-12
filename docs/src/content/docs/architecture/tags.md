---
title: Tags
description: A governed key vocabulary and per-entity value bindings that resolve union-on-key, override-on-value down the cascade.
sidebar:
  badge:
    text: Partial
    variant: note
---

:::note[Partial: the registry, the bindings, and the cascade are built; the console surface and the cascade extensions are deferred]
The first slice ([ADR-0021](/architecture/decisions/#adr-0021-tag-slice-1-a-governed-key-registry-with-entity-update-gated-bindings)) is
**built**: the governed **`tag`** key registry, the per-entity **`tag_binding`** cell on the exclusive arc, and the
union-on-key / override-on-value **cascade** resolver, over the API and the generated CLI. Deferred to later slices:
the operator console surface (a Tags directory and a per-entity tag editor), binding through
[groups](/architecture/groups/) and a `template`-scoped default (the shared-resolver work in
[#184](https://github.com/hyperscaleav/omniglass/issues/184)), value-domain governance (an open question below), and
binding onto a [file](/architecture/files/). Those divergences are logged in the
[decision log](/architecture/decisions/).
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
  (`global | location | system | component`), exactly the arc a [variable](/architecture/variables/) or a secret is
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
  tag it, using the keys the vocabulary already governs. A global binding (a tenant-wide default value) has no owning
  entity to defer to, so it is gated by `tag:update`.

So the vocabulary stays curated while tagging stays routine. Reading the vocabulary and an entity's tags rides the
viewer floor (`tag:read`, `component:read`).

## A key governs where it applies and whether it cascades

Two fields on the key shape how its bindings behave:

- **`applies_to`** narrows a key to a subset of entity kinds (`component`, `system`, `location`). An empty set is
  **universal** (the key applies everywhere); a non-empty set rejects a binding on any other kind at write time (a
  `rack_position` key that `applies_to: [location]` cannot be bound onto a component). The vocabulary carries its own
  scoping rules, so a key means the same thing wherever it is legal.
- **`propagates`** says whether a bound value cascades. A **propagating** key (the default) resolves down the tree:
  tag a location once and every system and component beneath it inherits the value. A **non-propagating** key binds as
  a **flat per-entity set**: it resolves only from a binding on the entity itself, never from an ancestor. That flat
  case is the shape a [file](/architecture/files/) needs (a file is not on the structural arc, so it has no parent to
  cascade from); the same key model serves both, toggled by this one field.

## Values resolve union-on-key, override-on-value

The effective tags for a component resolve down the structural cascade (`global -> location tree -> system tree ->
component tree`), the same walk the variable and secret resolvers use, but with a different combinator:

- **Keys union.** An entity surfaces **every** key bound at or above it. Two different keys set at two different scopes
  both appear; they do not compete.
- **Values override.** For a **single** key set at several scopes, the **most-specific** binding wins (highest band,
  then nearest depth), exactly like a variable. The shadowed candidates come back too, so the surface can teach the
  override.

A non-propagating key is admitted into the resolve only from the target entity itself, so its ancestor bindings never
leak downward. The `GET /components/{name}/effective-tags` route returns the resolved set (winner plus shadowed
candidates); `GET /components/{name}/tags` returns only the bindings set **directly** on the component.

:::caution[Open question]
Value-domain governance. Key normalization is settled (the governed registry plus the `tag:create` gate and the
lowercase-identifier rule). The open part is the **value** side: whether a key may **constrain** its values (an enum or
`value_type`, so `environment` accepts only its allowed set, validated and autocompleted like a
[`datapoint_type`](/architecture/datapoints/) domain), and whether it may **normalize** them on input (lowercase,
trim, fold synonyms) so `Prod`, `prod `, and `PROD` resolve to one value. Slice 1 ships free-text values; how much
governance a key places on its values is deferred.
:::

## Storage

The key vocabulary and the value cell; the physical layout (the owner arc, the cascade key) lives on
[storage](/architecture/storage/).

| Table | Key columns | Notes |
|---|---|---|
| `tag` | name, applies_to, propagates | **Built.** The tenant-wide governed key vocabulary; minting a key needs `tag:create`. `applies_to` narrows a key to entity kinds (empty = universal); `propagates` toggles cascade versus flat per-entity binding |
| `tag_binding` | (tag_id, **owner arc**), value | **Built.** The `key: value` binding at one owner on the exclusive arc (`global / location / system / component`); resolves **union on key, override on value** down the [cascade](/architecture/cascade/). Setting a value is the owner's own `update` write |
