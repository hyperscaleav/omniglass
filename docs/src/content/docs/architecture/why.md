---
title: Why Omniglass
description: "What Omniglass is, what it is for, and why AV needs its own observability platform instead of an IT monitoring tool."
---

Run AV at scale and you know the feeling. You find out a room is broken when someone walks out
of it. The operation runs on tribal knowledge and last-known-good guesses, and when a high-profile
meeting goes wrong you are in the postmortem with no data.

**That is not a reflection of you or your team.** AV is structurally hard to see, and the industry
never shipped a right way to do it. Omniglass exists to ship one.

## AV is not built to be observed

Most AV gear is built to be **controlled, not monitored**. You can tell a device what to do; asking
what it is *doing* is a problem the device was not designed to answer. At a handful of rooms you can
brute-force visibility with grit. At a thousand rooms, it collapses.

The reasons are structural:

- **It is agentless.** AV gear is firmware appliances: you ask the device from the outside and take
  whatever it is willing, or able, to give you.
- **There is no standard, and the APIs are uneven.** Control interfaces are usually decent;
  *management* data is an afterthought, when it exists at all. Different port, protocol, and format
  for every vendor, every product, sometimes every firmware revision. Every integration is bespoke.
- **The system is the hard part.** A room is not a device. It is a signal chain (a display, a video
  bar, the microphones, a DSP, a control processor, the UCC service in the cloud, the network) and
  "healthy" is a fact about the whole chain. Two of those mics might be redundant. Every unique
  combination of gear is its own health model.
- **It is fragmented by design.** Each manufacturer portal sees only its own devices: a dozen panes
  of glass and still no single view of the room.

## Why an IT monitoring tool does not finish the job

The IT monitoring world (Zabbix, Prometheus, and the rest) is genuinely excellent at what it was
built for: a fleet of servers, an agent on each one, clean and standardized metrics. But the
host-and-metric model quietly assumes the three things AV does not have: **an agent to install, a
standard API to read, and a host that is the thing you actually care about.** Point it at a room
and it has no idea what a "room" is, no language for an AV control protocol, no concept of a
redundant mic. It can tell you a host is up, not that the room is usable.

You *can* bend these tools to AV. Skilled people do it every day, scraping web interfaces and
gluing middleware on the side to reach the gear the platform cannot. But that is doing the
platform's job for it, by hand, forever, and it still has no model of the room at the end.

## It is an architecture problem, not a tooling problem

The fix is a method, not a better dashboard: figure out **why** you monitor, then **what**
(model what "healthy" means), then **how** (go get the data, however the device will give it). That
is the [AV Observability Framework](https://hyperscaleav.com/framework), its keystone the
**health model**, which answers one question:

> Is this room usable right now?

The health model always runs; the only question is whether it runs *as a system* against real
signal, or in the operator's head at 3am against half of it. Omniglass runs it as a system.

## What Omniglass is

Omniglass is an **open, self-hosted observability and control plane for AV (and IT) estates**. It
does three things an IT tool cannot, designed in from the start rather than bolted on.

**It meets the devices where they are.** Agentless and protocol-diverse, it gets the data however
the device will give it (SNMP, HTTP, SSH, a control processor's raw command dialect) and normalizes
every vendor's reading into one canonical signal, so a Sony display and a Samsung display answer
the same question the same way.

**It models your estate the way it actually nests.** Components, systems, rooms, buildings. The
room is a first-class system, not a tag, so health, alarms, and config attach at the level you
operate.

**It runs the health model.** Signals roll up the tree into "is the room working," and the rollup is
role-aware: a *required* display down takes the room down, a *redundant* mic only degrades it, an
*informational* sensor does not touch it. That turns a wall of red dots into one honest answer, and
makes a real uptime SLA possible.

```d2
direction: up
classes: { node: { style.border-radius: 8 }; warn: { style: { border-radius: 8; bold: true } } }
c1: "Display: up" { class: node }
c2: "Video bar: not in call" { class: node }
c3: "Backup mic: down\n(redundant)" { class: node }
system: "Boardroom A\ndegraded" { class: warn }
floor: "Floor 3\n1 room degraded" { class: node }
c1 -> system
c2 -> system
c3 -> system
system -> floor
```

And then it acts: notify the right person, run remediate-verify-escalate, open and close the
ticket as the alarm opens and clears.

Open source, self-hosted, vendor-agnostic, one server over a database you already know how to run,
and free.

## The architecture, as one journey

Every monitoring system is the same shape: **collect, evaluate, raise an event, hold it as an alarm,
act, and see it the whole time.** Omniglass is that shape, built AV-native.

```d2
direction: right
classes: { node: { style.border-radius: 8 }; key: { style: { border-radius: 8; bold: true } } }
gear: "AV gear\nSNMP · HTTP · SSH · raw AV control" { class: node }
sample: "sample\none canonical signal" { class: key }
event: event { class: node }
alarm: "alarm\nroom degraded" { class: node }
act: "notify · remediate · ticket" { class: node }
config: "config\ndesired: input = HDMI1" { class: node }
gear -> sample: collect: parse at the edge
sample -> event: evaluate: event_rule
event -> alarm: fire opens / clear resolves
alarm -> act: act
config -- sample: "drift?" { style.stroke-dash: 4 }
```

Each stop is a page:

1. **[Collection](/architecture/collection/)** gets the data from gear that never wanted to give
   it, and parses it at the edge.
2. **[Properties](/architecture/properties/)** type every reading into one owned, canonical signal,
   the same measurement across vendors.
3. **[Config](/architecture/variables/)** holds what a device *should* be, so drift becomes
   a signal you can see and a fix you can push.
4. **[Health](/architecture/health/)** rolls the signals up the system tree into the one answer that
   matters.
5. **[Alarms and actions](/architecture/alarms-actions/)** detect a condition, hold it until it
   resolves, and respond.

The [overview](/architecture/) is the map of the whole journey.

## The point

Omniglass exists so the people who keep rooms working can finally **know their systems**: see them
as systems, not a pile of hosts, and act before the 3am call. An IT tool answers "is the host up?"
Omniglass answers "is the room working?"
