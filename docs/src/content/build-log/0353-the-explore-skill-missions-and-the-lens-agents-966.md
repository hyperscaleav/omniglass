---
title: "The explore skill, missions and the lens agents (#966)"
date: 2026-10-20
refs: [966]
---

The synthetic user's daily run is the `/explore` skill over `web/e2e/explorer.mjs`, whose
decisions are pure and tested in `explorerlib.mjs`: which mission today (one whose repo surfaces
changed since the last run, else the least recently run), which lenses (the mission's own, with
a lens unused for a week taking the second slot, counted from the first run on record), the one
finding shape every lens returns, and the filing plan (a known key is a sighting, a bug is its
own issue, a suggestion is a sub-issue of the week's epic under a weekly cap, and the weekly
roll-over carries what was seen again and closes the rest). State lives on GitHub as marker
lines, so a fresh session needs nothing on disk. Seven missions ship in
`web/e2e/explorer/missions.yaml`, each signing in as a seeded non-owner user; a test holds every
mission surface to a path that exists. Five lens agents (`.claude/agents/lens-*.md`, Sonnet)
carry narrow rubrics, the design lens reusing `/assess-ui`'s. `probe.sh` gained `hold` (keep the
probed stack up, published, with a token for the mission's user in `stack.env`), `replay` (play
one script against it from the host, exit 0 when it reached its end, how a repro is proven) and
`release` (a TERM to the holder, since a backgrounded shell ignores INT). A fresh install now
stands the stack up as the server and its owner alone, where the scenario slice had refused it.
Proven live: hold on the dev and degraded scenarios as non-owner users, the CLI driven with the
held token, a broken script failing replay and a good one passing, release leaving no container,
and the empty scenario probed. The first live probes' accessibility bugs (#972 to #974) were
adopted with their digest keys.

Before it merged, a supervised trial had Sonnet run the whole loop unaided on the fresh-install
mission (it reached `done` through the CLI and API, dispatched both lenses and produced thirteen
valid findings), and its stumbles and an adversarial review shaped the slice: the weekly epics
are searched newest first (an unsorted search returns best match), a mission counts changes since
its own last run and cools down for three days so a busy area cannot starve the rest, a
finding's area is checked against the repo's real labels before anything is filed (nine of the
trial's thirteen named labels that do not exist), filing reports a partial failure instead of
stopping, replays run in their own directories with a still per step and an exit code that tells a
failing script from a replay that never ran, `release` only signals a process that is a probe
hold, `probe.sh node` runs a node the persona enrolled, and the walkthrough recorder and the
stack check refuse the fresh-install scenario, which has no fleet to walk.
