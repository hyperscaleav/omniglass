---
title: Alarms and health
description: "An alarm's life on a component (raise, acknowledge, snooze, clear), how health rolls up from it to a system and a location, and the loop end to end."
sidebar:
  label: Alarms and health
  order: 4
  badge:
    text: Partial
    variant: note
---

An **alarm** says what is wrong with one component. **Health** is what that means for the systems the
component serves and the locations above them. This page follows an alarm from raise to clear, then
follows its effect up the fleet.

## Alarms on a component

An **alarm** says what is wrong with **one component**. The component's **Alarms** panel lists the
active ones newest first, with a **Recently cleared** group beneath them: what is wrong now on top,
what was wrong underneath.

Raising one takes two things:

- a **severity**: one of the severity levels, listed with their rank, color and effect under **Catalog,
  under Actions: Severity** (the [rules guide](/guides/admin/rules/#severity-levels) renders the shipped
  set, and an admin can add more between them on that page). Every alarm wears its level's label in its color. This is how loudly to treat it, and it sets the
  **component's own** verdict (any active alarm makes the component degraded, one at a level marked
  outage takes it out). Only an **outage** stops a component occupying a slot it fills: an info or warning
  alarm still degrades it, but the component keeps its slot, so a quiet issue does not short-staff
  a system on its own;
- a **message**, for whoever reads it later. Write it for the person who finds this at 8am, not for you.

**Not every alarm is raised by hand.** An [event rule](/guides/admin/rules/) raises and clears its
own as telemetry lands: such an alarm says **by rule** and the rule's name, and the rule clears it
when the rule clears (the official reachability rule does this for every endpoint that stops
answering). You acknowledge it like any other. Clearing it by hand works too, and **a hand clear of a
rule's alarm holds until the rule clears and fires again**, whatever the input: a rule acts on
edges, not on each sample, so a metric still reading over its threshold on every poll does
not reopen it. The rule still counts itself as firing there (the firing and the events it wrote
are untouched); once the value recovers the rule clears, and if it fires again later it raises
a new alarm. Fix the fault rather than clearing its alarm.

**Clearing keeps the row.** The alarm moves to the history with the time it was cleared, so what was wrong
and when survives the fix. Clearing one twice is a plain miss rather than a silent success.

Both writes take effect immediately and completely: the system's verdict, the location above it, and the
recorded history all move in the same transaction as the alarm. There is no wait and no refresh cycle.

### Acknowledging: saying you have seen it

**Acknowledging an alarm records that a human has looked at it, and changes nothing else.** The alarm
stays exactly as raised as it was, the component stays exactly as broken, and no verdict moves.
Acknowledging is not fixing; clearing is.

That is why the two are independent, and the panel shows both facts at once:

- an alarm nobody has looked at carries an **unacknowledged** chip, and the panel header counts them,
  so the queue you actually work (raised, and nobody on it) is visible without opening each one;
- an acknowledged alarm names **who** looked and **when**, and stays in the active list, because it
  is still broken;
- a cleared alarm that was **never acknowledged** says so in the history: it came and went with
  nobody looking, which is worth spotting later.

The **eye** button beside an active alarm acknowledges it. Unlike raising and clearing, it is
available **without entering edit mode**: edit mode guards this component's own data, and an
acknowledgement writes none of it. It needs the `alarm:acknowledge` permission, which the
**Operator** role carries; a viewer sees the indicator and gets no button.

Acknowledging twice is harmless. The first person and the first time are what the alarm keeps, so a
second click (or a colleague reaching it a moment after you) changes nothing and is not an error.
There is no un-acknowledge.

### Snoozing: silencing a noisy alarm for a while

**Snoozing stops the people an alarm notifies from being told about it, for a bounded time.** It is
for the alarm that flaps or that you are already working on: **Snooze** beside the alarm (in the
**Why** strip and the alarms panel, active or recently cleared) offers 15 minutes, 1 hour, 4 hours
or 24 hours. While it holds, every notification about that alarm, a raise or a clear, on a
webhook, by email or in an inbox, is recorded instead of sent, and each one stays on the alarm's
timeline reading **snoozed**, with the reason. The alarm wears a **snoozed until** chip naming the time, and its tooltip
names who snoozed it.

A snooze belongs to the alarm's **identity** (its component and dedup key), not to one alarm row: when
the alarm clears and is raised again, the new alarm is snoozed too, until the deadline. Another alarm on
the same component is not affected. When the snooze ends, by its deadline or by **Unsnooze**, and the
alarm is still raised, the people its rules reach are told again; an alarm that cleared meanwhile gets nothing.

It is not acknowledging (nobody is recorded as having looked), not [opting out](/guides/admin/notifications/)
(that silences only you), and not pausing an action rule (that silences a rule for every alarm). It
changes nothing about the alarm itself: the component stays exactly as broken, and an unacknowledged
alarm stays in the queue. It needs the `alarm:snooze` permission, which the **Operator** role carries;
a viewer sees the chip and gets no button.

```bash
omniglass component alarm snooze display-1 <alarm-id> --duration 1h
omniglass component alarm unsnooze display-1 <alarm-id>
```

### Reading an alarm's timeline

