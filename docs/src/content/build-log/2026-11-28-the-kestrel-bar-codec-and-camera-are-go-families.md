---
title: "The Kestrel video bar, codec and camera are Go families (#2834)"
date: 2026-11-28
refs: [2834, 2826, 2829]
---

The Kestrel VRoom video bar, Relay C1 codec and Gaze P2 PTZ camera are now Go packages under `sim/device/`
(`kestrelvroom`, `kestrelrelay`, `kestrelgaze`), and their three YAML profiles and templates are
deleted. Each is one wire API with its model as a row of a typed table: the three APIs differ (the bar's
status, app and private log routes, its web admin, its live feed and control socket; the codec's call,
content and peripherals; the camera's pan, tilt, presets and tracking), so they are three families, each
over the same basic-auth https face. JSON is typed structs, the documents that carry a template's comment
lines keep them through `httpapi.Annotated`, controls are `httpapi.Control` (which now also writes the
changes a field implies, as a preset recall moves the lens), the dashboard is `html/template` over a typed
view, and the call, sharing, people count and call stats are `sim/behave` primitives and typed ticks. A
family reaches the live feed from Go: a route publishes a named event after its writes and a tick names the
events it reports from the state it left. Readings are identical because they were recorded: a script ran
against each profile first, its transcript (bytes, headers, state, the named events, seeded drift and the
pinned room day with and without a calendar, and the console's listing) is what every family test replays,
and ADR-2834 lists the three places the family corrects the profile.
