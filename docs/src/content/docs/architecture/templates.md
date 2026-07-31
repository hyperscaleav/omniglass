---
title: Templates
description: "Example configurations an operator clones. Forking is one-time and leaves no link back, which is exactly why a template can be improved in any release without migrating anyone."
sidebar:
  badge:
    text: Design
    variant: caution
---

A template is an **example configuration an operator clones**. It ships inside the binary as a menu,
never as data: nothing is inserted until somebody picks one, and what they get is an ordinary row
they then own outright.

The property that makes this worth having is what happens next: **nothing points back at the
template**. Forking is one-time, with no inheritance and no back-pointer, so a template can be
rewritten in any release without migrating a single install. Improving the examples we ship costs
nothing, because no instance is watching.

:::caution[Design]
Nothing on this page is built. The interim state is that standards and location types **seed as
ordinary rows** (`official: false`, inserted only if absent, so an operator's edit is never reverted
on the next boot). The template loader, the catalog it exposes, and the create-from-template flow are
tracked by [#317](https://github.com/hyperscaleav/omniglass/issues/317).
:::

## Fork, not pin

This page used to describe the opposite model, and the contrast is the fastest way to understand the
current one.

The old model was **immutable versioned shapes that instances pin**: a component pinned a versioned
device shape, a system pinned a versioned composition shape with a frozen bill of materials, editing
one minted a new version, and an instance tracked `latest` or followed a `stable` / `beta` channel.
The whole point of the pin was that **the shape could not change under an instance**.

The current model inverts that. A template is not a shape an instance holds on to; it is a starting
point an instance is copied from and then forgets. The point of the fork is that **a template can
change freely, because no instance is watching**. Same word, opposite purpose, which is why the two
were never reconcilable and why holding both made every page hedge
([ADR-0071](/architecture/decisions/#adr-0071-a-template-is-a-clonable-example-not-a-versioned-shape-an-instance-pins)).

What replaced the pinned shapes is not a template at all:

| The shape of a... | is its... | and the relationship is |
|---|---|---|
| component | [`product`](/architecture/core-entities/), with its `product_property` contract | a pointer, `component.product_id` |
| system | [`standard`](/architecture/core-entities/) | **conformance**, with live inheritance |

**Forking applies template to row. Conformance applies row to instance.** They are different
mechanisms and only the second is live: a standard's contract default resolves for every conforming
system until that system overrides it, and that link is real and permanent. A template's link is
severed at creation.

## What a template can create

Anything an operator would otherwise build from nothing, at three sizes:

- **A [location type](/architecture/core-entities/)**, so a new install can start from Campus,
  Building, Floor rather than inventing a hierarchy vocabulary on day one.
- **A [standard](/architecture/core-entities/)**, the composition shape a system conforms to: a
  huddle room, an auditorium, a training room.
- **A system**, instantiated whole. This is the operator-facing one: "start from: huddle room" should
  produce a working system, not merely the standard it conforms to.

The first two generalize the fork-seed model already recorded in
[ADR-0048](/architecture/decisions/#adr-0048-the-standard-blueprint-and-the-template-fork-seed-model);
the third is the reason the model is worth building rather than only worth writing down.

## How a template ships

A template is **authored as YAML in the repo and `go:embed`ed into the binary**, exactly like today's
seed files. Same authoring format, same git-diffable review. The difference is not the format, it is
what the loader does with it:

- a **seed** inserts rows whether or not anyone wanted them;
- a **template** inserts nothing. It sits in the binary as a catalog the create-from-template flow
  reads, and writes exactly one row when an operator forks it.

Offered, not applied. A template must never be addressable as a row, and must never be inserted
wholesale, or it stops being a template and becomes shipped data carrying every
update-versus-local-edit problem the model exists to avoid.

## Why this dissolves the shipped-defaults problem

The thing the vendor updates and the thing the operator owns are **never the same object**. That is
the whole trick, and it is why a template change never needs a migration.

Both alternatives fail on the same point. Shipping standards as authoritative rows means a release
either overwrites an operator's edits or gives up on improving the defaults. Shipping them as pinned
versions means every install carries a version pointer, and improving the example means persuading
people to re-point at something that may not be compatible. Forking has neither problem, because
there is nothing to reconcile.

## What a template carries

Open, and the answer decides how much a fork scaffolds in one write. A template that carries only the
row's own attributes is trivial to build. A template that also carries a **property contract**, and
later **[system roles](/architecture/health/)**, would let "start from: huddle room" scaffold the
slots a room needs filled as well as the row itself, which is most of the operator value. Tracked as
the open question on [#317](https://github.com/hyperscaleav/omniglass/issues/317).
