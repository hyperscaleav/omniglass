---
title: The model in one pass
description: "The words Omniglass is built on, in the order the data travels: the fleet, collecting from it, judging it, acting on it, and seeing it."
sidebar:
  label: The model
  order: 2
  badge:
    text: Partial
    variant: note
---

Every monitoring system has the same shape: **collect, evaluate, raise, act, and see it the whole
time.** This page walks that shape once, naming each word as it appears. Each word is defined once in
the [glossary](/reference/glossary/); the sections that follow this one go deep on each.

## The fleet

The **fleet** is everything an install manages.

- A **component** is one managed device or service: a display, a DSP, a cloud meeting service. Each
  is an instance of a **product**, a model a **vendor** sells, classified by its **component type**
  (display, projector, microphone).
- A **system** is the unit Omniglass monitors: a set of components in one location that works as one,
  with one health of its own. Its **system type** says what it is for (meeting, teaching, signage).
- A **standard** is the contract a system is built to. It declares the **system slots** the system
  fills (a main display, two table microphones), how many components each wants, and what losing one
  means.
- A **location** is a place the fleet occupies, nested under the location it sits in, to any depth. Its **location type** says what kind of place it is and what it may sit under.

The products, vendors, types and drivers the fleet is typed by are the **catalog**: Omniglass ships
one, and an admin extends it.

:::design[Target design, tracked in #876, #877 and #879]
Every system has a location, which bounds where its components sit (#876, #877), and a system can
group other systems, such as a divisible hall and the rooms it splits into (#879).
:::

## Collecting

An **outpost** is a collection process placed near the gear. It reaches each component through an
**endpoint**, a connection over one **transport** (ssh, https, snmp, telnet), and runs **tasks** on
it: a poll on a cadence, or a listener it holds open. The product's **driver** is the recipe: what to
fetch, how to read the answer, and what can be set. The outpost parses at the edge and sends back
typed values, never raw device output.

What comes back is typed by the catalog: a **metric type** is a numeric series (a temperature, a
response time), a **property type** a categorical value (a power state, an input, a firmware
version), and an **event type** a discrete occurrence (a button press, a reboot). The same driver
work makes two vendors' displays report the same metric under the same name.

Not every value is collected. A **calc rule** derives a new value from others, such as a system's
count of components powered on. A **declared value** is what a component or system *should* hold
(this display's input is HDMI 1); Omniglass compares it with what the device reports and shows the
drift.

## Judging

An **event rule** watches signals and fires when its criteria hold: a display unreachable for two
minutes, a temperature over its line. A rule that carries a **severity level** raises an **alarm**
on the component, which stays raised until the criteria clear.

Health then rolls up. A slot with fewer working components than it wants is impaired, and contributes
what the standard says losing it means. The system takes the worst contribution, and a location the
worst of everything in it. A system's health is one of four verdicts:

- **healthy**: everything is present and well;
- **incomplete**: still missing a component its standard requires (commissioning, not a fault);
- **degraded**: it still works, but has lost something it should have;
- **outage**: it cannot do its job.

Every change of verdict is recorded, so a system's health has a history.

:::design[Target design, tracked in #1002]
An event rule can also judge a system's own values, such as a calc rule's result, so an alarm can
be raised on the system rather than only on one of its components.
:::

## Acting

An **action rule** decides when a **notification** about an alarm is sent and to which
**notification targets**: a webhook, a mailbox, a user group or a person. A **command** is an
instruction sent to a component (power on, select an input), typed by a **command type**, with its
outcome recorded.

:::design[Target design, tracked in #1344 and #1345]
A flow runs on an alarm: remediate, wait, check again, escalate only if the fix did not hold (#1344).
And an alarm storm (every component behind one dead switch) rolls up into a few notifications,
deduplicated and suppressed, instead of one page per component (#1345).
:::

## Seeing it

Everything above is visible the whole time, in the console, the API and the CLI, which read the same
data. A **variable** is a named value set at one level of the fleet and inherited below it, and a
**secret** is an encrypted credential inherited the same way, so a password set at a building reaches
every component in it. A **tag** is an operator's key and value on anything in the fleet.

Who may see and do what is two layers: a **role** grants permissions to a **user** or a **user
group**, and the grant is scoped to part of the fleet. Every privileged action writes an **audit
event**.

Next: [your first ten minutes](/start/first-ten-minutes/), on a lab with a simulated fleet.
