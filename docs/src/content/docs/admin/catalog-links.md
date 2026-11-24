---
title: Follow the catalog
description: "Every reference in a catalog drawer is a link to the row it names, and each drawer says what uses it: a product's components, a driver's products, the rules that read a metric. What a product's driver collects, and how a link is addressed."
sidebar:
  label: Follow the catalog
  order: 9
  badge:
    text: Built
    variant: success
---

The catalog is a web of references: a product names its vendor, its driver and its component type; a rule
names the severity it raises at and the key it reads; an action rule names the notification type it sends
and the targets it sends to. In the console each of those is a **link** that opens the row it names, and
each drawer lists, under **Used by**, the rows that point back at it. Following a product to the components
that are that product takes two clicks and no search.

## What links

- **A product** links its [vendor](/admin/products/#vendors), its [driver](/admin/drivers/) and its component type.
- **A component type, a system type and a standard** link their parent (a standard's **Variant of**).
- **A location type** links each type it may sit under; a **slot** links the component types it accepts and
  the products it is pinned to.
- **A command type** links the property or metric it sets.
- **An event rule** links its severity level, the metric or property it reads, the event types it writes and
  the component types it watches. A **calc rule** links the keys it reads and the one it writes.
- **An action rule** links its notification type and its targets. A **notification target** links the secret
  that signs it, the person, group or role it tells, and the action rules that send to it.

A link opens the list page that owns the row with its drawer already open, at the row's address
(`?open=<name>`), so the same address works as a bookmark, a pasted link, and a Back step. A reference that
names a row the catalog no longer has reads as its handle and still links; the page it lands on says the
row is not found.

## Used by

The **Used by** section lists, for each kind of user, a count and the first few rows as links. A longer
list folds behind **Show all**, and a fleet list (components, systems) shows the first few with **See all**,
which opens Explore filtered to the same set.

| Drawer | Used by |
|---|---|
| Product | the components that are this product |
| Driver | its products, and the components it serves |
| Vendor | its products |
| Component type | its products, and the components of those products |
| Standard | the systems built to it |
| Metric, property | the products that declare it, the drivers that collect it, the rules that read it |
| Command type | the drivers that offer it |
| Severity level | the event rules that raise at it |
| Notification type | the action rules that send it |

The component and system counts are the number of rows **you can see**: the console asks the list route for
the count and a first page (`GET /components?product=<name>&limit=5`, and likewise `driver`,
`component_type`, and `GET /systems?standard=<name>`), and the server applies your scope before it counts.
The metric and property drawers ask the product list which products declare the key
(`GET /products?declares_metric=<name>` or `declares_property`). The rules and drivers are catalog rows the
console already holds.

## Collected by its driver

A product's **contract** (its declared properties and metrics) is what every component of the product is
expected to carry. It is not what a component will actually report: that is whatever the product's
**driver** emits. A product with no declared metrics can still report several, because its driver polls them.
So a product's drawer carries a second section, **Collected by its driver**: the metrics and properties the
driver's polls and listeners emit, and the command types it binds, each a link. A key the driver emits that
neither catalog names (a spec that has run ahead of the catalog) is listed under **Not in the catalogs**,
as text, because a link to nothing would be wrong. The driver's own drawer shows the same section as
**Collects and offers**.

## Written by

A metric or property names its writer. When a [calc rule](/admin/calc-rules/) writes the key, **Written by**
is that rule, linked. Otherwise it is the class of writer the API reports: **driver** (a driver's emits, a
push, a command's or an operator's value), **outpost probe** (an outpost's own probe or session), or
**platform** (the Omniglass process itself). The metrics and properties lists carry the same column.
