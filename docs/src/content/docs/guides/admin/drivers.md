---
title: Drivers
description: "The Drivers catalog: each driver's declarative spec (the menu one engine interprets), official rows read-only, admin-gated custom ones, and the attach flow that consumes them."
---

**Catalog, under Components: Drivers** (`/drivers`, with `driver:read`, covered by every viewer's `*:read` floor)
is the directory of **drivers**: the normalized menu that turns a device family's API into
what to fetch, how to parse it, and what actuates,
on the same flat-registry pattern as [Location Types](/guides/admin/location-types/) and [Tags](/guides/admin/tags/).
Where a [vendor](/guides/admin/vendors/) names who a device comes from, a driver names how it is
talked to (for example `Generic SNMP` or `Newtron NVP`). Each row shows the **name** (the
operator-facing name, for example `snmp-generic`), the **label**, an optional
**version**, and its **origin** (**official** or **custom**). A driver also carries
an `id`, a uuid minted by the database, the internal address the handle resolves to
([ADR-0062](/architecture/decisions/)); the handle is what you type and read.

A driver's body is its **spec** ([ADR-0135](/architecture/decisions/#adr-0135-a-drivers-spec-is-data-one-engine-interprets-it)):
a versioned declarative document (data, not code) naming the transport it rides, the **inputs**
an attach must supply (host, port, credentials as secret references), and its three function
families (poll functions, listeners, command bindings). The detail blade renders the spec as
the **menu it is**: what each poll asks and what lands, what each listener waits for, what each
command binding actuates. On a line protocol a listener's **arm** commands (the subscribe
conversation) are sent on every connection the node holds, its **match** rule claims the
device's pushes, and each claimed line lands as a sample with no poll involved; a request may
declare its **reply** shape (`request.reply`, a `prefix` or `regex`) so its answer is picked out of
those pushes, plus an optional `error` shape (`reply.error`) for the device's refusals, which then
answer the request as a failure carrying the device's text instead of timing out. Every `regex` in
a spec is Go RE2 with **substring** semantics: `INPUT \S+` also matches `EVT INPUT hdmi2`, so anchor
a pattern meant to match a whole line (`^INPUT \S+$`). `unmatched: collection-failed` makes a line
nobody claims a collection fault rather than a counted discard ([routing](/architecture/collection/#listeners-over-a-held-session)).
A poll's `schedule.every` is how often the node asks the device, at the node's heartbeat
granularity (a `60s` poll on a 30s heartbeat runs every other tick). A poll whose fetch fails
(the device rebooting, say) retries sooner, on a backoff that starts at the heartbeat and doubles
up to the interval, so a daily poll is not lost for a day. A listener's pushes land as they arrive
whatever the schedule. Changing a spec (or its version) reaches the endpoints already attached through it: their tasks
re-derive in the update's own transaction, under its one audit row, and the next poll runs the
new spec; an official spec a new release ships reaches them at boot the same way.
A row without a spec is a **stub** and cannot be attached; specs are
authored over the API or CLI (`omniglass driver create --spec ...`) and **validate at write**,
so a spec that references an unregistered sample name, an unknown secret shape, or a broken
template refuses with the fault named (422). Attaching a driver to a component happens on the
component's own detail ([nodes and reachability](/guides/operator/collection/)): the spec
derives the endpoint and its tasks.

A driver is consumed by the [product](/guides/admin/products/) catalog: a `product` references
its driver through an optional `driver_id` to say which driver reads it, chosen from a driver
picker on the product's create and edit forms, and three of the shipped official products bind a
driver this way. It is a leaf catalog beside the vendor registry. See
[core entities](/architecture/core-entities/) for where it sits in the fleet model.

- **New driver** (with `driver:create`, an admin permission) opens a create drawer: give it a
  **name** (unique tenant-wide, e.g. `snmp-generic`), a **label**, and,
  optionally, a **version**.
- Pick a row to open its **detail blade**. The footer **Edit** pencil (with `driver:update`) edits
  the label and version; the **name** is fixed, since a catalog row carries no rename
  (`:rename` is a component, system, location, and principal group affordance). **Delete**
  (with `driver:delete`) removes the row, behind a confirm. A verb you lack greys just that
  button, its hover reason naming the permission (`Requires driver:update`, `Requires driver:delete`);
  the pair never disappears.
- An **official** row is always read-only: the blade keeps the Edit and Delete pair in place,
  greyed, with the reason on hover: "Official: ships with Omniglass and updates with it."
  Omniglass ships a starter set of official drivers (Generic SNMP and Newtron NVP with real
  specs, Kestrel Device API and HTTP JSON as stubs), upserted idempotently at boot so the
  shared set cannot drift install to install; add a custom driver for anything else.
- **Delete** refuses while an **endpoint is attached** through the driver (422): an attach
  derives real collection work from the spec, so the driver cannot vanish out from under it.
  The [product](/guides/admin/products/) link stays soft: `product.driver_id` is
  `on delete set null`, so deleting an unattached driver detaches it from those products
  (their driver clears) rather than blocking. Official rows stay refused outright (422).

Minting a driver is admin-gated; the picker that consumes it lives on the
[product](/guides/admin/products/) create and edit forms. The same operations are `omniglass driver
list/get/create/update/delete` from the CLI (see the [CLI reference](/reference/cli/)).
