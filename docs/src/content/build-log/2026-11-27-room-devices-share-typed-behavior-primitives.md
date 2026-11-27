---
title: "The room devices' behaviors are shared, typed Go primitives (#2830)"
date: 2026-11-27
refs: [2830, 2826]
---

`sim/behave` holds the room-device behaviors as pure Go functions of the room phase, the device's previous typed
state, typed parameters and the instant: power and input follow the room, the call cycle and content sharing, people
count, climate, the 48-hour battery, recordings, building hours, bookings, and the mic, mixer, amplifier and PDU
readings. Randomness comes only through a seeded per-device source that draws the stream the engine's behaviors do,
and each primitive carries its no-calendar fallback and its defaults as typed parameters. A parity test per
primitive drives the current profile through the engine and compares the paths the primitive owns, a tick at a time
over a pinned room day. No profile changed and no device moved; the families compose the primitives in the sibling
slices of #2826.
