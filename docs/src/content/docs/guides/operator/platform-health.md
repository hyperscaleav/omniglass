---
title: Platform health
description: "Platform health: whether Omniglass itself is well, from the build it runs and how long it has been up to where work is lost hop by hop, each server's request timing, ingest lag, queue depths and retention, and whether each node is checking in, keeping its schedule and draining its command queue."
screenshots:
  # The docs lab runs with no node collecting, so the one node reads never and
  # its only figure is the check-in age the server writes for it. That age, the
  # build and the uptime change with every capture, so they are masked: the
  # header cards whole (a build id and an age, whose width moves with them) and
  # the check-in cell. The server's panels fill at its first report window, a
  # minute after the lab boots, so each shot waits for the Requests panel's
  # ranked routes; their figures, and how many routes the window saw, move with
  # every capture, so the panel grid is masked whole in the baseline.
  - id: platform-health
    path: /web/platform
    alt: "Platform health: the platform system's verdict linking to its system, the build the server runs and how long it has been up, then where work is lost hop by hop, then the server's panel set open on its Requests, Ingest, Queues and Retention panels, each row a key's last hour as a sparkline beside its current figure, the Requests panel ranking the slowest routes of the newest window."
    steps:
      - action: wait
        selector: "[data-testid=server-panel-requests] tr[data-testid=panel-row-http-request-duration-max][aria-expanded]"
        timeout: 150000
    mask:
      - "[data-testid=platform-version] >> xpath=ancestor::div[3]"
      - "[data-testid=platform-uptime] >> xpath=ancestor::div[3]"
      - "[data-testid=server-panels-server] >> xpath=./div"
      # Where work is lost reads the server's own delivery windows, which land
      # once a report window (60s here) passes, so whether its kinds and figures
      # show yet moves with when the capture reaches the page. Its tab row and
      # its hop row are full width and one line tall whatever they hold.
      - "[data-testid=pipeline] [role=group] >> xpath=.."
      - "[data-testid=pipeline] ol >> xpath=.."
  # The node table sits under where work is lost and the server panels, whose height moves with the
  # routes a window saw, so this shot closes the server's panel set first: the
  # table then sits at a fixed place under the header and the closed set.
  - id: platform-health-nodes
    path: /web/platform
    alt: "Platform health with the server's panel set closed to its heading, the server's name and verdict, over the node table: one row per node, its liveness pill and verdict, and one column per platform key grouped as Checkin, Schedule and Commands."
    steps:
      - action: wait
        selector: "[data-testid=server-panel-requests] tr[data-testid=panel-row-http-request-duration-max][aria-expanded]"
        timeout: 150000
      - action: click
        selector: "[data-testid=server-panels-server] > summary .eyebrow"
    mask:
      - "[data-testid=platform-version] >> xpath=ancestor::div[3]"
      - "[data-testid=platform-uptime] >> xpath=ancestor::div[3]"
      - "[data-figure=checkin-age]"
      - "[data-testid=pipeline] [role=group] >> xpath=.."
      - "[data-testid=pipeline] ol >> xpath=.."
  # The ingest hop's explainer: what it does, what counts as lost there and
  # the keys it is read from are the hop's own text; its components column
  # holds the server's figures once a window lands, so it is masked with the
  # rows above. It waits for the server's panels, as the shots above do, so the
  # panel set under the explainer is filled and masked the same way.
  - id: platform-health-hop
    path: /web/platform
    steps:
      - action: wait
        selector: "[data-testid=server-panel-requests] tr[data-testid=panel-row-http-request-duration-max][aria-expanded]"
        timeout: 150000
      - action: click
        selector: "[data-testid=pipeline-hop-ingest]"
    alt: "Where work is lost on Platform health with the Ingest hop open: what the server's ingest takes in and hands on, what counts as lost there, the three delivery keys it is read from with their descriptions, and its components with their share."
    mask:
      - "[data-testid=platform-version] >> xpath=ancestor::div[3]"
      - "[data-testid=platform-uptime] >> xpath=ancestor::div[3]"
      - "[data-figure=checkin-age]"
      - "[data-testid=pipeline] [role=group] >> xpath=.."
      - "[data-testid=pipeline] ol >> xpath=.."
      - "[data-testid=pipeline-hop-detail] > div:nth-child(2)"
      - "[data-testid=server-panels-server] >> xpath=./div"
walkthroughs:
  - id: read-platform-health
  - id: find-where-work-is-lost
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

## Where work is lost

Under the header, **Where work is lost** draws the path a collected item takes from the run that
produced it to the store, one hop at a time, in the order an item crosses them:

