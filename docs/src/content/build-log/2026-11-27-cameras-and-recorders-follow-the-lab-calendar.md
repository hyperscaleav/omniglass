---
title: "The PTZ camera and the recording service follow the lab calendar (#2775)"
date: 2026-11-27
refs: [2757, 2775]
---

The conference standard now staffs a camera and a recorder slot (Kestrel Gaze P2 and the recording service). The
camera reports the people actually in its room, nobody in a no-show, frames the group when a meeting starts and
returns to its home preset when it ends; an empty room's camera holds still. The recording service records a
seeded share of the meetings that took place, set by the new `recording_rate` meeting knob (a no-show is never
recorded): a recording is in progress while a recorded meeting runs in a room it covers, and queued for ten minutes
after. The recording is a draw of its own from the seed, the room and the slot, so no other part of the day moves.
A new `meetingPrev` template function reads the last meeting that has ended. A device in a room with no calendar
keeps its old drift. Scheduled recording failures are #2769.
