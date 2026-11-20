---
title: "scale labs run omnibot"
date: 2026-11-20
refs: [2298]
---

`omniglass lab run --scenario corporate --systems N` now runs omnibot, as a seeded lab does. With
`--omnibot-key-env VAR` the scale lab's server gets the key as the env-backed platform secret
`lab-omnibot-key` and the assistant's provider and key settings, the same as a seeded lab; both
refusals (the plan's and the CLI's) are gone. Without a key, a scale lab now plays a recording too.

The lab's recordings are a small library, embedded and picked by name with `--omnibot-replay`:
`default` (the dev fleet's meeting 405, unchanged) and `corporate-outage`, which triages the corporate
fleet's `hq-mr86-1` room after its main display is unplugged. A recording scripts omnibot's words while
its tool calls run for real against the lab, so a demo clip shows real data without live inference.
The marketing captures (#2299) use it.
