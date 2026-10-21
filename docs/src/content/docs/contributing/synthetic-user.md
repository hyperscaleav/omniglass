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

Those raw events land in `raw.jsonl` in the probe's output directory (`probe/out/`, beside `probe.sh`, gitignored), which no agent reads. The reducer
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

## A day's run

The [`/explore` skill](https://github.com/hyperscaleav/omniglass/tree/main/.claude/skills/explore)
is the whole day, and `web/e2e/explorer.mjs` makes every decision in it that needs no judgment
(its logic is `explorerlib.mjs`, unit-tested):

1. **Pick.** `explorer.mjs pick` chooses one mission from
   [`web/e2e/explorer/missions.yaml`](https://github.com/hyperscaleav/omniglass/blob/main/web/e2e/explorer/missions.yaml):
   one whose surfaces (repo paths) changed since its own last run comes first, most changed
   first, unless it ran in the last three days (so a mission over a busy part of the tree cannot
   starve the rest); else the one run least recently. A mission is a persona, the seeded user it signs in as, a
   goal, the scenario it starts from, the lenses it wants and what `done` looks like. The user
   is never the owner (except on a fresh install, which has no one else), so a missing
   permission or a scope leak is a finding rather than something owner rights hide.
2. **Stand up and probe.** `probe.sh hold` probes the scenario's stack, then keeps it up with the
   console published on a local port and `stack.env` (in the same output directory) holding the address and a
   token for the mission's user. `probe.sh replay <script>` plays one walkthrough script against
   it in a directory of its own with a still per step (exit 0 reached its end, 1 failed at a
   step, 2 never ran), which is how the run drives the console and how a repro is proven.
   `probe.sh node <name> <token>` runs a node the persona enrolled on the stack's network, how a
   fresh install gets a collector. `probe.sh release` tears it all down.
3. **Pursue the goal** as the persona: the CLI first, the API where the CLI has no verb, the
   console through replayed scripts, never screenshot by screenshot. The run keeps a journal of
   every step and every stumble.
4. **Bring the lenses**, at most two, as subagents: `lens-performance`, `lens-design`, `lens-ux`,
   `lens-ai-friendliness` and `lens-accessibility` (`.claude/agents/`). The mission names its
   lenses; any lens unused for a week takes the second slot, so none goes dark. Each returns
   findings in one fixed shape (lens, kind, severity, key, area, surface, claim, evidence,
   failure scenario, and for a bug its repro as the script itself), and `explorer.mjs validate`
   refuses anything else, including an `area` that is not one of the repo's labels. The run's
   own findings, and the digest's, carry the lens `explorer`.
   The design lens grades with the `/assess-ui` rubric rather than its own; the AI-friendliness
   lens reads the run's own stumbles as evidence of how an agent fares with the CLI, API and docs.
   The autopilot may dispatch the same lenses over its own slices.
5. **File and report** with `explorer.mjs file` (below), then release the stack.

## Filing

Every finding carries a **key**, the stable name of the defect, which its issue's last line
records (`explorer-key: <key>`). A digest finding's key comes from the digest itself
(`explorer.mjs digest-keys`), so two lenses or two days reporting it agree.

- A finding whose key an open issue already carries is a dated **sighting** on that issue,
  never a second issue. An issue a person filed without a key is **adopted** first
  (`explorer.mjs adopt <issue> <key>`) so the next sighting finds it.
- A **bug** is its own `Bug` issue, filed at once with its repro, because the fixer drains
  bugs daily.
- A **suggestion** is a `Task` sub-issue of the week's epic, `explorer: week of <Monday>`, at
  most 15 a week, most severe first; past the cap it is reported, not filed.
- The first run of a week opens its epic and rolls last week's over: a suggestion seen again
  during the week is carried into the new epic, one promoted (retyped from `Task`) is left where
  it now lives, and the rest close as not planned. The backlog cleans itself; anything real
  comes back as a sighting.
- A failure partway through filing is named in the report and the rest still files; a re-run
  recognizes what the same day already filed instead of sighting it.
- Each run posts its report on the week's epic, ending with a marker line
  (`explorer-run: <date> mission=<id> lenses=<a,b>`). The run log is those lines, which is how
  `pick` knows what ran when without any state on disk.

Weekly triage is the architect's: promote what is worth doing, ignore the rest.

## The ceiling

A run's token ceiling (250k a day to start, reset after two weeks to the observed 90th
percentile plus 25%) is enforced by caps a session can count, since it cannot read its own token
use: at most two lenses, six replays, ten images opened and twelve findings filed. Reaching one
ends the exploring; the run files what it has and its report names the cap.

## The Routine

One scheduled Routine fires a fresh session daily on the same worker machine as the
[autopilot](/contributing/autopilot/) (Docker for the stack, an authenticated `gh`, the
Playwright Chromium), with a thin prompt so the behavior lives in the versioned skill:

```text
Omniglass synthetic user: run today's mission. Work in the hyperscaleav/omniglass clone in
this environment on a clean checkout of origin/main. Read CLAUDE.md, then invoke the /explore
skill and follow it end to end: pick the mission, hold its stack, pursue the goal as its
persona, bring the picked lenses, file with explorer.mjs, release the stack. Find and file
only; never branch, commit or open a PR.
```
