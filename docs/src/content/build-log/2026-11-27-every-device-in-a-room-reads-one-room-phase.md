---
title: "Every device in a room reads one derived room phase (#2797)"
date: 2026-11-27
refs: [2797, 2757, 2798]
---

The timing rules of a room (how long before a booked meeting it wakes, how long after one it stays in use,
which stretch of a long meeting shares content) were worked out again in each device profile, so retuning
one split the room. They are now one Go primitive, `calendar.StateAt`, which gives a room at an instant one
phase (idle, waking, in a meeting, sharing, cooling down, impaired) and the facts its devices read.
The wake lead, cooldown tail, recording tail and sharing stretch are declared once per standard in the
`calendar.meetings` knobs, defaulting to the values the profiles carried, so every reading is unchanged.
Templates read it with `roomPhase`, `roomPhaseOf` and `ownerPhase`; the engine computes a room once per
second however many devices read it. The displays, projector, control processor, panel, switcher, extender,
PDU, video bars, codec, camera, microphones, DSP, amplifier, people counter and recording service all moved
onto it, `roomInUse` and `meetingSharing` retired, and a guard test fails the build if a profile writes
timing arithmetic again. A scripted problem shows as the impaired phase.
