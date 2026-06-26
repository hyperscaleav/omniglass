---
title: The learning-tool restriction
description: Every operator surface should also teach the concept it operates on, against real or simulated data.
---

Omniglass is two things at once, by design: **a functional tool and a learning tool.**
This is a standing design restriction, not a nice-to-have. It shapes what we build and how
we judge it done.

## The restriction

**Every operator surface should also teach the concept it operates on.** Where it makes
sense, a page is not just a control panel over data; it is an interactive explanation of
the concept and the data flow behind it, driven by real or simulated data.

A feature that introduces a concept (a collection function, an edge parse step, a calc rollup, an
alarm lifecycle) should ship a surface where a learner can *see the concept happen*:

- the function or pipeline rendered, not just described,
- real or simulated data moving through it,
- the ability to poke it and watch the result change.

## What it teaches, and what it does not

The audience is **AV and IT systems integrators and operators**, and the subject is **monitoring**: what
it is, how to do it well, and how Omniglass models and monitors an estate, so an operator understands the
data they get and the judgment behind it. It teaches the **AV Observability discipline** made concrete,
the Align / Measure / Instrument / Practice layers as explorable artifacts rather than a PDF.

It is **not** a software-engineering tutorial. It does not teach how to write software, how to architect
a platform, or how Omniglass is built internally. The learner is operating an estate and learning
**monitoring**, not reading source. "Teach the concept it operates on" means the *monitoring* concept (an
edge parse, a calc rollup, an alarm lifecycle, a health rollup), never the code that implements it.

## Why

The product is also the teaching artifact for the AV Observability discipline it
implements. The Measure and Instrument layers should be concrete, explorable artifacts
rather than blueprints in a PDF. A user who operates Omniglass should come away
understanding *how* it models their estate, because the tool taught them while they used
it.

## Real or simulated data

Teaching surfaces must work without a live fleet. A simulated/lab data source (the
emulated estate) backs the interactive pages so a learner, or a CI run, gets the same
explorable behavior as a live deployment. "Works against the lab emulator" is part of
done for a learning surface.

## How it interacts with the other doctrines

- **Docs with everything:** the teaching surface is part of the docs that ship with a
  feature. A concept-introducing PR that has no learning surface should say why in its
  docs note.
- **Test first:** the interactive surface is user-facing behavior, so it carries e2e
  coverage like any other surface.
- **Functional and pedagogical:** the learning surface rides on the *real*
  implementation and real (or lab-simulated) data. It is not a mock diagram detached
  from the engine; it is the engine, made legible.
