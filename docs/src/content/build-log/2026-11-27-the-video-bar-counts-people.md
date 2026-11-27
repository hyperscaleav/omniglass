---
title: "The video bar counts the people in its room (#2757)"
date: 2026-11-27
refs: [2757, 2808]
---

A huddle room has a video bar and no separate people counter, so its `occupancy-count` read "No data in
range" while the conference rooms, which have a counter, filled. The Kestrel VRoom's camera now counts the
room, as a real bar does: in the simulator it follows the room phase (the calendar's attendees in a held
meeting or a walk-in, one fewer now and then, and 0 in a no-show and outside any meeting), and a bar with
no calendar counts only the call it is in. The shipped driver collects it as the existing `people-count`
metric, so the one `room-occupancy` rule the plan already seeds fills occupancy in every room with a bar.
A huddle room's temperature stays empty: nothing in it is the source of one yet.
