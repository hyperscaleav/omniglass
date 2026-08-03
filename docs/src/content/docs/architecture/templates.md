---
title: Templates
description: "Example configurations an operator clones. Forking is one-time and leaves no link back, which is exactly why a template can be improved in any release without migrating anyone."
sidebar:
  badge:
    text: Design
    variant: caution
---

::::design[Target design, tracked in #317]

A template is an **example configuration an operator clones**. It ships inside the binary as a menu,
never as data: nothing is inserted until somebody picks one, and the fork is an ordinary row they
own outright. **Nothing points back at the template**: forking is one-time, no
inheritance, no back-pointer, so a template can be rewritten in any release without migrating a
single install.

:::caution[Design]
Nothing on this page is built. Interim: standards and location types **seed as ordinary rows**
(`official: false`, inserted only if absent, so an operator's edit is never reverted on boot). The
template loader, its catalog, and the create-from-template flow are tracked by
[#317](https://github.com/hyperscaleav/omniglass/issues/317).
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
| component | [`product`](/architecture/core-entities/), with its `product_property` contract | a pointer, `component.product_id` |
| system | [`standard`](/architecture/core-entities/) | **conformance**, with live inheritance |

**Forking applies template to row. Conformance applies row to instance.** Only the second is live:
a standard's contract default resolves for every conforming system until that system overrides it.
A template's link is severed at creation.

## What a template can create

Anything an operator would otherwise build from nothing, at three sizes:

- **A [location type](/architecture/core-entities/)**: start from Campus, Building, Floor rather
  than inventing a hierarchy vocabulary on day one.
- **A [standard](/architecture/core-entities/)**: a huddle room, an auditorium, a training room.
- **A system**, instantiated whole: "start from: huddle room" should produce a working system, not
  merely the standard it conforms to.

The first two generalize the fork-seed model of
[ADR-0048](/architecture/decisions/#adr-0048-the-standard-blueprint-and-the-template-fork-seed-model);
the third is the reason the model is worth building.

## How a template ships

**Authored as YAML in the repo and `go:embed`ed into the binary**, exactly like today's seed files;
the difference is the loader. A **seed** inserts rows unasked; a **template** inserts nothing, a
catalog the create-from-template flow reads, writing exactly one row on fork. Offered, not
applied: a template addressable as a row, or inserted
wholesale, becomes shipped data carrying every update-versus-local-edit problem the model exists to
avoid.

## Why this dissolves the shipped-defaults problem

The thing the vendor updates and the thing the operator owns are **never the same object**, so a
template change never needs a migration. Authoritative rows either overwrite operator edits or
freeze the defaults; pinned versions mean persuading every install to re-point. Forking has nothing
to reconcile.

## What a template carries

Open, and the answer decides how much a fork scaffolds in one write: only the row's own attributes
(trivial), or also a **property contract** and later **[system roles](/architecture/health/)**,
letting "start from: huddle room" scaffold the slots a room needs filled, most of the operator
value. Tracked as the open question on
[#317](https://github.com/hyperscaleav/omniglass/issues/317).

## Trust: the signature and the capability manifest

The trust surface of a catalog accepting templates from outside the binary (the hosted /
marketplace path), two glossary terms:

- **template signature / attestation**: an optional author signature verified on import;
  authenticity, distinct from the content-hash integrity check. Verified on the hosted /
  marketplace path regardless of the self-host runtime stance.
- **capability manifest**: which write-commands and credential shapes a template exercises, shown
  and approved at fork time, so a device-mutating template never gains powers the operator did not
  see.
::::
