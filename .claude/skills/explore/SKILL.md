---
name: explore
description: "Use when a scheduled session wakes to be Omniglass's synthetic user (#961): take today's mission, stand up its seeded stack, pursue the mission's goal as its persona through the CLI, the API and scripted console replays, bring at most two specialist lenses, and file what got in the way (bugs one each with an executable repro, suggestions under the week's epic, sightings on what is already open). Finds and files only; never fixes, branches or opens a PR."
---

# The synthetic user's day

A scheduled Routine fires a fresh session; this skill is everything it does. The program and
its contract are [synthetic user](../../../docs/src/content/docs/contributing/synthetic-user.md).
Every decision that needs no judgment is made by `web/e2e/explorer.mjs` (pure logic in
`explorerlib.mjs`, tested); spend tokens only where judgment is the point.

## Ground rules

- **Find, never fix.** No branch, no commit, no PR. A defect becomes an issue; the fixer and
  the autopilot fix things.
- **Evidence over browsing.** Never drive the console screenshot by screenshot. Drive it with
  walkthrough scripts replayed by the harness, and open an image only when a failure frame or
  a specific still is the evidence you need.
- **Act as the persona.** Use the mission's `user` (its token is in `stack.env`), never the
  owner token, unless the step is impossible for that user and the impossibility is itself the
  finding.
- **House style** in everything filed: no em dashes, no AI or assistant attribution.
- **Budgets** (the daily ceiling, enforced by these caps since a session cannot read its own
  token count): at most 2 lenses, 6 repro replays, 10 images opened, 12 findings filed. Reaching
  a cap ends exploring: file what you have and say in the report which cap stopped you.

## 1. Pick

```bash
git fetch -q origin main
make build                                   # bin/omniglass, the CLI you drive
node web/e2e/explorer.mjs pick > /tmp/mission.json
```

`pick` returns the mission, the lenses (the mission's own, or a backstop lens unused for a
week), and why it was chosen. Do not second-guess it; steering is the missions file.

## 2. Stand the stack up and read the digest

Start it in the background (the Bash tool's `run_in_background`), logging to a file:

```bash
STACK_SCENARIO=<mission.scenario> PROBE_USER=<mission.user> bash web/e2e/probe.sh hold > /tmp/hold.log 2>&1
```

then wait for it with a second background command, `until grep -q 'holding the stack'
/tmp/hold.log; do sleep 5; done`, and continue when that one completes (a foreground sleep is
refused). It takes a few minutes: it builds the image and probes every surface first.

It probes every route and walkthrough first, then holds the stack. Read
`web/e2e/probe/out/digest.json` and nothing else from `out/` (raw.jsonl is not for reading).
`node web/e2e/explorer.mjs digest-keys web/e2e/probe/out/digest.json` gives each digest
finding the key it files under.

## 3. Pursue the goal

Load the environment (`set -a; . web/e2e/probe/out/stack.env; set +a`) and work toward the
mission's goal until its `done` holds or you are stuck:

- **CLI first:** `./bin/omniglass ...` (it reads `OMNIGLASS_SERVER` and `OMNIGLASS_TOKEN`).
  `--help` is part of the product under test.
- **API** with `curl -H "Authorization: Bearer $OMNIGLASS_TOKEN" $OMNIGLASS_SERVER/api/v1/...`
  when the CLI has no verb for it (itself worth noting).
- **Console** through a walkthrough script (the format is in `web/e2e/walkthroughlib.mjs`),
  written to `web/e2e/probe/out/repro/<id>.yaml` and replayed with
  `bash web/e2e/probe.sh replay web/e2e/probe/out/repro/<id>.yaml`. It prints its own
  directory, which holds a still per step (`<id>-step-N.png`, so a passing script shows the
  state it reached) and a `digest.json`. Exit 0 reached the end; 1 failed at a step (the digest
  names it and its frame); 2 never ran the script at all, which proves nothing about the product.
- **A node,** when the mission needs collection and the scenario has none (a fresh install):
  enroll one as the persona would (`./bin/omniglass node create ...`, then `node enroll`, both
  in `--help`), then `bash web/e2e/probe.sh node <name> <enrollment-token>` runs it on the
  stack's network. The steps it took to get there are part of the mission.

Keep a terse journal in `web/e2e/probe/out/notes.md` as you go: each step, what you expected,
what happened, every command that errored or misled, how long things took. The lenses read it,
and the ai-friendliness lens treats your own stumbles as findings.

## 4. Bring the lenses

Dispatch the picked lenses in parallel, each with the `lens-<name>` agent type
(`.claude/agents/`; if the type is not offered, a general-purpose agent told to read
`.claude/agents/lens-<name>.md` and act as it), giving it: the mission JSON, the paths of `digest.json`, `stack.env` and `notes.md`, and the
date. Each returns a JSON array of findings in the shape `findingProblems` accepts and nothing
else. Do not re-read what a lens already digested.

## 5. Decide what is a finding

Merge your own findings with the lenses' into `web/e2e/probe/out/findings.json`. For each one:

- **Lens and key:** your own findings, and the digest's, are lens `explorer`. A digest finding
  keeps its `digest-keys` key (and its repro is the probe, so it needs none of its own);
  anything else gets a stable slug (`<lens>:<surface>:<what>`), the same words you would use if
  you saw it again tomorrow.
- **Already filed?** `gh issue list --search "<words>" --state open` for anything a person
  may have filed without a key. If one covers it, `node web/e2e/explorer.mjs adopt <n> <key>`
  and the plan will record a sighting on it instead of a duplicate.
- **A bug needs a repro that fails on main:** the full text of a replay script that fails, or a
  CLI or curl script and its failing output, in the finding itself (the issue outlives this
  session's scratch files). No repro, no bug: file it as a suggestion or drop it.
- **Area** is one of the repo's `area:` labels; `validate` names them when one is wrong.
- **Severity** is the operator's cost, not your confidence: high means the goal cannot be
  reached or data is wrong; medium means it is reached with real friction; low is polish.

`node web/e2e/explorer.mjs validate web/e2e/probe/out/findings.json` must pass.

## 6. File and report

Write `web/e2e/probe/out/report.md`: the mission and persona, whether `done` held, the
three things that most got in the way, any cap that stopped the run, and the day's slowest
routes from the digest. Then:

```bash
node web/e2e/explorer.mjs file web/e2e/probe/out/findings.json --mission <id> \
  --lenses <a,b> --report web/e2e/probe/out/report.md --dry-run   # read the plan
node web/e2e/explorer.mjs file web/e2e/probe/out/findings.json --mission <id> \
  --lenses <a,b> --report web/e2e/probe/out/report.md
```

`file` opens the week's epic if needed (rolling last week's over), files bugs, files
suggestions under the epic up to the weekly cap, records sightings, and posts the report with
the run marker the next `pick` reads. Always run it, even with no findings: the marker is
how tomorrow knows today happened.

## 7. Release

`bash web/e2e/probe.sh release`, then end with a three-line summary: mission, done or not,
what was filed.
