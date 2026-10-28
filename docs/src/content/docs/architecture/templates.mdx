---
title: Templates
description: "Example configurations an operator clones. Forking is one-time and leaves no link back, which is exactly why a template can be improved in any release without migrating anyone."
sidebar:
  badge:
    text: Design
    variant: caution
---

::::design[Target design, tracked in #827]

A template is an **example configuration an operator clones**. It ships inside the binary as a menu,
never as data: nothing is inserted until somebody picks one, and what they pick lands as ordinary rows
they own outright. **Nothing points back at the template**: forking is one-time, no inheritance, no
back-pointer, so a template can be rewritten in any release without migrating a single install.

:::caution[Design]
Nothing on this page is built. Interim: the shipped standards **seed as ordinary rows**
(`official: false`, inserted only if absent, so an operator's edit is never reverted on boot), and
the shipped location types are official rows an operator reshapes through the registry fork. The
template catalog, its read routes, and the fork and instantiate flows are tracked by
[#827](https://github.com/hyperscaleav/omniglass/issues/827).
:::

## Fork, not pin

This page used to describe the opposite model, **immutable versioned shapes that instances pin**,
retired by
[ADR-0071](/architecture/decisions/#adr-0071-a-template-is-a-clonable-example-not-a-versioned-shape-an-instance-pins).
The current model inverts it: a template is a starting point an instance copies and then forgets,
so **a template can change freely, because no instance is watching**. Same word, opposite purpose.

What replaced the pinned shapes is not a template at all:

| The shape of a... | is its... | and the relationship is |
|---|---|---|
| component | [`product`](/architecture/core-entities/), with its `product_property` contract, naming its [driver](/architecture/collection/#drivers-the-declarative-menu) | a pointer, `component.product_id` |
| system | [`standard`](/architecture/core-entities/) | **conformance**, with live inheritance |

**Forking applies template to row. Conformance applies row to instance.** Only the second is live:
a standard's contract default resolves for every conforming system until that system overrides it.
A template's link is severed at creation. How to reach and read a class of component is not a
template's job either: that is the driver's spec
([ADR-0827](/architecture/decisions/#adr-0827-the-template-layer-owns-the-clone-model-and-the-componenttemplate-authoring-model-is-superseded-by-the-driver-spec)).

## The four kinds

A template is one of four kinds, each naming what it creates:

- **A standard template**: a [standard](/architecture/core-entities/)'s attributes, its
  [system roles](/architecture/core-entities/#system-roles-the-slots-a-system-needs-filled) (accepted
  types, pinned products, quorum, capacity, choices), and its property and metric contract lines.
  Forking one scaffolds the contract and the roles in the same write, which answers what a template
  carries: everything the standard declares, so "start from a meeting room" yields the slots the
  space needs filled, not only a name.
- **A component template**: a product, a driver attach preset (every driver input except the address,
  secret inputs as references), and declared property values.
- **A system template**: a standard (a standard template by name, or an existing standard), a
  component template or a product per role, and declared system property values.
- **A place template**: a nested tree of places, each naming a location type, an optional label, and
  optional nested places and system templates.

A location type is not a template kind. Since
[ADR-0106](/architecture/decisions/#adr-0106-a-location-type-is-platform-owned-and-a-nullable-object-clears-under-the-mask)
a location type is an official row an operator reshapes through the
[registry fork](/architecture/decisions/#adr-0095-an-operator-forks-a-shipped-registry-row-instead-of-the-platform-writing-it),
so what places get is a template for a **subtree of places**, each place's type checked against its
parent's allowance, and no wording or logic keyed on any particular type.

## How a template ships

**Authored as kind-tagged YAML in the repo and `go:embed`ed into the binary** (an `internal/template`
package), exactly like the seed files; the difference is the loader. A **seed** inserts rows unasked; a
**template** inserts nothing. The loader exposes a read-only catalog, validated against the template
schema at load, and never writes a row: a template addressable as a row, or inserted wholesale, would
become shipped data carrying every update-versus-local-edit problem the model exists to avoid. A build
test dry-runs every template against the seeded catalogs in a rolled-back transaction, so a template
naming a product, a type or a property a release withdrew fails the build rather than an operator.

There is **no template versioning**: a template is addressed by its name and replaced by the next
release. Only shipped templates exist; an operator authoring or saving a template of their own is
[#1330](https://github.com/hyperscaleav/omniglass/issues/1330). Once the catalog ships, the stand-in
standards seed retires: a fresh install starts with no standards, and the catalog offers them.

## Reading and starting from a template

- **`GET /templates`** (filtered by `kind`) and **`GET /templates/{template}`**, gated
  `template:read` on the viewer floor. A catalog read is a registry read: no owner and no scope
  injection.
- **`POST /standards:fork`**, gated `standard:create`: one operator-owned standard
  (`official: false`) with its roles, choices and contract lines, under a name the operator chooses
  (the template's by default).
- **`POST /components:instantiate`**, **`POST /systems:instantiate`** and
  **`POST /locations:instantiate`**, each gated by its resource's own create permission. Every piece
  a call creates (a component, its endpoint attach, a system and its standard, a place) is checked
  as its own route checks it, in the caller's own scope, audited, and landed in **one transaction**:
  all of it or none. A target place outside the caller's location scope is a non-disclosing 404.

**Provenance lives in the audit trail only**: each created row's audit detail names the template it
came from. No column and no back-pointer, which keeps the fork severed while still answering "where
did this come from".

## Why this dissolves the shipped-defaults problem

The thing the vendor updates and the thing the operator owns are **never the same object**, so a
template change never needs a migration. Authoritative rows either overwrite operator edits or
freeze the defaults; pinned versions mean persuading every install to re-point. Forking has nothing
to reconcile.
::::

:::design[Trust: the signature and the capability manifest, tracked in #318]
The trust surface of a catalog accepting templates from outside the binary (the hosted /
marketplace path), which matters only once templates arrive from somewhere other than the release:

- **template signature / attestation**: an optional author signature verified on import;
  authenticity, distinct from the content-hash integrity check. Verified on the hosted /
  marketplace path regardless of the self-host runtime stance.
- **capability manifest**: which write-commands and credential shapes a template exercises, shown
  and approved at fork time, so a device-mutating template never gains powers the operator did not
  see.
:::
