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
