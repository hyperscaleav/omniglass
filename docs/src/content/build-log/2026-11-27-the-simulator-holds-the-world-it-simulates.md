---
title: "The simulator holds the world it simulates, from a world file the lab writes (#2773)"
date: 2026-11-27
refs: [2757, 2773]
---

A scale lab's simulators now know their rooms. The lab resolves the scenario plan to one **world file**
per site outpost (`scenario.SiteWorlds`, a pure function): the site's rooms with their standard and
capacity, each room's planned devices by component name and address, the services with the systems
they speak for, the meeting knobs per standard and a calendar seed taken from the scenario. The seed
one-off writes the files into the data volume, each outpost mounts it read-only and `sim run` loads
its own from `OMNIGLASS_SIM_WORLD`, builds the lab calendar and installs it on the engine. The
simulator joins each pulled device to the world by name and address, which fills its site, building
and room (so `meetingNow` reads its room's meeting and `{room}` expands), and a service resolves a
push's `.Owner` system to a room through the world (`ownerRoom`, `ownerMeetingNow`, `coveredRooms`).
The server, the bus and the schema are unchanged: the simulator is, and Omniglass learns it by
collecting. ADR-2757 records the transport and the join.