The **clock** button on an alarm row (in the **Why** strip at the top of a component's or a system's
page, and in the alarms panel, active or recently cleared) opens the alarm's **timeline** under the row: everything recorded about it, oldest first. An **event** line is what a rule saw (its
`rule-fired` when it fired and raised the alarm, its `rule-cleared`
when it cleared and resolved it), named by the rule and the endpoint; an **audit** line is what was
done to the alarm (raised, acknowledged, cleared, snoozed, unsnoozed, snooze ended) and by whom, a person by name or the rule the
platform acted for. A **notify** line is a notification the alarm's raise or clear queued, read as a
status row: its target, how its delivery stands (**on its way**, **sent**, **failed** with the
receiver's answer and a **Retry**, or **snoozed** with the snooze's reason), and the **action rule**
that queued it. A hand-raised alarm's timeline is its audit lines alone. The same button sits on
each open alarm in a rule's blade on the Event rules page. Click it again to fold the timeline away; any
number can be open at once.

From the CLI: `omniglass component alarm list <name> [--include-cleared] [--unacknowledged]`,
`omniglass component alarm create <name> --severity <level> --message <text>`,
`omniglass component alarm acknowledge <name> <id>`, `omniglass component alarm timeline <name> <id>`, and
`omniglass component alarm delete <name> <id>`.


## Health on a system or location

A **system** and a **location** each carry a **health verdict**, shown as a badge on its detail view and in
the outline:

| verdict | means |
|---|---|
| **healthy** | nothing it depends on is impaired |
| **incomplete** | something it needs was never installed |
| **degraded** | it is working, worse |
| **outage** | it is not working |

**`incomplete` is not a fault.** It means a slot is short because nobody has put the hardware in
yet, so no alarm will ever fire for it: there is nothing there to alarm. A system mid-installation
reads incomplete, and it stays that way until somebody fills the slot. That is deliberately a
different colour from a system that is broken, because during a rollout most of your fleet is in
the first state and you need to be able to see past it to the second.

A location's verdict is the **worst** of every system placed anywhere beneath it, so a top-level location reads red
when one system anywhere beneath it is out. A system's verdict is the worst contribution among the **slots** it
needs filled.

**The system's view is the answer to "why".** A bare "degraded" gives you nothing to do, so the
view names the chain instead: **Why** leads its Overview, each alarm naming the down component
and the slot it impairs, and the component rows beneath carry the arithmetic. Read together they are this
chain:

```text
alarm on mic-pod-2 (high, "no audio on channel 1")
  -> mic-pod-2 is down (its own verdict, from the alarm)
    -> slot room-mic wants 2, and mic-pod-2 no longer occupies its slot
      -> only 1 assigned component still occupies it
        -> slot impaired, impact degraded
          -> hq-r1 is degraded
```

Read it bottom-up when you want the verdict and top-down when you want the fix. A slot can also be
impaired with **no down component named**, which means it is **short-staffed** rather than broken:
nobody is assigned. Those are two different jobs, and the view keeps them apart: an unstaffed seat
reads **incomplete** as an empty slot, a down occupant keeps its seat and reads the impact its slot
declared.

**The History strip is the answer to "since when".** It is the same shape as the reachability
availability strip: one segment per stretch the entity held a verdict, drawn from the **recorded edges**
over the last 30 days. It is not a sample and not a redraw of what somebody happened to look at; each edge
was written at the moment the fleet changed, by the write that changed it. That is what makes "it broke
Friday at 18:40 and came back Monday at 09:15" answerable on Tuesday.

From the CLI: `omniglass system health list <name>` and `omniglass location health list <name>`.

## The whole loop, end to end

Once, in order, on a real system:

1. **Declare the slots with their impact.** On the system's
   [standard](/admin/standards/#slots-what-a-conforming-system-needs-filled), give **Main Display**
   impact **outage** and **Room Microphone** impact **degraded** with quorum 2, accepting `video-bar`.
   Every conforming system inherits both immediately.
2. **Staff the system.** Assign components to each slot from the system's **Slots** panel. A component
   of the wrong type is refused by name (`component "panel-1" is a display; slot "Room Microphone"
   wants a video-bar`), so a wrong assignment never becomes a wrong verdict.
3. **Raise an alarm.** On one of the mic pods, raise a `high` alarm.
4. **Watch the system move.** The system goes **degraded** (the `room-mic` slot now has one occupant
   against a quorum of 2, and its impact is `degraded`), and the location above it follows. Had
   the alarm been on the main display instead, the system would be an **outage**, because that slot says so.
5. **Read the system's Overview** to find the cause: the active alarm, the component it took down and
   the slot that component fills, with the message and the time it was raised. Walk to the pod.
6. **Clear the alarm** once it is fixed. The system returns to **healthy** in the same transaction, and the
   alarm row stays in the component's history.
7. **Read the history afterwards.** The transition strip now shows the exact stretch the system was
   degraded, with the edge at the moment the alarm went up rather than the moment you opened this page.
   That is the whole point: come back in three weeks and the answer is still exact.

:::design[Target design, tracked in #1344 and #1345]
An alarm runs a flow: remediate, wait, check again, and escalate only if the fix did not hold (#1344).
An alarm storm (every component behind one dead switch) rolls up into a few notifications instead of
one per component (#1345).
:::
