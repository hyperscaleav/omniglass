---
title: "Learn opens guided lessons that drive the live console (#2450)"
date: 2026-11-23
refs: [2450, 270, 2427, 2425, 2462, 2424]
---

The sidebar's Learn entry was a placeholder. It now opens six lessons in monitoring, in lifecycle
order: from device to samples, reach and collection, rules and alarms, health and impact, acting on
it, and reading the history. Each lesson is a typed definition in `web/src/lib/lessons.ts`: steps
of short prose, a live element that is an existing console component over a simulated component
(the simulation drawer's state, the component's samples, the delivery pipeline, the reach ladder, a
threshold previewed by the event rule preview, the alarms, the systems it serves with their health,
its commands, its activity), and a "Do it in the console" link that opens the real page on that
component. The same definitions generate `docs/src/generated/lessons.json`, which the new
[Learn in the console](/start/learn/) page renders, so the docs and the console cannot drift.

Finishing a lesson is remembered in the person's own settings. The settings cascade had no user
level yet (it was designed under #270), so this slice builds it for profile-domain namespaces: a
`user` scope on `setting_binding` tied to its person, `PATCH /settings/me/{namespace}` for the
caller's own binding (a platform-domain namespace is a 403, a platform-locked key a 409), and
`GET /settings/me` resolving it over the platform. The new `learn` namespace holds `done`.

Four steps would be better with surfaces siblings are building (test an endpoint #2427, the overlay
timeline #2425, one-click simulated faults #2462, the metrics grid #2424). Each is written against
what the console has today and carries its better form behind a capability that is off until that
surface lands. With no simulator connected, the page says so and links the lab instructions. The
rule preview moved out of the event rules page into its own component so the lesson reuses it.
