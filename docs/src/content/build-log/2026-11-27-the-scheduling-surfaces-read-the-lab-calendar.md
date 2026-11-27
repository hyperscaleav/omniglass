---
title: "The booking service, scheduling service and room panel read the lab calendar (#2765)"
date: 2026-11-27
refs: [2757, 2765]
---

Bookings, as every scheduling surface reports them, now come from the lab calendar's booked
entries, not from fixed lists. A new pure reading, `RoomBookingsAt`, and three template funcs over it
(`bookings`, `roomBookings`, `ownerBookings`) say what a room's booked meetings are on the device's
clock: a no-show holds its slot and reads booked, a walk-in is nobody's booking and reads free. The
booking service pushes `booking-count` and `booking-state` from it for the room the world puts each
system in (by the names the world gives it), and a system with no calendar keeps the fixed meeting day it always had.
The scheduling service counts today's, remaining and in-progress bookings across the rooms it covers,
and the Slate 5 panel shows its room's booked or free state with `GET CURRENT` and `GET NEXT`.

The scheduling service is one account for the whole fleet, a conduit: a new `services:` block in the
scenario manifest places it once, at the headquarters, covering every system, and a new
`scheduling-service-api` driver polls its token-paged `GET /api/rooms` (one record per room, named by
the system) and fans each room's `booking-state` and `booking-count` out to the system the lab's seed
bound that room id to (`sched-room-id`). A room nobody bound lands in the conduit's unbound inbox. A
pusher pushes for the system names the world gives it, since a grant carries only the server's ids.
The driver reads one page for now (TODO(#2789)).
