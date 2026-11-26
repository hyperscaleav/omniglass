---
title: "The simulator derives a lab calendar of meetings per room (#2764)"
date: 2026-11-26
refs: [2757, 2764]
---

A scenario can now declare a `calendar:` block, and a simulated room's meetings follow from it
(ADR-2757). `calendar.meetings` holds knobs per standard (booking rate, a slot mix of 15, 30 and 60
minutes, no-show, ended-early and ended-late rates, an attendance range, a walk-in rate and the hours
of the day), refused at load naming the field when one is out of range; the corporate scenario ships
knobs for every standard with a room. The new `sim/calendar` package derives each room's meetings
from those knobs, a seed and a window as a pure function: slots on wall-clock boundaries, starts and
ends jittered (a late end pushes the next start, so a room's meetings never overlap), no-shows,
walk-ins in the gaps, and a room's day identical across runs, engines and windows. The engine holds
the calendar (`Engine.SetCalendar`) and a template reads it through the device's clock:
`meetingNow` and `meetingNext` for its own room, `roomMeetingNow` and `roomMeetingNext` by room key
for a service that speaks for many. Nothing yet reads it: the device profiles, the problem source
and `GET /lab/calendar` are the slices that follow, and the knobs and each device's room do not yet
reach a running simulator (the server's device list names neither).
