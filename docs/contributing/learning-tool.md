# The learning-tool restriction

Omniglass is two things at once, by design: **a functional tool and a learning tool.**
This is a standing design restriction, not a nice-to-have. It shapes what we build and how
we judge it done.

## The restriction

**Every operator surface should also teach the concept it operates on.** Where it makes
sense, a page is not just a control panel over data; it is an interactive explanation of
the concept and the data flow behind it, driven by real or simulated data.

A feature that introduces a concept (a collection flow, a transform, a calc rollup, an
alarm lifecycle) should ship a surface where a learner can *see the concept happen*:

- the flow or pipeline rendered, not just described,
- real or simulated data moving through it,
- the ability to poke it and watch the result change.

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
