---
title: Why Omniglass
description: "What Omniglass is, what it is for, and why it exists when Zabbix and Prometheus already do monitoring."
---

It is 9:58 on a Tuesday. A board meeting starts in two minutes. The executive walks in to a
black display, a video bar with no touch panel, and a call that will not connect. Somewhere,
the AV professional responsible for that room is about to find out the hard way, when the
phone rings.

That is the problem Omniglass exists to solve. **Make an AV system as observable and operable as
any cloud service**, so the answer to "is the room working?" arrives before the meeting, not
after the complaint.

## Start with the question

Every monitoring tool answers a question, and the question gives away what it was built for.

Zabbix and Prometheus answer **"is the host up?"** They model a fleet of servers: a *host*, and
the *metrics* it emits. That is exactly the right shape for a data center, and they are
the best in the world at it.

Omniglass answers a different question: **"is the room working?"**

**A room is not a host.**

```mermaid
flowchart TD
  R["Boardroom A<br/><b>a system</b> · is it working?"]
  R --> D["Display"]
  R --> VB["Video bar"]
  R --> TP["Touch panel"]
  R --> DSP["DSP"]
  R --> CT["Control processor"]
  R --> UC["UCC service (cloud)"]
  R --> SC["Scheduling service"]
  R --> NW["Network"]
  R --> EN["Environment"]
  classDef sys fill:#21CAB9,stroke:#080c16,color:#080c16;
  class R sys;
```

A room is a **system**: a display, a video bar, a touch panel, a DSP, a control processor, a
cloud UCC service, a scheduling service, the network it rides on, the environment around it.
Together they have exactly one job: let people meet. The display being "up" tells you almost
nothing. The *room* working is a fact about all of them, at once.

This is the line the discipline draws: **observable devices are not the same as observable
systems.** You can have a perfectly instrumented display and still have no idea whether the
room is usable.

## Why the tools we were handed do not fit

Give an AV pro a host-and-metric tool and ask them to monitor two hundred meeting rooms, and
watch what happens. They bend it. They bolt on a separate integration tool to reach the gear
the platform cannot speak to. They script a fake "room." They graft on a separate service-tree subsystem to get
an SLA, a parallel feature instead of one composed from the core model. They keep inventory in a spreadsheet. It is duct tape, and
duct tape does not survive contact with two hundred rooms.

The IT tools are not bad. They are aimed at a different target, and pointed at a room they miss
everything that matters:

- a **room** and a **system**, not just a host;
- a **redundant** microphone whose failure should *degrade* the room, not down it;
- an **AV control protocol** that sends plain command strings over a TCP port or serial line,
  every vendor with its own dialect, no SNMP, no agent, no API;
- a **UCC call state**, a **codec input**, a **DSP channel**, as first-class signals, not raw
  vendor strings we had to scrape and guess at;
- a **desired configuration** that should be enforced when the world drifts from it.

None of that is a metric. So none of it fits.

## How Omniglass is built differently

Omniglass starts from the discipline, not from a metrics database. Every part of the
architecture follows from one question: "is the room working?"

### The estate is the model

A component, a system, a location: a real tree, the way an AV estate actually nests. The room
*is* a system. The building *is* a location. Health, alarms, and config attach to any level,
not only to a device. The host-and-metric world cannot say "this room," so it cannot reason about
one. [The taxonomy](/architecture/taxonomy/) is built so it can.

### One canonical signal

Every vendor's reading normalizes onto one canonical signal name from a governed
[registry](/architecture/taxonomy/): `power.state`, `audio.level`, `call.state`. A Sony display
and a Samsung display answer the same question the same way, because the *measurement* is named,
not the device. That single
canonical path is what makes a cross-fleet dashboard, a real SLA, and useful AI possible at all;
a pile of vendor-specific strings makes all three impossible.

### Collection that speaks AV's weird wire

