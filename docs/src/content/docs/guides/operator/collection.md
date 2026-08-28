---
title: Nodes and reachability
description: "Enrolling a collection node, adding a transport-named endpoint to a component, and reading the per-endpoint reachability and the log events a node reports."
---

Collection is how the fleet learns whether a device is reachable and what it reports. An **edge
node** runs the probes, a component's **endpoint** is the API the node reaches for, the component's
**Reachability** panel shows the verdict, and its **Events** and **Logs** panels show recent occurrences
the node ships back. This page walks the console surfaces; the model behind them is
[data collection](/architecture/collection/), and every action here has the same
[scope](/guides/operator/inventory/) and permission checks as the rest of the console.

## Nodes

**Nodes** in the sidebar (with `node:read`, which must be **all-scope**, since a node is
fleet-wide, so a location-scoped operator cannot list nodes) is the collection-daemon
inventory. Each row reads the way every list in the console reads: the node's **label** on the
first line and its **name** beneath it (shown only when the two differ), then a **liveness pill** (up,
down, or never, derived from its last heartbeat against the server's down window), the relative
last-heartbeat time, and its tags. A row opens the node's detail.

- With `node:create` and `node:enroll`, **New node** registers a node (the name is its
  fleet address) and mints its **enrollment token**. The form also takes an optional
  **label** and **location**. The token is a secret shown **once**, in a
  copy-to-clipboard field with a "shown once, cannot be retrieved again" warning. Copy it now
  and hand it to the node deployment; the node presents it to claim its NATS credential. The
  server stores only a hash of the token and never logs it.
- The detail is **read-edit-save**, like a component or location. With `node:update`, **Edit**
  changes the node's **label**, **description**, and **location** (a descriptive
  placement picked from the fleet's locations, not a scope); the **name is immutable** (it is
  the fleet address and enrollment identity). The location clears if that location is deleted.
- The detail carries a **Tags** panel: with `node:update`, edit mode adds and removes governed
  [tags](/guides/operator/inventory/) (keys whose vocabulary allows nodes), the same tag editor the
  component and location details use. The node list shows a Tags column and filters by any tag key.
- With `node:delete`, **Delete** (the destructive action, left of the footer) **decommissions** the node after a confirm: its endpoints, derived tasks, tags, and enrollment are removed. The telemetry it collected for components stays.
- **Enroll** (or **Re-enroll**, if it is already enrolled) is a secondary action in the detail's
  kebab: it re-mints the token, invalidating the previous one.
- The detail also shows whether the node is enrolled and when it last sent a heartbeat.

The node detail also carries a read-only **Self-logs** panel (backed by `GET /nodes/{name}/logs`,
gated by `node:read`): the node's own recent operational log lines, newest first, over the last
24 hours, so a node that is up but misbehaving explains itself without a shell on the box.

## Endpoints

An endpoint is an **API on a component** that a node reaches for, and it lives **on the
component**: there is no standalone Endpoints surface. Open a component from the Fleet
list's **Components** tab (with `endpoint:read`) and its endpoints read as a panel on the
detail, each
showing the endpoint's **label** over its transport name, its reachability, its node placement, and
its probed target. An endpoint is **named by its transport**: you pick the wire it speaks over and
the endpoint takes that transport as its name, unique within its component, so one component can
have one `tcp` and one `http`, and a second endpoint of a transport it already has is refused.
Because you never type that name, the **label** is the one string on an endpoint that is yours:
give it one ("Control processor") to say what the connection is FOR, since `ssh` only says how it is
reached and reads the same on every component in the fleet. It is optional, and an endpoint
without one reads its transport name exactly.

- With `endpoint:create`, **Add endpoint** on the component detail creates one, in either of
  two faces:
  - **Probe**: give it a **label** (optional, and the only name-like string you type here),
    choose a **transport** (the picker reads the code registry the binary ships,
    `GET /transports`: `icmp` and `tcp` probe layers 3 and 4, `ssh` and `http` climb to
    layer 7 (the probe draws a real response, so reached-but-not-responsive and
    responded-but-not-authenticated are visible states), `udp` and `snmp` have no standalone
    probe; there is no free-text name), a node placement, and a target (`host:port` for the
    tcp-family transports, `host` for icmp).
  - **Attach a driver**: pick a **driver** (only drivers whose declarative spec exists are
    offered) and fill the **inputs** its spec declares: a host, a port with its default
    pre-filled, credentials as **secret references** (the name of a secret of the declared
    shape, never a value). The spec derives the transport, the target, and the endpoint's
    tasks: a poll task per poll function, a standing listen task per listener. The
    [Drivers](/guides/admin/drivers/) page shows each spec's menu before you attach it.

  The owning component is the one you are on. Either face **derives the endpoint's tasks**
  for you, so a fresh endpoint is working collection with no second step.
- With `endpoint:update`, editing an endpoint changes only its **node placement** and its
  **target**; the transport (and so the name) is fixed at creation.
- With `endpoint:delete`, deleting an endpoint removes it and **cascades its derived task**.

Because an endpoint belongs to a component, it inherits that component's scope: an endpoint
on a component outside your scope is not shown. A node **purge cascades** its endpoints and
their derived tasks.

## Tasks

A task is the **collection work** a node runs, and it is **derived**, not authored: creating an
endpoint creates its one poll task. A task has **no name**: it is a binding, a **function**
running over an **endpoint**, so it reads as its endpoint (the anchor) plus that function,
never a redundant label. There is no standalone Tasks surface. A node's derived tasks read as a
**panel on the node's detail** (open a node from **Nodes**, with `task:read`): each
shows its endpoint, the function it runs (the built-in **reachability** check, or a driver
function carried whole in the task's spec: `snmp-generic/scalars`, `newtron-nvp/status`, a
standing listener), and an **enabled** state; the node it runs on follows its endpoint's
placement. To change what a node
collects, add or remove the **endpoint**; there is no task create, edit, or delete.

## Reachability

Every component's detail carries an **Endpoints** panel showing composed reachability: is each
of its endpoints reachable, and why. One row per endpoint shows the endpoint (its label, or its
transport name where it has none) and its address, a **verdict
pill** (responding, down, stale, or unknown), an **availability strip** drawn from the
verdict's up/down transitions over time, and an expandable **gate breakdown** with each probe's signal and timing, then the composed
verdict (the endpoint is up only when every applicable probe passed). An `http` or `ssh`
endpoint also wears its upper rungs as chips once a probe has climbed them: **responds** (the
API drew a real answer) or **no response** (the port accepted but the service never spoke),
and for `ssh` **auth ok** / **auth failed** (shown only when a credential was actually
tried). A down endpoint also shows a plain-language **why** line. Every value is a real reading from the node, and the panel
is also the authoring surface: its header carries **Add endpoint** (with `endpoint:create`)
and each row that maps to an endpoint a **Manage** affordance opening that endpoint's detail.

To author a reachability check, add an **endpoint** to the component (above), from this
panel's own header: a bare probe endpoint is the reachability check, and **attaching a
driver** is the authoring flow for real collection (the spec's functions become the
endpoint's tasks, and their samples land on the component). There is no standalone
Endpoints page.

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
