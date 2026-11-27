---
title: "The meeting platform serves paged rooms and devices that fan out to systems and components (#2790)"
date: 2026-11-27
refs: [2757, 2790, 2789]
---

The simulated meeting platform now reads like a real cloud room-and-device platform, and a lab is a fleet
of AV systems Omniglass learns the way a customer's Omniglass would. It is one account for the whole
fleet: the scenario manifest places it once, at the headquarters, through the `services:` block, covering
every room of every site (it leaves the per-site `pushers:` list, and a profile with a webhook face is a
signed-webhook sender as well as a polled service).

`GET /api/rooms` is token-paged and carries each room's id (its system's name), status (`available`,
`in-meeting`, `offline`), the meeting it holds (topic, start, participants), the booking that holds it now
and the next booked one, all off the lab calendar; a booked no-show reads available with its booking. A new
`GET /api/devices` lists every device in those rooms that a meeting platform manages (codec, bar,
controller, scheduling panel, camera, microphone) with the world's stable device id, its room, type, status,
firmware and last-seen time. The world now owns device ids as it owns names (`scenario.DeviceID`, a pure
function of the name) and carries each device's platform type and the firmware its profile reports.
Webhooks add participant joined and left and device status changed to the meeting boundaries, each naming
its room id and, where it applies, the device id.

The `ucc-rooms` driver declares `request.paginate` for both lists and gains a second poll for the devices
(an endpoint is named for its transport, so one component holds one `https` endpoint and the two lists are
one driver's polls); `ucc-webhook` routes the new posts by room id or device id. The lab seed binds each
room id to its system and each device id to its component as an installer would, and a room or a device
nobody bound lands in the conduit's unbound inbox. The scheduling platform's driver declares its paging
rule too, which retires its single-page walk. Two new property types carry it: `ucc-device-id` (the match
key) and `platform-state` (online, offline).

A device reads offline while a scripted problem has it unplugged (#2769), derived from the calendar.
