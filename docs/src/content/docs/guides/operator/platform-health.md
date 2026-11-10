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
    alt: "Platform health: the platform system's verdict linking to its system, the build the server runs and how long it has been up, then a table with one row per node, its liveness pill and verdict, and one column per platform key grouped as Checkin, Schedule and Commands."
    mask:
      - "[data-testid=platform-version] >> xpath=ancestor::div[3]"
      - "[data-testid=platform-uptime] >> xpath=ancestor::div[3]"
      - "[data-figure=checkin-age]"
      # Where work is lost reads the server's own delivery windows, which land
      # once a report window (60s here) passes, so whether its kinds and figures
      # show yet moves with when the capture reaches the page. Its tab row and
      # its hop row are full width and one line tall whatever they hold.
      - "[data-testid=pipeline] [role=group] >> xpath=.."
      - "[data-testid=pipeline] ol >> xpath=.."
  # The ingest hop's explainer: what it does, what counts as lost there and
  # the keys it is read from are the hop's own text; its components column
  # holds the server's figures once a window lands, so it is masked with the
  # rows above.
  - id: platform-health-hop
    path: /web/platform
    steps:
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
walkthroughs:
  - id: read-platform-health
  - id: find-where-work-is-lost
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
  acks its batch, and lost when the spool drops it at a cap, the bus refuses it, or it never
  encodes.
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

Two hops are not drawn yet: the schedule (whether each run the worklist asks for is run, #1693)
and the node's causes of loss by kind of failure (#1692). They appear when their figures are
written; until then the path starts at the node's spool.

## The node table

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

The overrun and skip columns are counts over the node's report window, written every window and 0
when quiet, so a node that skipped five polls last week reads 0 today, not five. The expired column
is a count over the server's command pass, and a pass that expired nothing writes nothing: it shows
only while it is the newest pass's, and reads **not reported** again once two report windows pass
with nothing new. Expired commands are the newest pass's total, the undelivered and the unexecuted
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
shown as zero. Where work is lost is left out with them, and the Loss column reads not reported.
Without a fleet-wide system read the Platform verdict reads `-`.

## From the CLI

The page is one read of the platform view, so `omniglass view platform list` returns the same
answer: the system verdict, each server's build and start time, the key groups, and each node's
verdict with its current figures, its `loss`, and the `pipeline` with each hop's figures per kind
and per component. A key a node has not reported is absent from its `figures`, and a hop or a node
with nothing to compose has no `loss`, never zero, so the command reads not-reported the same way
the page does.

## Walkthrough

From Home, open the tile, read the header, and open a node's row onto its platform component.

::walkthrough{#read-platform-health}

Read where work is lost at 0, open a hop's explainer, break a simulated panel so every poll of it
fails, watch the failures land on the panel while the node hop stays at 0, and follow the node's
row in the node hop to its delivery series.

::walkthrough{#find-where-work-is-lost}
