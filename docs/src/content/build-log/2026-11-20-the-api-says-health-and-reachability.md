---
title: "The API and CLI say health and reachability, not verdict (#2244)"
date: 2026-11-20
refs: [2086, 2102, 2243, 2244]
---

ADR-2086 (#2086) retired verdict as the word for health's result; #2243 renamed the Go code, and
this is the API half of #2102. A client now reads `health: degraded`, never `verdict`. The fleet
view's locations, systems and dots, the system and location health reports and their
transitions, a component's alarm list, the platform view's system, servers and outposts, and the
bulk read all carry `health`; a system member's `component_verdict` is `component_health`. The
bulk read `GET /systems:health` is `list-system-health`, its body `SystemHealthListOutputBody`
with a `systems` list of `SystemHealthEntryBody` entries. An endpoint's up/down result in
`GET /components/{name}/reachability` is `reachability`, its shape `ReachReadingBody`. Route
paths do not change, and there are no aliases.

`omniglass platform health` follows: its `--json` reading names each part's `health`, and the
table's server column is `HEALTH`. The console's call sites read the renamed fields; its own
wording and the `Verdict` type in `web/src/lib` are #2103. The soak, perf and footprint gates, a
subscription's test and a target's test send keep verdict: those judge a run, not health.
