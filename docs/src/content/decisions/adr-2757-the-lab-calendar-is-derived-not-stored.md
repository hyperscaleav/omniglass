---
title: "The lab calendar is derived, not stored, and a scripted source feeds it problems"
date: 2026-11-26
status: Accepted
summary: "A lab's meetings are one calendar every simulated room acts out, a pure function of the scenario's calendar block, a seed and a window, so every simulator and the server compute the same one with nothing stored or shipped; services that span rooms read it by room key; a scripted problem source joins it on the same clock as a second entry kind."
refs: [2757, 2764, 2769]
---

- **Date:** 2026-11-26 | **Status:** Accepted | **Pages:** [simulation](/architecture/simulation/)
- **Decision:** The lab calendar is derived, never stored. `calendar.Generate(room, knobs, seed, window)`
  is pure (no clock, no global): a room's day is generated whole from the seed, the room key (site,
  building, room) and the UTC day, so a day is the same across runs, engines and windows. The
  scenario declares it in a `calendar:` block, `meetings` as knobs per standard, validated at load.
  An entry has a kind, a window on the simulator's clock, a room scope and a payload of its kind;
  `meeting` is the only kind built, and a scripted problem (#2769) is a second kind from a scripted
  source on the same clock, not a second calendar. Template funcs `meetingNow` and `meetingNext`
  read the device's own room; `roomMeetingNow` and `roomMeetingNext` read a room by key. A service
  that speaks for many rooms (a booking platform pushing per `.Owner` system, a meeting platform
  across a whole account) reads by room key, and resolves its system to a room key itself, because
  the calendar is keyed by room and a system is not a room.
- **Context:** The video bar kept its own schedule from its own draws (ADR-2754.2), so the panel, the
  booking service and the meeting platform could each say something different about one room.
  Storing a calendar would need a table, a sync and a place for it to be wrong; a pure derivation has
  none, and a seed plus the manifest is all a simulator already has. The seed is the room key and not
  #1749's stable device key, which has not landed: meetings belong to a room, and every device in it
  must see the same ones. Whether the knobs and each device's room reach a running simulator beside
  an outpost is a separate seam (the server's device list names neither today); this decision does
  not add it.
