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

The node's delivery (the items of each kind its spool took in, handed on and lost, as a loss ratio
where 0 is healthy) lands on its platform component every window (#1659); its column group on this
page is #1646's.

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
shown as zero. Without a fleet-wide system read the Platform verdict reads `-`.

## From the CLI

The page is one read of the platform view, so `omniglass view platform list` returns the same
answer: the system verdict, each server's build and start time, the key groups, and each node's
verdict with its current figures. A key a node has not reported is absent from its `figures`,
never zero, so the command reads not-reported the same way the page does.

## Walkthrough

From Home, open the tile, read the header, and open a node's row onto its platform component.

::walkthrough{#read-platform-health}