- **Node**: the node's spool, from a run's output to the stream. An item is out when the stream
  acks its batch, and lost when the spool drops it at a cap, the bus refuses it, it never encodes,
  or (one of the node's own log lines) its bounded log buffer drops it.
- **Ingest**: the server's ingest, from the stream to the store. An item is out when it is stored,
  coalesced or had already landed (a replay is not a loss), and lost when it is refused, orphaned,
  or its batch is terminated whole.

Every hop is read from the same three keys, written by the component that runs the hop:
`delivery-in-count`, `delivery-out-count` and `delivery-loss-ratio`, one series per item kind
(metric, property, event, log) as the instance
([how each hop counts](/architecture/messaging/#measuring-the-pipeline)). So one rule reads every
hop: its **loss** is the share of what it took in that it did not hand on, `(in - out) / in`, over
the last hour's summed counts across every component on the hop. It is never an average of the
components' ratios: a node that lost 1 of 2 and one that lost none of 98 are a hop that lost 1 of
100, 1%. **0% is healthy**, and a hop above 0 is tinted so it stands out.

- **End to end** is the share lost anywhere on the path: one minus the product of each hop's
  survival, `1 - (1 - a)(1 - b)`, so two hops losing 1% each lose 1.99% end to end, not 2%.
- **All, Metric, Property, Event, Log** read the figures for every kind summed or one kind alone.
  A kind shows only once a hop has carried it.
- A hop that has written nothing yet reads **not reported**, never 0: a 0 would claim it measured
  no loss. A hop with series but nothing in over the hour shows its zero counts and **nothing in**,
  no ratio.

Select a hop for its explainer: what it does, what counts as out and as lost there, the keys it is
read from with their descriptions from the [metrics catalog](/guides/admin/metrics/), the class of
platform component that records it, and each component's own share. Each component links to its
platform component, where its delivery series live with their history.

::screenshot{#platform-health-hop}

A failed poll is not lost work. A device that does not answer is a fact the node collects and hands
on (it lands on that device's component, as its poll failures), so the node hop still reads 0 while
a broken device fails every poll. A hop's loss is work the platform itself dropped.

Two parts of the path are not drawn yet. The schedule hop (whether each run the worklist asks for
is run): nodes write its figures since #1693, at the instance `schedule/{kind}` on their own
component, where its series already read; the page draws it with #1739. Until then the path starts
at the node's spool. The node hop's loss by cause (a spool cap, a bus refusal, a dropped self-log
line, an encode failure) is not drawn here either: each batch's ledger carries it, and a lab run
reads it per kind ([reading the delivery table](/guides/lab/#reading-the-delivery-table), #1692).

## The server panels

Each server has a panel set: a disclosure headed with the server's name, its verdict and how many
alarms are open on it, the first one open. The name links to the server's platform component. The
four panels are **Requests**, **Ingest**, **Queues** and **Retention**, and the keys in each are the
groups the platform view returns for a server, so the page holds no key list of its own.

A panel row is one key: its last hour as a sparkline beside its current figure. Click a row, or tab
to it and press Enter or Space, to open the full chart beneath it, and again to close it. Hover a key's name, or tab to it, for what
it measures, the description from the [metrics catalog](/guides/admin/metrics/); the tooltip links
onto the key's series on the server's platform component, the same component view any device has.
A key the server has not reported reads **not reported**, never 0, and has no sparkline. A key the
server writes only for a window that saw something (a request, a landed batch, a slow query) is
that window's figure, so it reads **not reported** again once two report windows pass with nothing
new, instance by instance: an ingest lag from before the fleet went quiet is not shown as
today's. The gauges the server samples every window, and the retention facts its hourly pass
writes, read their latest however old. A pair
that only reads against its bound (a pool's connections in use against its size, the oldest
partition's age against the retention window) reads `a / b`, and byte sizes read in KB, MB or GB.

A panel turns amber, and names the alarm, only while an alarm is open on one of its keys. The seeded
rules on the server's keys raise those alarms; the page adds no threshold of its own, so an amber
panel is the same open alarm the console shows everywhere else.

A closed panel set reads no series: its figures are already on the page, and its sparklines load
when you open it.

### Requests

How the API is answering, measured inside the server for every request by its method, route
template and status class. The slowest routes and the busiest are each ranked, highest first, as
the server cut them from its newest report window: a window is a minute by default, so a ranking
is the last minute's, and a route that went quiet drops off it. Below them sits the database's side
of a request: the queries that ran past the slow-query threshold, the connection pool in use
against its size, and the acquires that found the pool exhausted and waited.

### Ingest

How far behind the pipeline that lands telemetry runs. **Ingest Lag** is the time from a sample's
timestamp to the commit that landed it, averaged over the window's batches, so it reads how stale
the figures on every other page can be; **Ingest Lag (max)** beside it is the oldest sample's. The
sample's time is the node's clock, so a node whose clock is skewed reads as lag. Then the batches
and samples landed in the window. The server hop's delivery (the items of each kind it took in,
handed on and lost, as a loss ratio where 0 is healthy) lands on its platform component every window
(#1659) and is read as the Ingest hop under [where work is lost](#where-work-is-lost). An oldest sample more than 30 seconds behind for two minutes raises the seeded Ingest lag high alarm,
which turns this panel amber.

### Queues

What is waiting. The telemetry batches not yet delivered to the server's consumer and those
delivered but not yet acknowledged, the calculations waiting to run, the outbox's queued effects
and how long the oldest due one has waited, and the share of the window the telemetry lane spent
busy. A lane near fully busy with a backlog that only grows is a server that cannot keep up,
usually before any figure elsewhere moves.

### Retention

Whether old data is dropped on time. **Retention Behind** has a row per lane: 0 when the lane's
last pass caught up with its window, 1 when it stopped short or failed. A lane that stays at 1
raises the seeded Retention behind alarm. Beside it, the rows each pass deleted, the oldest metric
day's age read against the retention window (steady at about the window plus a day once retention
keeps up), and the database's size with each table's.

## The node table

::screenshot{#platform-health-nodes}

One row per node. **Status** is the same liveness pill the [Nodes](/guides/operator/collection/)
page shows (`up`, `down` or `never`), judged at the same moment the uptime is, beside the node's
verdict. A row's tone is that verdict: the seeded rules on the platform keys decide it, and the
page adds no threshold of its own.

The columns are the platform keys, grouped:

- **Checkin**: how long since the node last checked in.
- **Schedule**: how late its collection ticks run, and how many overran or were skipped.
- **Commands**: how many commands wait for it, the oldest one's age, and how many are stuck or
  expired.
- **Loss**: **Node Loss** is the node's own share of [where work is lost](#where-work-is-lost): the
  end to end over the last hour of the hops its own component records (the node hop today), every
  kind summed. 0% is healthy; a node whose hops took nothing in reads **not reported**. Its series
  are on the node's platform component, one per kind.

Hover a column's header, or tab to it, for what the key measures; the text is the key's description from the
[metrics catalog](/guides/admin/metrics/), not prose written into the page. A figure the node has
never reported reads **not reported**, never 0: a 0 would claim the node measured nothing wrong.

The overrun, skip and expired columns are counts over a window (the collection report window, or
the server's command pass for expired commands), written every window, 0 when nothing happened. So a
count is the newest window's: a node that skipped five polls last week and none since shows 0 today.
Expired commands are the newest pass's total, the undelivered and the unexecuted together; the
node's platform component splits them by instance.

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
shown as zero, and the server panels are left out whole. Where work is lost is left out with them,
and the Loss column reads not reported. Without a fleet-wide system read the Platform verdict reads
`-`.

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
answer: the system verdict, each server's build and start time with its panel groups (each key's
current figure, a ranked key's routes, and the alarms open on the server), the node key groups,
each node's verdict with its current figures and its `loss`, and the `pipeline` with each hop's
figures per kind and per component. A key a node has not reported is absent from its `figures`,
and a hop or a node with nothing to compose has no `loss`, never zero, so the command reads
not-reported the same way the page does.

`omniglass view platform simulation list` is the simulation section's read (`simulation:read`, and a
fleet-wide component read, since a node's window spans every device it simulates): each node's window
state, start and end, its gaps with their ratios and readings, its protocols and its devices.

## Walkthroughs

From Home, open the tile and read the header, read each hop under where work is lost as a loss ratio
and open the node hop's explainer, fold the server's panels to read the node table and the simulator's
gaps under it, open the panels again on the Requests panel, then follow the slowest route's figure from
its tooltip onto its series on the server's platform component.

::walkthrough{#read-platform-health}

Read where work is lost at 0, open a hop's explainer, break a simulated panel so every poll of it
fails, watch the failures land on the panel while the node hop stays at 0, and follow the node's
row in the node hop to its delivery series.

::walkthrough{#find-where-work-is-lost}

On a lab with a simulator, drop every packet toward one snmp device from its drawer, watch its gap
open on Platform health read as impaired, then repair it and watch the attribution clear as the window
slides past.

::walkthrough{#watch-an-impaired-device}
