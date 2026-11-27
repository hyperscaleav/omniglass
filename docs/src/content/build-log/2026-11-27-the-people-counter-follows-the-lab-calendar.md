---
title: "The people counter's occupancy follows the lab calendar (#2766)"
date: 2026-11-27
refs: [2766, 2757]
---

The Tessellate Census C1 no longer walks its head-count at random: its occupancy behavior reads its own
room's meeting off the lab calendar (`meetingNow`, through the device's clock), so the count is a
meeting's actual attendees (a walk-in's too, over the room's capacity when the payload says so), zero in
a no-show and in an empty room, and one fewer on about one tick in seven in a meeting of two or more.
The increased, decreased and steady signal and the vacant flag derive from the count as before. With no
calendar (the dev fleet holds no world) the counter keeps its old walk. The corporate lab's divisible conference rooms gained a people-counter
slot, filled with a Census C1, and a projection slot, filled with a Beam 340 projector (the seeded boardrooms staff both), and a `make test-lab` test holds a real counter's raw-TCP count to its
room's calendar.
