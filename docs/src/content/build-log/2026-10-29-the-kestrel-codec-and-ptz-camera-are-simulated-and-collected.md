---
title: "The Kestrel codec and PTZ camera are simulated and collected (#897)"
date: 2026-10-29
refs: [897, 1266]
---

The Kestrel Relay C1 codec and Gaze P2 PTZ camera had no simulator profile: neither is a video bar,
so neither could share the VRoom's. The dev fleet places six Gaze cameras, which the lab collected
nothing from; the codec ships in the catalog without a place in the dev fleet yet, and now has a
profile that serves it the day one is placed.

The codec speaks the Kestrel API the bar does, over https behind basic auth with the same live feed,
reusing the bar's route templates for its system, call and audio, so the bar's own `kestrel-api`
driver reads it unchanged: call state, participants, volume, mute and power. It adds what a rack codec
has: content sharing as a source of its own (started, stopped, cleared by a hang-up) and the
peripherals wired to it.

The camera answers the same API family for its system info and its camera state: the preset it sits
at, whether it tracks a presenter, and its pan, tilt and magnification. Recalling a preset stops
tracking, as a PTZ camera does, and while tracking a behavior moves the pan within the lectern's
sweep. A new `kestrel-camera-api` driver reads `camera-preset` and `tracking-state`, both new to the
boot seed, beside identity and power, attached in the lab with the bar's seeded login.

The node tests that drove the bar's control socket picked "the first `kestrel-api` profile", which is
the codec now; `driverProfile` takes the faces a caller needs, and those tests name the websocket.
