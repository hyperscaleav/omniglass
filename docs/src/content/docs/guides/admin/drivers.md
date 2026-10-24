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
A spec may declare a **session** block, the auth step the held session runs before anything rides
it ([auth and rejection](/architecture/collection/#auth-and-rejection)). `session.login` is a login
dialog: its `steps` each wait for a prompt (`expect`, a `prefix` or `regex` that may match a prompt
with no line ending, such as `login: `) and `send` a line, which may name a secret input's field
(`${secret.login.password}`, the input declared `kind: secret`), and its `ready` match says the
login passed. `session.terminal` lists the replies retrying cannot fix, each a `match` with a
`class` (`auth`, `host-key`, `certificate`, `protocol`): a line matching one, during the login or
answering any request, moves the session to `rejected`, where the node attempts nothing more until
the endpoint's connection fingerprint changes (its secret is rotated, its address, port or transport
changes, the driver's login declaration changes) or an operator retries it (**Retry** on the
rejected session's notice in the console, or `omniglass endpoint retry <id>`, with
`endpoint:retry`); a label edit changes nothing. `session.reprobe`
(at least `5m`, default never) lets a rejected session try once more on its own. A wrong password the
component answers with a declared refusal then costs one attempt rather than a retry every 30 seconds;
a component that hangs up on a wrong password without saying so costs three (the login budget), and
then reads `rejected` as `auth`. Anchor a prompt's regex (`^login: ?$`): a prompt is matched on a line
the component pauses on, and an unanchored one also matches a banner such as `Last login: `. A spec
whose session only commands would ride (no poll or listener to hold it) refuses at write. Over `ssh`
the session's auth step is the transport's own: `session.credential` names the secret input it
authenticates with (required: a `basic-auth` username and password, or an `ssh-key` username and
private key; a `session.login` beside it only answers keyboard-interactive questions, and a question
nothing declared answers, such as a one-time code, rejects the session), `session.pty` asks for a
terminal and `session.exec` runs a command as the channel instead of a shell. The host key is trusted
on first use and kept as the endpoint's `ssh-host-key`; when it changes the session reads `rejected`
naming both keys, which the gate carries as data (`endpoint-rejection` ends with the pinned and the
offered key). The console's **Trust this key** on the session's notice trusts the offered one after
a confirm ([held sessions](/guides/operator/collection/#a-changed-key)), and so does
`omniglass endpoint retry <id> --host-key <the offered key>`. While the refusal holds, the key must be
the offered one it names (any other is refused, 409, and changes nothing; retry without a key first
to have the node read a key the component presents since); with no refusal held, `--host-key`
declares a pin ahead of time, for a replacement whose key you already know.

A spec may also declare its **reachability ladder** (`ladder`), bottom up: `ladder.ping.enabled`
(whether to ping at all; a ping is informational only and never gates), `ladder.transport.rung` (the
gate: `connect` on the transport's port, the protocol's `handshake`, or a named `request` answered,
given as `ladder.transport.request`, where an open port cannot be detected; it runs as a line over
`tcp` today, with snmp's own request #821 and udp's #1037) and
`ladder.session.enabled` (whether a login rung exists). Every field defaults from the transport
registry, so most drivers declare nothing, and a rung the transport cannot run refuses at write. A
cloud API is never pinged, but its 443 is checked open before its session runs:

```yaml
transport: http
ladder:
  ping: { enabled: false }
  transport: { rung: connect }
```

A refusal at a rung gates that rung and the ones above it for the endpoint; the rungs below keep
reporting ([the gate](/architecture/collection/#the-connection-fingerprint-and-the-layered-gate)).
A telnet driver:

```yaml
session:
  login:
    steps:
      - { expect: { regex: "^login: ?$" }, send: "${secret.login.username}" }
      - { expect: { regex: "^Password: ?$" }, send: "${secret.login.password}" }
    ready: { prefix: "Newtron Panel 7 NVP ready" }
  terminal:
    - { class: auth, match: { prefix: "auth rejected" } }
  reprobe: 1h
```

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
derives the endpoint and its tasks. Its inputs (the host, the port, an attached secret's name) then
edit in place on the endpoint's blade or with `omniglass endpoint update <id> --inputs ...`, checked
against the spec's declared inputs; no re-attach. Clearing an optional input restores its default;
a required one cannot be cleared.

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
  Omniglass ships a starter set of official drivers (Generic SNMP, Newtron NVP, and Newtron NVP
  over Telnet, its login a session block, with real specs, Kestrel Device API and HTTP JSON as stubs), upserted idempotently at boot so the
  shared set cannot drift install to install; add a custom driver for anything else.
- **Delete** refuses while an **endpoint is attached** through the driver (422): an attach
  derives real collection work from the spec, so the driver cannot vanish out from under it.
  The [product](/guides/admin/products/) link stays soft: `product.driver_id` is
  `on delete set null`, so deleting an unattached driver detaches it from those products
  (their driver clears) rather than blocking. Official rows stay refused outright (422).

Minting a driver is admin-gated; the picker that consumes it lives on the
[product](/guides/admin/products/) create and edit forms. The same operations are `omniglass driver
list/get/create/update/delete` from the CLI (see the [CLI reference](/reference/cli/)).
