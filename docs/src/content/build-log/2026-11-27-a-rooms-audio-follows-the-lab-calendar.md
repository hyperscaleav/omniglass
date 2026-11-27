---
title: "A room's audio chain shows its meeting: microphones, the DSP and the amplifier (#2774)"
date: 2026-11-27
refs: [2774, 2757, 2764, 2773]
---

The Lyra Arc A2 and HX-1 microphones, the Polaris DSP 16 and the Amp 4-100 now read their room's meeting off
the lab calendar, through the device clock, instead of drifting on their own. The sim is; Omniglass learns:
nothing is asked of the server. The array is active (level up, mute moving now and then) while someone is
in the room and idle otherwise, a no-show included. The HX-1 transmits in the same hours, and its battery is
computed from the calendar alone over the 48 hours before now, draining in use (0.4 percent a minute) and
recharging between meetings (0.8 percent a minute), so a pack reads the same charge at any instant on any
run and a scheduled problem can pin the path. The DSP opens a gate per two attendees actually in the room
(to four strips) and raises their peak with the count; the amplifier carries signal and warms toward a
loaded temperature in use. One generic template func, `meetingsBetween`, reads a room's meetings over a
window. A device with no calendar now reads an empty room: the mics at their floor, the DSP's gates shut,
the amplifier silent.
