---
title: Platform health
description: "Platform health: whether Omniglass itself is well, from the build it runs and how long it has been up to each server's request timing, ingest lag, queue depths and retention, and whether each node is checking in, keeping its schedule and draining its command queue."
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
    alt: "Platform health: the platform system's verdict linking to its system, the build the server runs and how long it has been up, then the server's panel set open on its Requests, Ingest, Queues and Retention panels, each row a key's last hour as a sparkline beside its current figure, the Requests panel ranking the slowest routes of the newest window."
    steps:
      - action: wait
        selector: "[data-testid=server-panel-requests] tr[data-testid=panel-row-http-request-duration-max][aria-expanded]"
        timeout: 150000
    mask:
      - "[data-testid=platform-version] >> xpath=ancestor::div[3]"
      - "[data-testid=platform-uptime] >> xpath=ancestor::div[3]"
      - "[data-testid=server-panels-server] >> xpath=./div"
  # The node table sits under the server panels, whose height moves with the
  # routes a window saw, so this shot closes the server's panel set first: the
  # table then sits at a fixed place under the header and the closed set.
  - id: platform-health-nodes
    path: /web/platform
    alt: "Platform health with the server's panel set closed to its heading, the server's name and verdict, over the node table: one row per node, its liveness pill and verdict, and one column per platform key grouped as Checkin, Schedule, Delivery and Commands."
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
walkthroughs:
  - id: read-platform-health
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
server writes only for a window that saw something (a request, a landed batch, a drop, a slow query)
is that window's figure, so it reads **not reported** again once two report windows pass with
nothing new, a drop reason on its own: an ingest lag from before the fleet went quiet is not shown as
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
and samples landed in the window, and the batches the server gave up on, one row per reason. An
oldest sample more than 30 seconds behind for two minutes raises the seeded Ingest lag high alarm,
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
- **Delivery**: the node's own counts of samples dropped from its spool and publishes that failed.
- **Commands**: how many commands wait for it, the oldest one's age, and how many are stuck or
  expired.

Hover a column's header, or tab to it, for what the key measures; the text is the key's description from the
[metrics catalog](/guides/admin/metrics/), not prose written into the page. A figure the node has
never reported reads **not reported**, never 0: a 0 would claim the node measured nothing wrong.

The overrun, skip, drop, failure and expired columns are counts over a window (the collection report
window, or the server's command pass for expired commands), written every window, 0 when nothing
happened. So a count is the newest window's: a node that dropped five batches last week and none
since shows 0 today. Expired commands are the newest pass's total, the undelivered and the unexecuted
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
shown as zero, and the server panels are left out whole. Without a fleet-wide system read the Platform verdict reads `-`.

## From the CLI

The page is one read of the platform view, so `omniglass view platform list` returns the same
answer: the system verdict, each server's build and start time with its panel groups (each key's
current figure, a ranked key's routes, and the alarms open on the server), the node key groups,
and each node's verdict with its current figures. A key a node has not reported is absent from its `figures`,
never zero, so the command reads not-reported the same way the page does.

## Walkthrough

From Home, open the tile and read the header, fold the server's panels to read the node table,
open them again on the Requests panel, then follow the slowest route's figure from its tooltip onto
its series on the server's platform component.

::walkthrough{#read-platform-health}
