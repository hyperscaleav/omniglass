---
title: "The Newtron processor and scheduling panel are simulated and collected (#896)"
date: 2026-10-29
refs: [896, 1266]
---

The dev fleet's three Newtron Core CP-3 control processors and twenty-eight Slate 5 scheduling panels
had no simulator profile of their own; they had briefly shared the touch panel's, which the architect
ruled out. Each now has one, and a driver of its own in place of the panel's `newtron-nvp`.

A touch panel's surface is a subset of a control processor's, so the issue asked that the two share
it rather than copy it, and left open how. The catalog gains a small primitive for it
([ADR-0896](/architecture/decisions/#adr-0896-a-device-profile-extends-another-whose-surface-it-carries)):
a profile may `extends` another, carrying its whole surface and adding to it. The Core CP-3 extends
the Panel 7, so it answers every NVP verb, route and face the panel does, and adds the processor's
own: its control program, running or stopped (polled, pushed on `SUBSCRIBE PROGRAM`, started and
stopped over HTTP), the panels connected to it, and its control ports, all on an enterprise SNMP branch
(`.55510.10`) the panel's MIB-2-only agent still withholds. A signal the panel gains later, the
processor answers too.

The Slate 5 is not a touch panel and shares nothing with it: its NVP face answers `GET BOOKING`,
`GET NEXT` and pushes on `SUBSCRIBE BOOKING`, its HTTPS buttons book the space now or end the meeting,
and a behavior turns the space over every half hour, a stand-in for the place's day that a scenario
timeline can replace.

`newtron-core-nvp` reads what `newtron-nvp` reads plus `program-state` and `panel-count`, both new to
the boot seed; `newtron-slate-nvp` reads `booking-state`, the property the lab's booking service
already pushes for a system, now observed on a component too. Drivers have no composition of their
own, so the processor's driver repeats the panel driver's polls and listeners.
