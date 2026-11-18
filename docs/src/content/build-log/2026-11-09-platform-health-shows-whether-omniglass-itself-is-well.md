---
title: "Platform health shows whether Omniglass itself is well (#1644)"
date: 2026-11-09
refs: [1644, 1643, 1625, 1626, 1627, 1646, 523]
---

The console has a [Platform health](/guides/operator/platform-health/) page at `/platform`, behind
`node:read`, reached from the sidebar under Nodes and from a new Platform tile on Home that replaces
the Collectors placeholder. Its header names the build the server runs, how long it has been up and
the platform system's verdict, linking to that system's view; its table has one row per node, with
the Nodes page's liveness pill judged at the view's read time and one column per platform key,
grouped as Checkin, Schedule, Delivery and Commands. A row opens the node's platform component,
where each figure is a series with its history. Every figure is a sample on a platform component the
fleet already had (#1625, #1626, #1627): the page reads them through one composed read,
`GET /views/platform`, which gathers the whole platform in a fixed number of statements and returns
the column groups with each key's catalog description, so the page holds no key list and no
threshold. A count reported only when nonzero would show only while its window is the newest, so a
drop from last week never reads as today's; every windowed count is now written every window, zero
when quiet, so none ages out. A boot now writes `server-version` and `server-started-at` on the server's platform
component, keys only the boot may write. A caller whose component read is short of fleet-wide sees
the rows with their pills and a line saying the figures need a fleet-wide read; the figures are left
out, never zeroed. The page is a page, not a dashboard
([ADR-1644](/architecture/decisions/#adr-1644-platform-health-is-a-page-behind-noderead-not-a-dashboard)):
the Loss group is #1646 and the dashboard conversion is #523.
