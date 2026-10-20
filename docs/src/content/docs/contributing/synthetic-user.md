---
title: Synthetic user
description: "A scheduled agent uses Omniglass every day toward a concrete operator goal on a freshly seeded fleet, and files what it finds; the harness does the clicking and the timing so the agent only spends tokens on judgment."
---

The **synthetic user** is a standing program that uses Omniglass the way an operator would and
reports what got in its way ([#961](https://github.com/hyperscaleav/omniglass/issues/961)). It is
built so that tokens are spent on judgment only. Everything deterministic, from standing a fleet up
to driving every page and timing every call, is harness work whose output is one small file an agent
reads.

## The fleet it runs against

Every run starts from a throwaway stack seeded with a **scenario**: a fresh install, the example
fleet, the example fleet having a bad day, or the example fleet at enterprise size. Scenarios are
`omniglass seed-dev --scenario <name>`; see [seed a different fleet](/guides/install/#seed-a-different-fleet).

## The probe and its digest

`make probe` brings up the [walkthrough stack](/guides/install/#the-whole-loop-in-docker) (the
fleet picked by `STACK_SCENARIO`), visits every route in the console's route manifest, and replays
every [walkthrough](/contributing/walkthroughs/) at machine speed, a beat after each action rather
than a person's pace. On every surface it records:

- console errors and uncaught page errors;
- every API call, its status and its time;
- the page's largest contentful paint and cumulative layout shift;
- the accessibility rules [axe-core](https://github.com/dequelabs/axe-core) finds violated (WCAG 2 A
  and AA);
- for a walkthrough, whether it reached its end, and if not the step, Playwright's account of what it
  waited on, and the frame it failed on.

Those raw events land in `web/e2e/probe/out/raw.jsonl`, which no agent reads. The reducer
(`web/e2e/digestlib.mjs`) folds them into `digest.json` beside it: findings grouped by what they are
(a console error seen on forty rows is one finding with a count and the surfaces it was seen on; an
API call is grouped under its OpenAPI path template, read from the generated `api/openapi.json`, so a
slow route called once per device is one finding, not one per device),
ranked breakage first (page errors, 5xx answers, a walkthrough that failed), then friction (slow
routes, slow paint, accessibility), and capped at 16 KB of the file as written whatever the run saw, with a count of what
the cap dropped. It depends only on what the run saw, never on the order the listeners reported it.
The probe also records how many surfaces it meant to drive, so a route that hung and took the ones
after it down shows up as a finding rather than as a quietly smaller run. The slowest routes ride along even within budget, so a reader can see a breach coming.

A miss face (a parametrized route visited with an id that does not exist) is expected to answer 404,
so a 404 on one is not recorded; those pages are only ever probed in that state, so a real 404 on them
would be hidden too, and their populated state is a mission's to exercise. Everything else is, and nothing in the digest is a gate: the probe exits
non-zero only when it could not run, since what it found is evidence for a reader, not a verdict.
