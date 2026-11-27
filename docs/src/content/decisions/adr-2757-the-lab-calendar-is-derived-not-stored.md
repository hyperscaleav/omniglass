---
title: "The lab calendar is derived, not stored, and a scripted source feeds it problems"
date: 2026-11-26
status: Accepted
summary: "A lab's meetings are one calendar every simulated room acts out, a pure function of the scenario's calendar block, a seed and a window, so every simulator and the server compute the same one with nothing stored or shipped; services that span rooms read it by room key; a scripted problem source joins it on the same clock as a second entry kind."
refs: [2757, 2764, 2769, 2773]
---

- **Date:** 2026-11-26 | **Status:** Accepted | **Pages:** [simulation](/architecture/simulation/)
- **Decision:** The lab calendar is derived, never stored. `calendar.Generate(room, knobs, seed, window)`
  is pure (no clock, no global): a room's day is generated whole from the seed, the room key (site,
  building, room) and the UTC day, so a day is the same across runs, engines and windows. The
  scenario declares it in a `calendar:` block, `meetings` as knobs per standard, validated at load.
  An entry has a kind, a window on the simulator's clock, a room scope and a payload of its kind;
  `meeting` is the only kind built, and a scripted problem (#2769) is a second kind from a scripted
  source on the same clock, not a second calendar. Template funcs `meetingNow` and `meetingNext`
  read the device's own room; `roomMeetingNow` and `roomMeetingNext` read a room by key. A service
  that speaks for many rooms (a booking platform pushing per `.Owner` system, a meeting platform
  across a whole account) reads by room key, and resolves its system to a room key itself, because
  the calendar is keyed by room and a system is not a room.
- **Context:** The video bar kept its own schedule from its own draws (ADR-2754.2), so the panel, the
  booking service and the meeting platform could each say something different about one room.
  Storing a calendar would need a table, a sync and a place for it to be wrong; a pure derivation has
  none, and a seed plus the manifest is all a simulator already has. The seed is the room key and not
  #1749's stable device key, which has not landed: meetings belong to a room, and every device in it
  must see the same ones.
- **Amended (#2773): the world is the simulator's own.** The sim is; Omniglass learns. The simulator
  holds the world it simulates (`sim/world`: sites, rooms with standard and capacity, each room's
  planned devices, the services and the rooms they cover, the meeting knobs per standard, the calendar
  seed, versioned) and never reads it from the server or its database, so the server's device list
  carries no location. The lab resolves the plan to one world file per simulator outpost (a pure
  `plan -> world`, sized to the outpost's site) and the simulator loads it.
  - **Transport.** The seed one-off writes `world/<outpost>.json` into the data volume it already
    mounts; each simulator outpost mounts that volume read-only and finds its file at the path in
    `OMNIGLASS_SIM_WORLD`, as the lab already hands it its pushers. No new volume, host path or
    server route. A lab with no plan writes no file and its simulator has no calendar and no rooms.
  - **The join: the world names, the seed obeys.** The pull still says which devices to serve
    (ADR-0142, unchanged). The world gives every planned device a name of its own, unique across the
    plan and room-qualified, `<system>-<slot>-<n>` (`hq-mr86-1-main-display-1`), and the lab's scale
    seed onboards each component under exactly that name instead of asking the platform's generator
    for one. The simulator then places a pulled device by an exact lookup of its component name; the
    world does not replay Omniglass's naming, and no address is predicted. A test seeds a plan and
    holds every seeded name and room to the world's. A pulled device the world does not name keeps
    empty room fields. This changes a scale lab's component names (they were `display-1` in every
    room); the storyline, the omnibot recording and the marketing shots that named components follow.
  - **Services.** A service resolves a push's `.Owner` system to a room through the world
    (`ownerRoom`, `ownerMeetingNow`, `ownerMeetingNext`) and reads the rooms it covers
    (`coveredRooms`); Omniglass is not asked. A grant names its systems by the server's uuid, which the
    simulator does not hold, so a pusher the world covers pushes for the system names the world gives it
    (`worldSystems`, #2765) and the server resolves each inside the pusher's grant; with no world it
    pushes for its grant, as before.
  - **Polled services.** A scenario's `services:` block places a polled cloud service once for the
    whole scenario (#2765), at the headquarters: a component the lab seeds with the product's own
    driver, covering every system of every site, so its world holds the other sites' rooms (with no
    devices) too. The service is a conduit (ADR-1282): the lab's seed binds each covered system's name
    as a room id, as an installer would, and the simulator names its rooms by the same ids from its
    world.
  - **The meeting platform** (#2790) is a polled service and a webhook sender at once, one account for
    the whole scenario placed through `services:` like the scheduling service. The world owns the ids
    of what it lists: a room is its system's name, a device has the stable id `scenario.DeviceID` gives
    its name, with the kind a meeting platform lists it as and the firmware it reports. The lab's seed
    binds each room id to its system and each device id to its component; the platform's device list is
    a second poll of the same driver, since an endpoint is named for its transport and a component holds one.
  - **Deferred.** The inversion of ADR-0142, where the simulator decides which devices exist and
    Omniglass discovers them, is a separate definition; the world file is the seam it would grow from.
- **Amended (#2769): scripted problems.** The second kind is built. A scenario's `calendar.problems` scripts
  recurring problems (a cadence, a UTC time and duration, a scope resolved from the plan and the world, and
  the role of the room it lands on, never a product), derived like the meetings from the world and the seed:
  only an optional `spread` is drawn, a seeded whole-minute offset inside the cadence. `Entries`, `Now`, `Next`
  and `Prev` stay the meetings alone so no reader of a meeting meets another kind; `Problems`,
  `ProblemsNow` and `All` read the rest. One executor in the simulator applies a problem's impairment as its
  window opens and clears it as it closes: a pin through the behavior-override seam (put back as it was), and
  unplugged or packet loss through the netns impairer, merged with the server's own impairment and refused
  with a log line without NET_ADMIN. Omniglass is told nothing; the meeting platform's devices list reads a
  device offline from the calendar while a problem has it unplugged, not from the dark device.
