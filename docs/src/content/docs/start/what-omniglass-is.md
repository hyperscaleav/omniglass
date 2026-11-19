---
title: What Omniglass is
description: "The problem Omniglass solves, why AV needs its own observability platform, and what Omniglass does about it."
sidebar:
  order: 1
  badge:
    text: Partial
    variant: note
---

Run AV at scale and you know the feeling. You find out a room is broken when someone walks out of
it. The operation runs on tribal knowledge and last-known-good guesses, and when a high-profile
meeting goes wrong you are in the postmortem with no data.

That is not a reflection of you or your team. AV is structurally hard to see, and the industry never
shipped a right way to do it. Omniglass exists to ship one.

## AV is not built to be observed

Most AV gear is built to be **controlled, not monitored**. You can tell a device what to do; asking
what it is *doing* is a question the device was not designed to answer. With a handful of rooms you
can brute-force visibility with grit. With a thousand, it collapses. The reasons are structural:

- **There is no agent.** AV gear is firmware appliances: you ask the device from the outside and take
  whatever it is willing, or able, to give you.
- **There is no standard API.** Control APIs are usually decent; management data is an
  afterthought, when it exists at all. A different port, protocol and format for every vendor, every
  product, sometimes every firmware revision.
- **The system is the hard part.** A meeting space is not a device. It is a chain (a display, a video
  bar, microphones, a DSP, a control processor, a cloud meeting service, the network) and "working"
  is a fact about the whole chain. Two of those microphones might be redundant.
- **Every vendor sees only its own.** Each manufacturer's portal shows its own devices: a dozen panes
  of glass and still no single view of the space.

## Why an IT monitoring tool does not finish the job

IT monitoring tools are excellent at what they were built for: servers, an agent on each, clean
standardized metrics. That model quietly assumes three things AV does not have: an agent to install,
a standard API to read, and a host that is the thing you care about. Point one at a meeting space and
it can tell you a host is up, not that the space is usable.

Skilled people bend these tools to AV every day, scraping web pages and gluing middleware on the side.
That is doing the platform's job by hand, forever, and it still ends with no model of the space.

## What Omniglass does

Omniglass is an **open, self-hosted observability and control plane for AV and IT fleets**: one Go
server over a PostgreSQL database you run, with a console, an API and a CLI over the same data.

**It meets devices where they are.** Agentless and protocol-diverse, an [outpost](/reference/glossary/)
near the gear collects over whatever the device speaks (SNMP, HTTP, SSH, telnet, a control
processor's own dialect) and a driver normalizes each vendor's reading into one canonical signal, so
two displays from two vendors answer the same question the same way.

**It models the fleet the way it nests.** Components (the devices) work together as **systems**, and
systems sit in **locations** nested to any depth. The system is first-class, not a tag,
so health, alarms and values attach at the level you operate.

**It runs the health model.** Signals roll up into one answer per system, and the rollup knows the
system's design: a required display down takes the system out, a redundant microphone only degrades
it, an informational sensor does not touch it. A location takes the worst of what is in it.

```d2
direction: up
classes: { node: { style.border-radius: 8 }; warn: { style: { border-radius: 8; bold: true } } }
c1: "Display: up" { class: node }
c2: "Video bar: up" { class: node }
c3: "Backup mic: down\n(redundant)" { class: node }
system: "Boardroom system\ndegraded" { class: warn }
floor: "Floor 3\ndegraded" { class: node }
c1 -> system
c2 -> system
c3 -> system
system -> floor
```

**It acts.** A rule watching a signal raises an alarm, and an action rule sends the notification to
the people who should know.

:::design[Target design, tracked in #1344]
Beyond notifying, an alarm runs a flow: remediate, wait, check again, and escalate only when the fix
did not hold.
:::

It is open source, vendor-agnostic and free. [The model](/start/the-model/) is the next page: the
words, in one pass.
