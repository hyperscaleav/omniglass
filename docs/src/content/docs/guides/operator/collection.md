---
title: Nodes and reachability
description: "Enrolling a collection node, adding a protocol-named interface to a component, and reading the per-interface reachability and the log events a node reports."
---

Collection is how the estate learns whether a device is reachable and what it reports. An **edge
node** runs the probes, a component's **interface** is the API the node reaches for, the component's
**Reachability** panel shows the verdict, and its **Events** and **Logs** panels show recent occurrences
the node ships back. This page walks the console surfaces; the model behind them is
[data collection](/architecture/collection/), and every action here has the same
[scope](/guides/operator/inventory/) and permission checks as the rest of the console.

## Nodes

**Inventory > Nodes** (with `node:read`, which must be **all-scope**, since a node is
estate-wide, so a location-scoped operator cannot list nodes) is the collection-daemon
inventory. Each row reads the way every list in the console reads: the node's **label** on the
first line and its **name** beneath it (shown only when the two differ), then a **liveness pill** (up,
down, or never, derived from its last heartbeat against the server's down window), the relative
last-heartbeat time, and its tags. A row opens the node's detail.

- With `node:create` and `node:enroll`, **New node** registers a node (the name is its
  estate address) and mints its **enrollment token**. The form also takes an optional
  **label** and **location**. The token is a secret shown **once**, in a
  copy-to-clipboard field with a "shown once, cannot be retrieved again" warning. Copy it now
  and hand it to the node deployment; the node presents it to claim its NATS credential. The
  server stores only a hash of the token and never logs it.
- The detail is **read-edit-save**, like a component or location. With `node:update`, **Edit**
  changes the node's **label**, **description**, and **location** (a descriptive
  placement picked from the estate's locations, not a scope); the **name is immutable** (it is
  the estate address and enrollment identity). The location clears if that location is deleted.
- The detail carries a **Tags** panel: with `node:update`, edit mode adds and removes governed
  [tags](/guides/operator/inventory/) (keys whose vocabulary allows nodes), the same tag editor the
  component and location details use. The node list shows a Tags column and filters by any tag key.
- With `node:delete`, **Delete** (the destructive action, left of the footer) **decommissions** the node after a confirm: its interfaces, derived tasks, tags, and enrollment are removed. The telemetry it collected for components stays.
- **Enroll** (or **Re-enroll**, if it is already enrolled) is a secondary action in the detail's
  kebab: it re-mints the token, invalidating the previous one.
- The detail also shows whether the node is enrolled and when it last sent a heartbeat.

The node detail also carries a read-only **Self-logs** panel (backed by `GET /nodes/{name}/logs`,
gated by `node:read`): the node's own recent operational log lines, newest first, over the last
24 hours, so a node that is up but misbehaving explains itself without a shell on the box.

## Interfaces

An interface is an **API on a component** that a node reaches for, and it lives **on the
component**: there is no standalone Interfaces surface. Open a component from **Inventory >
Components** (with `interface:read`) and its interfaces read as a panel on the detail, each
showing the interface's **label** over its protocol name, its reachability, its node placement, and
its probed target. An interface is **named by its protocol**: you pick a **type** (the transport) and
the interface takes that protocol as its name, unique within its component, so one component can
have one `tcp` and one `http`, and a second interface of a protocol it already has is refused.
Because you never type that name, the **label** is the one string on an interface that is yours:
give it one ("Control processor") to say what the connection is FOR, since `ssh` only says how it is
reached and reads the same on every component in the estate. It is optional, and an interface
without one reads its protocol name exactly.

- With `interface:create`, **Add interface** on the component detail creates one: give it a
  **label** (optional, and the only name-like string you type here), choose a
  **type** (the console picker offers `icmp` and `tcp` today; the `ssh` and `http` types exist
  through the API and CLI, probing as a tcp connect; there is no free-text name),
  a node placement, and a target (`host:port` for the tcp-family transports, `host` for icmp).
  The owning component is the one you are on. Creating an interface **derives its poll task**
  for you, so a fresh interface is a working reachability check with no second step.
- With `interface:update`, editing an interface changes only its **node placement** and its
  **target**; the type (and so the protocol name) is fixed at creation.
- With `interface:delete`, deleting an interface removes it and **cascades its derived task**.

Because an interface belongs to a component, it inherits that component's scope: an interface
on a component outside your scope is not shown. A node **purge cascades** its interfaces and
their derived tasks.

## Tasks

A task is the **collection work** a node runs, and it is **derived**, not authored: creating an
interface creates its one poll task. A task has **no name**: it is a binding, a **function**
running over an **interface**, so it reads as its interface (the anchor) plus that function,
never a redundant label. There is no standalone Tasks surface. A node's derived tasks read as a
**panel on the node's detail** (open a node from **Inventory > Nodes**, with `task:read`): each
shows its interface, the function it runs (today the built-in **reachability** check, with a
provisional marker since named collection functions arrive with device drivers), and an
**enabled** state; the node it runs on follows its interface's placement. To change what a node
collects, add or remove the **interface**; there is no task create, edit, or delete.

## Reachability

Every component's detail carries an **Interfaces** panel showing composed reachability: is each
of its interfaces reachable, and why. One row per interface shows the interface (its label, or its
protocol name where it has none) and its endpoint, a **verdict
pill** (responding, down, stale, or unknown), an **availability strip** drawn from the
verdict's up/down transitions over time, and an expandable **gate breakdown** (the L3/L4 ping
and port probes this slice ships) with each probe's signal and timing, then the composed
verdict (the interface is up only when every applicable probe passed). A down interface also
shows a plain-language **why** line. Every value is a real reading from the node, and the panel
is also the authoring surface: its header carries **Add interface** (with `interface:create`)
and each row that maps to an interface a **Manage** affordance opening that interface's detail.

To author a reachability check, add an **interface** to the component (above): a proper
driver-based authoring flow is a later collection slice, so today a check is an interface plus
its derived poll task, created from this panel on the component's own detail (there is no
standalone Interfaces page).

## Events

Alongside sampled readings, a component carries two occurrence panels, both read-only and gated by
`component:read`. The **Events** panel shows the most recent typed **events**, newest first, over the
last 24 hours (capped at 200): discrete things that *happened* (a `call-started`) that a component
published natively or a rule derived, each row showing its **time**, the **event type** it is typed by,
the **message**, and any structured **attributes**. Below it, the **Logs** panel shows the component's raw **log lines**
(the ingest lane, [ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events)):
untyped device text a rule may later derive events from, each row showing its **time**, a **severity**
badge, the **facility** and **source**, the **message**, and its structured **fields** on demand. Most
log lines never become events.

Where a reachability verdict is a sampled state, an event or a log line is a past occurrence, so the
panels are read differently: the verdict answers "is it reachable *now*", the logs and events answer
"what did it *say*, and when". Both land under the same [scope](/guides/operator/inventory/) and owner
checks as every other reading, so an out-of-scope component's events and logs are a non-disclosing 404,
exactly like its reachability. Every value is a real occurrence from the node.