Reaching AV gear is the hard part, and it is where the IT tools tap out. Omniglass collects
through a **[flow engine](/architecture/collection/)**: SNMP, HTTP, SSH, and the raw-socket AV
control planes, parsed where it is collected, close to the gear, and normalized into the
canonical signal on the way in. No middleware glue, no scripts in a second tool. The thing that talks to a Crestron
processor and the thing that talks to a switch are the same engine.

```mermaid
flowchart LR
  G["AV gear<br/>SNMP · HTTP · SSH · raw AV control"] -->|"flow engine<br/>parse at the edge"| DP["datapoint<br/>one canonical signal"]
  DP -->|"event_rule fires"| EV["event"] -->|"fire opens / clear resolves"| AL["alarm<br/>room degraded"]
  AL --> AC["action<br/>notify · remediate · open ticket"]
  V["variable<br/>declared config: input = HDMI1"] -. "drift?" .- DP
  classDef k fill:#21CAB9,stroke:#080c16,color:#080c16;
  class DP k;
```

### Config is a first-class thing, with drift

What a device *should* be is an operator decision: this codec should be on HDMI1, this DSP at
this gain. Omniglass holds that as a **[variable](/architecture/variables/)**, a declared value
that can be compared against the observed reality. When they disagree, that is **drift**, and
drift is a signal you can alarm on or a fix you can push. The IT tools have nowhere to put
"what it should be," so they cannot tell you when the world walked away from it.

### Health is the headline

Signals are not the point. **Health is.** Omniglass turns the canonical signals into the one
answer that matters, and it rolls up the tree.

```mermaid
flowchart BT
  C1["Display: up"] --> S["Boardroom A<br/><b>degraded</b>"]
  C2["Video bar: not in call"] --> S
  C3["Backup mic: down<br/>(redundant)"] --> S
  S --> L["Floor 3<br/>1 room degraded"]
  classDef d fill:#f0b429,stroke:#080c16,color:#080c16;
  class S d;
```

[Health](/architecture/health/) is just a computed datapoint, owned by the system, reduced from
its members, and it is **role-aware**: a *required* member down takes the room down; a
*redundant* one only degrades it (the room goes down only if every redundant peer is down); an
*informational* one does not touch it. That is the
difference between "a thing is red" and "the room is in trouble." And it is what makes a real uptime SLA possible at all.

A room's health is a fact you can read at a glance:

| Member role | Example problem | Effect on the room |
|---|---|---|
| required | Display powered off while in use | down |
| required | Video bar not connected to UCC | down |
| redundant | Backup mic unreachable (a peer still holds) | degraded |
| informational | Room temperature high | noted, room stays up |

### And then it acts

Seeing is half of it. Omniglass also **acts**: notify the right person, run
remediate-verify-escalate (send the command, wait, re-check the real datapoint, escalate if it
did not take), open and close the ticket as the alarm opens and clears (and, on the reserved
self-healing path, converge a drifted device back to its declared config). [Alarms and actions](/architecture/alarms-actions/) are one
model, composed, not a separate workflow engine bolted on.

## What Omniglass is

Omniglass is an **open observability and control plane for AV and IT estates**. One install
over a database you already know how to run: point it at your fleet and operate. It is fully open
source and has been public from day one, so you can read every line, run it yourself, and never
get locked into a vendor. (A single self-contained server on standard PostgreSQL; AGPLv3.)

It is also a **learning tool**. The same binary serves an interactive docs and concept site, so
the tool that runs your estate also teaches the discipline it implements, against real or
simulated data. The Measure and Instrument layers are concrete, explorable artifacts, not
blueprints in a PDF.

## The difference, in one line

We did not build Omniglass because the world needed another monitoring tool. We built it because
the people who keep rooms working deserve to *see* them, as systems, not as a pile of hosts,
and to act before the phone ever rings.

That one difference is the whole architecture.

See how it is built. Start with the [architecture overview](/architecture/), then the
[taxonomy](/architecture/taxonomy/).
