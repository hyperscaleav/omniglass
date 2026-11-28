---
title: "Room control and switching are Go families (#2833)"
date: 2026-11-28
refs: [2833, 2826, 2832]
---

The Newtron control processor, touch panel and scheduling panel and the Meridian switcher and extender are
Go packages under `sim/device/` (`newtroncore`, `newtronpanel`, `newtronslate`, `meridianswitcher`,
`meridianextender`), one per wire API, and their five YAML profiles, templates and the shared `nvp-room`
surface are deleted. The panel's NVP room is the shared `sim/surface/nvp` on all five of its line faces, its
dashboard is `html/template` over a typed view, and the room-following behaviors are `sim/behave`
primitives: the processor's `RoomPower` and `Climate`, the scheduling panel's `RoomBooking`, and the new
`PanelFollowsRoom`, `RouteFollowsRoom`, `SignalOnInput` and `ExtenderSignal`.

What the engine and the shared helpers gained for it: a tick reads its room's booked meetings
(`TickContext.ReadBookings`); a control route takes free text, optionally held to a pattern
(`httpapi.Text`), and writes a value it derives from the request in the same change (`Control.Also`); the
console lists a family's line commands under the interface that serves them (`Command.Face`,
`Surface.ListedOn`), each with its subscription verb; and the wire harness records line protocols, the
lines a device answers and pushes, and an operator's hand on the device. Parity with each profile is the
transcript recorded before it was deleted (ADR-2833 lists the two refusals the families make plainly).
