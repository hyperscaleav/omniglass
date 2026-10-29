---
title: "The simulated UCC platform speaks for many rooms (#1387)"
date: 2026-10-29
refs: [1387, 1282]
---

Slice 3 of the shared-API fan-out (#1282,
[ADR-1282](/architecture/decisions/#adr-1282-a-conduit-fans-each-identified-value-out-to-the-owner-its-binding-names-and-a-binding-costs-a-write-on-both-of-its-ends)).
The simulator gains `ucc-platform`, the meeting platform's account-wide plane: one service whose
room list answers every room's meeting state and participant count, whose meeting routes post a
start and an end naming the room by id, and whose report route posts one batched state naming every
room. A room id the account has not seen joins it, and a request's room id reaches the device's
state only as one path key, through a new `segment` template function.

Two shipped drivers read it. `ucc-rooms` (new) polls the room list as records, each naming its room
by the `ucc-room-id` identity. `ucc-webhook` gains three listeners for the platform's room-naming
posts, each declaring the same identity; its per-room listeners are unchanged, so the lab's per-room
UCC services land on their own component as before (their retirement is #1390). The dev fleet
carries the platform at headquarters, polled and subscribed, and binds four of its rooms to their
systems; the fifth room is left unbound for the inbox to show.
