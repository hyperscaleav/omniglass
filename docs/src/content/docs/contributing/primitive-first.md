---
title: Primitive first
description: Build the reusable primitive, then consume it. The shared engines (expression, ViewResult, gateway, cascade, timer) the rest of the system is written against.
---

Build the **reusable primitive first, then consume it.** A behavior that more than one feature
needs is a primitive: define it once, test it once, and write every consumer against it. Do not
inline a one-off where a primitive belongs, and do not grow a second variant of something that
already exists.

## Why

- **One tested thing, not N.** A primitive carries its own full test set, so every consumer
  inherits correctness. N inlined copies are N places to drift and N places a bug can hide.
- **Consistency by construction.** When one engine backs every site, a contributor (and an
  operator) learns it once: the `filter` you write for a dynamic group is the `filter` you write
  for a list.
- **The learning tool renders the real engine.** Operator surfaces teach a concept by running the
  primitive against real data ([learning tool](/contributing/learning-tool/)), so a primitive is
  the teaching artifact, not a diagram.

## The primitives the system is written against

| Primitive | One model for | Doc |
|---|---|---|
| **Expression engine** | list `filter`, rule `scope`, dynamic-group membership, `fire_criteria`, calc `reduce` | [expressions](/architecture/expressions/) |
| **`ViewResult` renderer contract** | every read beyond one resource (`{columns, rows}`, one renderer) | [views](/architecture/views/), [UI](/architecture/ui/) |
| **Storage Gateway** | the only DB door: scope and in-transaction audit by construction | [storage](/architecture/storage/), [identity and access](/architecture/identity-access/) |
| **Cascade** | resolving config, credentials, and variables down one tree | [cascade](/architecture/cascade/) |
| **Admission consumer and the two lanes** | the one owner fence and the one data / record split on the bus | [messaging](/architecture/messaging/) |
| **Timer and clock** | schedule, watchdog, for-duration, and runbook-wait, all one durable model | [time](/architecture/time/) |
| **The `action` row** | every long-running operation's handle, rule-fired or API-called | [API](/architecture/api/), [alarms and actions](/architecture/alarms-actions/) |
| **`datapoint_type` registry** | one registry across metric, state, and log | [datapoints](/architecture/datapoints/) |

## How to apply

- **Reach for the primitive before writing the one-off.** Need a filter, a read, a scheduled fire,
  a scoped query? It already exists, consume it. If you are about to hand-roll one, stop.
- **Extract on the second use, not the third.** The moment a pattern is copied it is a primitive
  that has not been named yet. Pull it out, give it tests, point both callers at it.
- **A primitive lands with its tests and its first consumer in the same slice** (vertical, not
  horizontal): build the primitive, prove it with one real consumer, ship both. The
  `/add-collection-primitive` and `/canonical-datapoint` skills are this doctrine made procedural.
- **Do not fork an engine.** A second filter language, a second DB path, a second timer model is
  the anti-pattern this doctrine exists to prevent.

This composes with the others: the [API](/contributing/api-first/) is generated from the
primitives, [test-driven](/contributing/test-driven/) tests each primitive once, and the
[learning tool](/contributing/learning-tool/) renders the real one.
