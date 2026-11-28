---
title: "Occupancy, power and the cloud services are Go families (#2836)"
date: 2026-11-28
refs: [2836, 2826, 2832, 2833]
---

The Tessellate people counter, the Ferrite PDU, the generic SNMP device and the account-wide UCC platform,
scheduling, booking and recording services are Go packages under `sim/device/` (`tessellatecensus`,
`ferritepdu`, `genericdevice`, `uccplatform`, `schedulingservice`, `bookingservice`, `recordingservice`), one
per wire API, and their seven YAML profiles and templates are deleted. Each is a family of one, named for its
product.

What the engine and the shared helpers gained for it: a family reads the rooms it speaks for (`spec.RoomsOf`:
the world's covers, a room's phase, meeting and bookings, the devices a platform lists), sends signed webhooks
(`spec.Send`) and names an event on its stream (`spec.Report`); a family declares a push face (`Spec.Push`) and
a webhook face (`Spec.Webhook`); a list pages by the names its API declares (`httpapi.Paging`, `Cut`,
`PagedDocument`); the engine's refusals are exported (`httpapi.Refuse`, `MutationFailed`); a family may be named
for its only product; and the wire harness records a line-protocol conversation, an event stream and the
webhook posts a device makes. Parity with each profile is the transcript recorded before it was deleted
(ADR-2836 lists what differs).
