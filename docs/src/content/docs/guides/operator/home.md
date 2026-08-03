---
title: Home, the situation room
description: "The first page after sign-in: what your estate looks like right now, live, and where each panel's numbers come from."
screenshots:
  - id: home
    path: /web/
    alt: "Home: stat tiles for components and interfaces, a reachability grid, and a table of recent occurrences."
    # The feed's timestamps move with every seed, so masking keeps the image
    # byte-stable for the freshness gate.
    mask:
      - "text=/[A-Z][a-z]{2} \\d+, \\d{2}:\\d{2}/"
---

**Home** is the first page after you sign in, and it answers one question: what does my estate look like
right now. Everything on it is **live**. When a device stops answering or an event lands, the page updates
itself; there is nothing to refresh and no polling interval to wait out.

::screenshot{#home}

Everything you see is **scoped to you**. Each panel counts and lists only what your grants cover, so two
operators signed in at the same time see two different pages, and neither sees an estate they are not
responsible for. Nothing here is a summary of the whole install unless your grants cover the whole install.

## The tiles

The four tiles across the top are the size and health of what you can see:

- **components** and **interfaces**: how much estate is in your scope. An interface is one way of reaching a
  component (a ping target, a port, an API), so a component often has several.
- **reachable** and **unreachable**: how those interfaces currently answer. These two do not have to add up
  to the interface count: an interface nobody has probed yet is neither, and it is counted in neither tile.

## Reachability

The grid below the tiles is one chip per interface, labelled `component / interface` so two ways into the
same device stay distinguishable, and coloured by the latest verdict:

- **up**: the last probe answered.
- **down**: the last probe did not.
- **unknown**: nothing has probed it yet. This is a real state, not a blank: an interface nobody is watching
  is a gap in your coverage, and the grid says so rather than leaving it out.

The verdict is the **latest reading by observation time**, not by arrival time, so a late-arriving probe
result cannot overwrite a newer one.

## Recent occurrences

The table is the newest events across your scope, newest first: a call starting, a device reporting a
condition, anything the platform caught or derived. It is a live tail, not a search; when you need to look
further back or filter, the component's own **Events** panel and the event surfaces are where that lives.

## Where the numbers come from

Every panel on this page is a **[view](/architecture/views/)**: a named, parameterized, scope-checked query
the platform ships, run through a renderer. The page itself holds no queries of its own. That is worth
knowing for two reasons. The same views are available through the API and the CLI, so anything you can see
here you can also script (`omniglass view run component-reachability`). And when a view gains a column, every
surface reading it gains that column too, so the console cannot quietly fall behind what the platform knows.

## If a panel shows an error

A panel that cannot load says so in place, and the rest of the page keeps working. The usual cause is
permissions: the event feed needs `event:read` and the reachability grid and tiles need `component:read`, so
a role missing one of them sees that panel refuse while the others render. The message names the permission
the view required.
