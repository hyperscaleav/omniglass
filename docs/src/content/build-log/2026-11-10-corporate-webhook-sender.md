---
title: "The corporate scale lab carries a signed webhook sender per site (#1747)"
date: 2026-11-10
refs: [1747, 1743, 1370, 1282, 1623]
---

The scale lab's push lane now carries webhook receipts as well as bookings. The corporate scenario
declares one `ucc-platform` meeting platform per site in its `pushers` list, with no `systems`, so it
speaks for every system of its site. The seed reads the platform's webhook face (`driver: ucc-webhook`,
`provision: /api/rooms`) and the driver spec, and seeds the platform as a component with a
`ucc-webhook` endpoint, a signing key minted for that seed alone and sealed in the server, and a
`ucc-room-id` binding per system. The simulator beside the site's node reads the key and the bound
rooms through the device list, subscribes, adds each room, and the platform's new `rooms_report`
behavior posts every room's state, signed, every 30 seconds. `omniglass lab seed` reports the senders
it seeded and never the key. A measured corporate lab reads the platform's posts on the push lane at
loss 0, the five demo rooms the profile ships with arriving unbound as out, not lost, until #1756 retires them. The decision is
[ADR-1747](/architecture/decisions/#adr-1747-a-webhook-sender-is-a-component-whose-signing-key-the-seed-mints).
