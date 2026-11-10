---
title: Platform health
description: "Platform health: whether Omniglass itself is well, from the build it runs and how long it has been up to whether each node is checking in, keeping its schedule and draining its command queue."
screenshots:
  # The docs lab runs with no node collecting, so the one node reads never and
  # its only figure is the check-in age the server writes for it. That age, the
  # build and the uptime change with every capture, so they are masked: the
  # header cards whole (a build id and an age, whose width moves with them) and
  # the check-in cell.
  - id: platform-health
    path: /web/platform
    alt: "Platform health: the platform system's verdict linking to its system, the build the server runs and how long it has been up, then a table with one row per node, its liveness pill and verdict, and one column per platform key grouped as Checkin, Schedule, Delivery and Commands."
    mask:
      - "[data-testid=platform-version] >> xpath=ancestor::div[3]"
      - "[data-testid=platform-uptime] >> xpath=ancestor::div[3]"
      - "[data-figure=checkin-age]"
walkthroughs:
  - id: read-platform-health
  - id: watch-an-impaired-device
---

Omniglass monitors a fleet, and it is a fleet of its own: a server, a database and the nodes that
collect. **Platform health** is Omniglass watching itself. It answers one question, whether the
platform is well, and it answers it the way you would answer it about any room: from the samples
on the platform's own components.

Open it from **Platform health** in the sidebar, under Nodes, or from the **Platform** tile on
Home, which carries the platform's verdict and how many nodes are not up. You need `node:read`
to open it; the tile and the sidebar entry are hidden without it.

