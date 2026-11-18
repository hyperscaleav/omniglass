---
title: Commands
description: "Sending an instruction to a component, following it from queued to its outcome, and reading the evidence it settled on."
sidebar:
  label: Commands
  order: 5
  badge:
    text: Built
    variant: success
---

A **command** is an instruction sent to a component: power on, select an input, set a volume. Its
**command type** says what it sets, the driver on one of the component's endpoints says how, and the
outcome is recorded with its evidence.

## Reading a component's commands

A component's **Activity** tab carries its **Commands** above its events: what the component was told
and where each command stands, newest first. A command in flight wears its phase, **queued** (waiting
for its outpost to take it), **delivered** (its outpost has it, and the next observation settles it),
**awaiting reply** or **awaiting observation**, with its settle window counting down beside it; a
terminal one reads **settled**, **failed** or **timed out**, with why. Hover the phase for what it
means for that command. A fire-and-forget command such as a reboot has nothing to observe, so its
outpost's report decides it: **settled** once the outpost carried it out, **failed** when it could not,
and **timed out** when no report arrives within ten minutes of its issue. The list refreshes every second while anything is in flight.

The arrow at a row's end opens the command in place: its **arc**, the issue's time and each later
step (delivered to its outpost, answered, settled) as how long after the issue it came; **why** it
settled, failed or timed out; how it **settles** (from the component's reply or an observation of
what it sets), its window, the **endpoint**, **outpost** and **driver** that carried it, and who
**issued** it; and the **evidence**: the component's **reply** (the endpoint's login and anything it labels a
credential masked before it was stored, or withheld when that login could not be read), or the **observed** sample that matched. A timed-out command says where it stopped:
queued for an outpost that never took it, delivered with no answer, or answered with nothing observed
after.

## Issuing a command

**Issue a command** (with `command:issue`, which the **Operator** role carries) sits in the section's
header wherever a driver attached to one of the component's endpoints binds a command, and offers
only those commands, since only those reach it. Pick the command, give the value it sets (a number
for a metric such as a volume, any value for a property such as an input; a fire-and-forget command
such as a reboot takes none), and **Issue**. A **toast** follows the new command live, from queued to
its outcome, and its **Open** brings you to its row here, opened. A component no driver can command
says so where its history would be.

Reading a component's commands takes `command:read`, which every viewer holds; a viewer reads the
history and the evidence and gets no Issue button.

From the CLI: `omniglass component command issue <name> --command-type <type> [--value <v>] [--wait]`,
`omniglass component command list <name> [--status <s>]`, and `omniglass component command get <name> <id>`
([commands](/guides/admin/commands/#read-a-command-back)).

