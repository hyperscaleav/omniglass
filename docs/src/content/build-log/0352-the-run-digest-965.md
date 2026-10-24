---
title: "The run digest (#965)"
date: 2026-10-20
refs: [965]
---

`make probe` brings up the walkthrough stack, visits every route in the console's route manifest and
replays every walkthrough at machine speed (a 300 ms beat after each action, since at none a key can
beat the drawer it should close), recording console and page errors, every API call's status and
time, web vitals, axe-core accessibility violations and walkthrough outcomes as raw events. A pure
reducer (`web/e2e/digestlib.mjs`) folds those into `digest.json`: grouped by kind, API calls by their
OpenAPI path template (read from `api/openapi.json`), ranked breakage before friction, capped at 16 KB
of the written file with a count of what the cap dropped, and independent of event order. A planned
surface count makes a run that never reached some surfaces a finding of its own. The digest is what the synthetic user (#961) reads instead of logs. The recorder's step
locator moved into `walkthroughlib.mjs` so the probe and the recorder cannot read a script
differently. Tests: the reducer's unit tests (an injected console error and an injected slow route
each appear by name, a clean run has no findings, the byte cap holds on the written file under a 2,000-error flood,
the output is the same for events in any order, a slow route called per device is one finding, an
unvisited planned surface is named); proven live with two consecutive clean probe runs on the dev scenario
(49 surfaces, about 640 API calls, every walkthrough passing). Its first run found the console's
contrast, an unlabelled profile field and nested controls on Files (#972, #973, #974).
