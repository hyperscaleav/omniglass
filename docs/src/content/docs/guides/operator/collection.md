---
title: Nodes and reachability
description: "Enrolling a collection node, adding a protocol-named interface to a component, and reading the per-interface reachability a node reports."
---

Collection is how the estate learns whether a device is reachable. An **edge node** runs the
probes, a component's **interface** is the API the node reaches for, and the component's
**Reachability** panel shows the verdict. This page walks the console surfaces; the model
behind them is [data collection](/architecture/collection/), and every action here has the
same [scope](/guides/operator/inventory/) and permission checks as the rest of the console.

## Nodes

**Inventory > Nodes** (with `node:read`, which must be **all-scope**, since a node is
estate-wide, so a location-scoped operator cannot list nodes) is the collection-daemon
inventory. Each row shows the node's name, a **liveness pill** (up, down, or never, derived
from its last heartbeat against the server's down window), the relative last-heartbeat time,
and a description. A row opens the node's detail.

- With `node:create` and `node:enroll`, **New node** registers a node (the name is its
  estate address) and mints its **enrollment token**. The token is a secret shown **once**,
  in a copy-to-clipboard field with a "shown once, cannot be retrieved again" warning. Copy
  it now and hand it to the node deployment; the node presents it to claim its NATS
  credential. The server stores only a hash of the token and never logs it.
- From a node's detail, **Enroll** (or **Re-enroll**, if it is already enrolled) re-mints the
  token, invalidating the previous one.
- The detail also shows whether the node is enrolled and when it last sent a heartbeat.

## Interfaces

An interface is an **API on a component** that a node reaches for, and it lives **on the
component**: there is no standalone Interfaces surface. Open a component from **Inventory >
Components** (with `interface:read`) and its interfaces read as a panel on the detail, each
showing the interface's protocol name, its reachability, its node placement, and its probed
target. An interface is **named by its protocol**: you pick a **type** (the transport) and the
interface takes that protocol as its name, unique within its component, so one component can
have one `tcp` and one `http`.

- With `interface:create`, **Add interface** on the component detail creates one: choose a
  **type** (the built types are `icmp`, `tcp`, `ssh`, and `http`; there is no free-text name),
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
interface creates its one poll task. There is no standalone Tasks surface. A node's derived
tasks read as a **panel on the node's detail** (open a node from **Inventory > Nodes**, with
`task:read`): each shows its interface, its mode (`poll`), and an **enabled** state, and the
node it runs on follows its interface's placement. To change what a node collects, add or
remove the **interface**; there is no task create, edit, or delete.

## Reachability

Every component's detail carries a read-only **Reachability** panel: is each of its interfaces
reachable, and why. One row per interface shows the interface and its endpoint, a **verdict
pill** (responding, down, stale, or unknown), an **availability strip** drawn from the
verdict's up/down transitions over time, and an expandable **gate breakdown** (the L3/L4 ping
and port probes this slice ships) with each probe's signal and timing, then the composed
verdict (the interface is up only when every applicable probe passed). A down interface also
shows a plain-language **why** line. The rows are read-only and every value is a real reading
from the node.

To author a reachability check, add an **interface** to the component (above): a proper
driver-based authoring flow is a later collection slice, so today a check is an interface plus
its derived poll task, created from the Interfaces page.