::screenshot{#platform-health}

## The header

- **Platform** is the verdict of the platform system, the system every node and every server is
  a component of. It links to that system's own view, which explains the verdict the way every
  system's view does, so the reasons are written once.
- **Version** is the build the server runs, written by the server on its own platform component
  each time it boots.
- **Up for** is how long the server has been running: the page's read time minus the start time
  its boot wrote. A restart moves the start time forward.
- On a lab instance the page says so beside its title.

## The node table

One row per node. **Status** is the same liveness pill the [Nodes](/guides/operator/collection/)
page shows (`up`, `down` or `never`), judged at the same moment the uptime is, beside the node's
verdict. A row's tone is that verdict: the seeded rules on the platform keys decide it, and the
page adds no threshold of its own.

The columns are the platform keys, grouped:

- **Checkin**: how long since the node last checked in.
- **Schedule**: how late its collection ticks run, and how many overran or were skipped.
- **Delivery**: the node's own counts of samples dropped from its spool and publishes that failed.
- **Commands**: how many commands wait for it, the oldest one's age, and how many are stuck or
  expired.

Hover a column's header, or tab to it, for what the key measures; the text is the key's description from the
[metrics catalog](/guides/admin/metrics/), not prose written into the page. A figure the node has
never reported reads **not reported**, never 0: a 0 would claim the node measured nothing wrong.

The overrun, skip, drop, failure and expired columns are counts over a window (the collection report
window, or the server's command pass for expired commands), and a window that counted nothing writes
nothing. So a count shows only while it is the newest window's, and reads **not reported** again once
two report windows pass with nothing new: a node that dropped five batches last week does not show
five today. Expired commands are the newest pass's total, the undelivered and the unexecuted
together; the node's platform component splits them by instance.

Click a row to open the node's **platform component**, the same component view any device has.
Each figure on the row is a series there, with its history. The arrow at the end of the row opens
the node in the Nodes page instead, where enrollment and editing live.

## Without a fleet-wide read

The rows are every node, so the page needs a fleet-wide `node:read`. If yours covers only some
places, the page says it needs a fleet-wide node read instead of listing a partial fleet, and the
Home tile reads the same.

The platform components belong to no place, so only a fleet-wide (all-scope) read reaches them.
If your component read is scoped to part of the fleet, the page still lists every node with its
pill, and one line says the platform figures need a fleet-wide read: the figures are left out, not
shown as zero. Without a fleet-wide system read the Platform verdict reads `-`.

## With a simulator

While a [simulator](/guides/operator/simulation/) is connected, and you hold `simulation:read`, the page
gains a section below the node table: the same condition that shows **Simulation** in the sidebar, so the
two appear and disappear together. Everything above it is the platform's own account of itself. The
simulator is the one witness outside the platform: it counts what each simulated device saw arrive and
how it answered, while the node counts what it sent and the server counts what landed. Set side by side,
the two accounts **close the loop**: anything lost between them is a gap the page counts, not one it
infers. Only a simulator can say what a device was asked, so a fleet of real gear shows no section.

### The closed loop

Each node with a simulator gets a card. Its first figures are the telemetry batches the server landed
for the node over the window and the most requests the simulator held at once. **Queue depth** and
**Busy workers** read `not wired yet`: the node does not report them to the server yet, and a blank
reads truer than a 0 (#1634, #1249).

The gap table is the loop itself, one row per hop a request or a batch travels:

- **connect, not accepted**: connections the node opened that no listener on the simulator accepted,
  pooled over every listener on the node, so its protocol reads `any listener`.
- **sent, not received**: requests whose bytes left the node that the simulator never received.
- **received, not settled**: requests the simulator received and never finished, with an answer, a
  refusal or an error.
- **answered, not returned**: answers the simulator gave that the node never got back.
- **published, not landed**: telemetry batches the bus acknowledged that never landed on the server.
- **spool dropped**: batches the node's spool gave up on.
- **unhit endpoint**: a poll target due at least twice over the window that the simulator saw nothing for.

Each row is a **loss ratio**, like every health figure in Omniglass: what was lost at the hop over what
went into it, 0 healthy, with both counts beside it. A hop nothing went into reads `-`, never 0 and never
a division error. Every gap is a lower bound: a request still in flight at the window's end, an answer
late past the node's timeout, a batch landed just outside the window could each hide a loss, so
**Masked by** names that term, with its count when it has one. The worst gaps sort first.

Below the gaps, the protocol table says what each protocol drew from the simulator over the window:
connections **Accepted**, requests **Received** and **Answered**, the simulator's own p95 service time,
the node's **Poll targets** on the protocol, and the most requests it held at once. A probe that only
connects (a tcp reachability check), or logs in and asks nothing, draws a connection and no request, so
those rows count accepted connections beside 0 received. The device table lists every simulated device
with the requests it received by protocol: a device due a poll that saw none reads **due, never seen**
and sorts first, then the impaired ones. A device's name opens its drawer on the Simulation page. The
table does not yet show what each device answered (#1721), nor the system a repeated name belongs to
(#1724).

### The sliding window

The simulator's and the node's counters only grow from the moment each starts, so a total since start
would carry an hour-old loss into every read after it. Instead the server reads each node every minute
and keeps its recent reads: the window is the last ten minutes, and it slides a minute with each read.
Each read is a bracket of three, the simulator, the node, the simulator again, so a request in transit
between two reads lands inside the bracket and never reads as a gap. Batches are counted by the time
they landed, through the read after the window's end, so one the bus acknowledged just before the end
has time to land. The card's header names the window's start and end.

A lab brackets faster, so its window fills in seconds: every 10 seconds (a 100 second window) on a
lab you run, every second (a 10 second window) on the throwaway labs the tests and walkthroughs run.
`OMNIGLASS_CLOSED_LOOP_BRACKET` sets it on a server, from `1s` to `1m`. To count which poll targets
were due, the server also has to know how often the node polls: set
`OMNIGLASS_CLOSED_LOOP_HEARTBEAT` and `OMNIGLASS_CLOSED_LOOP_PROBE_EVERY` to the nodes' `--heartbeat`
and `--probe-every` (the lab sets all three; #1722 has each node report its own). The window lives in the server's memory, per process: a
server that restarts starts it over, and nothing of it is stored.

### How each gap reads

- **warming up**: the server does not hold a window and the read after it yet. The card says so and
  shows no number, since a count from a partial window would read as a whole one. After three reads
  the window is one bracket long; it grows a bracket at a time to ten, then slides.
- **loss**: nothing explains the count, so a gap above 0 is a fault, and its row and ratio turn red.
- **impaired**: the node's simulator has an [impairment](/guides/operator/simulation/#break-it-on-purpose)
  applied, which loses traffic on purpose below the simulator's view. The gap is expected, not a
  fault, and reads blue. The attribution is node-wide today: every gap on that node reads impaired
  while any device on it is (#1662 narrows it to the protocol). Once the window slides past the repair,
  it clears.
- **restarted**: a counter went backwards inside the window, because the node or its simulator started
  again. No figure from that window means anything, so the card says so and shows none until the
  window slides past the restart.
- **not observed by the sim**: the kernel answers it (a ping), so the simulator never sees it.

An impairment that drops packets shows where the protocol rides on them. Snmp is datagrams: a dropped
request is gone, so the node counts it sent, the simulator never sees it, and **sent, not received**
opens. Tcp retransmits what it loses, and a connect that never completes sent nothing, so loss on a tcp
device slows it but opens no gap (#1723 tallies the dial, so a lost connect will). The walkthrough below drops an snmp device's packets and watches the
gap open and clear.

## From the CLI

The page is one read of the platform view, so `omniglass view platform list` returns the same
answer: the system verdict, each server's build and start time, the key groups, and each node's
verdict with its current figures. A key a node has not reported is absent from its `figures`,
never zero, so the command reads not-reported the same way the page does.

`omniglass view platform simulation list` is the simulation section's read (`simulation:read`, and a
fleet-wide component read, since a node's window spans every device it simulates): each node's window
state, start and end, its gaps with their ratios and readings, its protocols and its devices.

## Walkthroughs

From Home, open the tile, read the header, and open a node's row onto its platform component.

::walkthrough{#read-platform-health}

On a lab with a simulator, drop every packet toward one snmp device from its drawer, watch its gap
open on Platform health read as impaired, then repair it and watch the attribution clear as the window
slides past.

::walkthrough{#watch-an-impaired-device}
