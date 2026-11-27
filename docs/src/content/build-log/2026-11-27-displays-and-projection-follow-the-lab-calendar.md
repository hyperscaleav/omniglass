---
title: "Displays, projectors and signage follow the lab calendar (#2768)"
date: 2026-11-27
refs: [2757, 2768]
---

The Boreal Edge display and Beam projector now keep their room's meetings: they wake onto the conferencing input
two minutes before a booked meeting someone will attend (at the start of a walk-in), and go to standby five
minutes after it ends; a no-show changes nothing. The projector's light source ages only while it is on, so its
hours grow only around held meetings. The Cast signage player follows the building's day: a new generic template
function, `buildingHours`, reads the hours the building's rooms declare for their meetings (the widest span in the world's knobs, so a signage room that holds no meetings still has its building's day), and the
player plays inside them and stops outside. Every reading derives from the calendar through the device's clock;
nothing comes from Omniglass.
