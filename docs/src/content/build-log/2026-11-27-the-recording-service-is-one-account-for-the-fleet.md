---
title: "The recording service is one account-wide cloud component (#2757)"
date: 2026-11-27
refs: [2757, 2775]
---

The recording service stopped being a slot in every conference room. Like the scheduling and meeting
platforms it is one cloud component for the whole fleet: the corporate manifest's `services:` block
places it once, at the headquarters, covering every system, and `divisible-conference` lost its
`recorder` slot (and the dev fleet its two staffed ones). Its simulated API gained a token-paged
`GET /api/recordings`, one record per covered room named by the system, each with that room's
recordings in progress and queue depth off the room phase; the account's own counters stay the sum of
its rooms, and a service with no calendar lists no room and drifts as before. The
`recording-service-api` driver walks the list with `request.paginate` (ten rooms a page) and fans each
record's `active-recording-count` and `queue-depth` out to the system the lab's seed bound the room id
to (`recording-room-id`). The lab seed binds the ids with the same code that binds the scheduling
platform's. At 1,000 systems the plan is 3,124 components (3,136 before); at 3,000, 9,166 (9,190).

Two dev-fleet fixes rode along. Boardroom A seeded two power conditioners, one filling the power slot
and one a member filling none, so its page read "1 component is not in a slot" beside a staffed power
slot; it has one now, in the slot, and the membership-without-a-slot case is held by the recording
service and the panels and spare mic that really do fill no slot. A component shared by several systems
could render with no name on a system page: the also-in chip held its full width while the name
truncated, so a long list of systems squeezed the name to nothing. The chip now shrinks first and the
name keeps a floor, as the outline's does.
