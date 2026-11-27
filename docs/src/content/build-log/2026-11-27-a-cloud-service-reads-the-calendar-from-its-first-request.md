---
title: "A cloud service reads the lab calendar from its first request (#2757)"
date: 2026-11-27
refs: [2757, 2790, 2765]
---

The meeting platform, scheduling service and recording service derive each covered room's state from
the lab calendar in a timed behavior, and a behavior's first tick came a phase offset plus a whole
interval after the device started (up to 30 s, 60 s and 120 s). A collector that polled at start read
every room as idle while the room's own devices were already in the meeting; a corporate lab started
in meeting hours fanned `idle` to a room that was mid-meeting. The cause was the cold start, not the
world: a branch room in the headquarters' world and in its own site's world read the same calendar,
which a new scenario test now holds.

A behavior may now declare `schedule.on_start: true` and ticks once as the device starts serving,
before it answers anything. The three services set it on the behavior that reads the calendar. The
meeting platform lab test no longer depends on the hour: it offsets the simulator's clock into a held
meeting of a judged room, so the in-meeting fan-out is always exercised.
