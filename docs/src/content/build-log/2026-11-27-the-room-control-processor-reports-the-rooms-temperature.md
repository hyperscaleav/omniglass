---
title: "The room's control processor reports the room's temperature (#2757)"
date: 2026-11-27
refs: [2757, 2808]
---

A corporate room's `room-temperature`, a metric its standard declares, read "No data in range": no
simulated device reported it, and the dev fleet's chart is samples the seed writes. The Newtron control
processor now reads the room's temperature, as a real control processor polls a room sensor. In the
simulator it follows the room phase: the room warms with the people in a held meeting toward the setpoint
plus a load per attendee and settles back between meetings and overnight, deterministic for a calendar and
a clock, with small noise; with no calendar it reads the setpoint with the same noise. The processor answers
`GET ROOMTEMP`, its shipped driver collects it as a new `room-sensor-temperature` metric on the component,
and the plan seeds a second calc rule, `room-temperature`, which carries it up to the system the same way
`room-occupancy` carries the people counter's count (a rule cannot read the key it writes, hence the two
types). The dev fleet keeps its synthetic day: it is a shaped history the chart can be judged on, written
at once with no simulator running, which a steady live setpoint cannot give.
