---
title: Drivers
description: "The Drivers catalog: official rows read-only, admin-gated custom ones, attaching a driver to an endpoint, built-in and collected data, records and identity, and checking a spec."
sidebar:
  order: 11
  badge:
    text: Built
    variant: success
---

**Catalog, under Components: Drivers** (`/drivers`, with `driver:read`, covered by every viewer's `*:read` floor)
is the directory of **drivers**: the normalized menu that turns a device family's API into
what to fetch, how to parse it, and what actuates,
on the same flat-catalog pattern as [Location Types](/admin/location-types/) and [Tags](/admin/tags/).
Where a [vendor](/admin/products/#vendors) names who a device comes from, a driver names how it is
talked to (for example `Generic SNMP` or `Newtron NVP`). Each row shows the **name** (the
operator-facing name, for example `snmp-generic`), the **label**, an optional
**version**, and its **origin** (**official** or **custom**). A driver also carries
an `id`, a uuid minted by the database, the internal address the handle resolves to
([ADR-0062](/architecture/decisions/)); the handle is what you type and read.

A driver's body is its **spec**, a versioned declarative document (data, not code) that one engine
interprets. Writing one is driver-author work, covered by [the driver spec](/developer/driver-spec/);
this page is what an admin does with the catalog around it.

## The catalog

A driver's drawer lists the products that use it and the components it serves, and links each metric,
property and command type its spec names: see [Follow the catalog](/admin/catalog-links/).

A driver is consumed by the [product](/admin/products/) catalog: a product references its driver
through an optional `driver_id`, chosen from the driver picker on the product's create and edit forms,
and three of the shipped official products bind a driver this way. See
[core entities](/architecture/core-entities/) for where it sits in the fleet model.

- **New driver** (with `driver:create`, an admin permission) opens a create drawer: give it a
  **name** (unique tenant-wide, e.g. `snmp-generic`), a **label**, and,
  optionally, a **version**.
- Pick a row to open its **detail blade**. The footer **Edit** pencil (with `driver:update`) edits
  the label and version. The blade does not edit the **name**: rename a custom driver with
  `omniglass driver rename <name> --name <new>` (`driver:rename`), which keeps every product and
  endpoint that uses it and re-derives the endpoints' tasks under the new name, as a version change
  does. **Delete**
  (with `driver:delete`) removes the row, behind a confirm. A verb you lack greys just that
  button, its hover reason naming the permission (`Requires driver:update`, `Requires driver:delete`);
  the pair never disappears.
- An **official** row is always read-only: the blade footer shows one note, "Official, read-only", with the reason on hover: "Official: ships with Omniglass and updates with it."
  Omniglass ships a starter set of official drivers, each with a real spec (the set the binary
  seeds is rendered under [the declarative menu](/architecture/collection/#drivers-the-declarative-menu)),
  upserted idempotently at boot so the
  shared set cannot drift install to install; add a custom driver for anything else.
- **Delete** refuses while an **endpoint is attached** through the driver (422): an attach
  derives real collection work from the spec, so the driver cannot vanish out from under it.
  The [product](/admin/products/) link stays soft: `product.driver_id` is
  `on delete set null`, so deleting an unattached driver detaches it from those products
  (their driver clears) rather than blocking. Official rows stay refused outright (422).

Minting a driver is admin-gated; the picker that consumes it lives on the
[product](/admin/products/) create and edit forms. The same operations are `omniglass driver
list/get/create/update/delete` from the CLI (see the [CLI reference](/reference/cli/)).

## Attaching a driver to an endpoint

A row without a spec is a **stub** and cannot be attached. Attaching happens on the component's own
detail ([outposts and reachability](/operator/collection/)): the spec derives the endpoint and its
tasks. The endpoint's inputs (the host, the port, an attached secret's name) then edit in place on its
blade or with `omniglass endpoint update <component>/<endpoint> --inputs ...`, checked against the
spec's declared inputs, with no re-attach. Clearing an optional input restores its default; a required
one cannot be cleared.

Changing a spec (or its version) reaches the endpoints already attached through it: their tasks
re-derive in the update's own transaction, under its one audit row, and the next poll runs the new
spec. An official spec a new release ships reaches them at boot the same way. A spec **validates at
write**, so one that references an unregistered sample name, an unknown secret shape, or a broken
template refuses with the fault named (422).

A webhook driver's attach also mints the endpoint's token, the `webhook-token` secret on the
component: reveal it with `omniglass secret reveal <component id>/webhook-token` (audited) and give
the sender `/api/v1/webhooks/<token>` on the server's address and the signing key.
`omniglass endpoint rotateToken <component>/webhook` mints another, and the old URL answers 404 at
once.

## Built-in and collected data

An endpoint carries two kinds of data, and a driver configures only one of them:

- **Built-in data** is what Omniglass knows about the endpoint itself, and every endpoint gets it,
  whatever its driver, because the endpoint cannot work without it:
  - its reachability ladder's readings and timings;
  - a held session's state and reason;
  - the gate and the pins it holds;
  - `collection-failed`;
  - its own collection telemetry, once a report window: how long its polls took and how many failed,
    the pushes its listeners claimed and did not, and for a webhook endpoint the posts the server
    accepted and refused.

  Omniglass produces it from the endpoint's transport. A driver never declares or emits it, and a
  spec that emits one is refused. A driver only tunes how some of it is gathered: its `ladder` ([the driver spec](/developer/driver-spec/))
  picks the reachability rung and whether to ping. The keys are the `outpost-probe` and `platform` writer
  classes, listed from the code on [who writes a key](/architecture/properties/#who-writes-a-key-the-writer-class).
- **Collected data** is what the device's own API says: a volume, a call state, an input. It is
  exactly what the driver's polls and listeners emit, each a **collected** key, and the driver
  decides what is read and how often (a poll's `schedule.every`).

A device that only pushes, or reports little of its own, still carries its built-in data; what makes
it worth watching is what its driver collects.

## Records and identity: one answer about many owners

A cloud service or a controller fronting many devices answers about all of them at once: one
request lists every room, one webhook post names two. A poll or a listener reads such an answer
with two fields:

- `records` names the array in the JSON body (`rooms`, or `data.rooms`). Each element is read as
  its own payload, so every emit (and the identity) reads once per element.
- `identity` names the **match key**, a registered property type such as `ucc-room-id`, and the
  extract that reads its value off each element.

```yaml
polls:
  - name: rooms
    schedule: { every: 30s }
    request: { path: /rooms }
    records: rooms
    identity: { key: ucc-room-id, extract: { jsonpath: id } }
    emits:
      - { name: call-state, extract: { jsonpath: state } }
```

Polled against `{"rooms":[{"id":"r1","state":"in-call"},{"id":"r2","state":"idle"}]}`, that
function yields two `call-state` values, one naming room `r1` and one naming `r2`. An element with
no `id` lands nothing while the others land; however many elements miss, the run reports one fault
counting them and naming the first ten. At most 2048 elements are read from one answer, and one
fault says how many past that were not. A function reading `records` must declare an `identity`
unless it emits only log lines. Log lines carry no
identity and land as before. A save refuses a key that is not a registered property type, an
extract the transport cannot read, and `records` on anything but a JSON body. A
[preview](#check-preview-and-run-a-poll-now) shows each sample's identity.

### Paged lists

A platform with thousands of rooms lists them a page at a time. A poll that reads `records` declares
`paginate` on its request, and one poll walks every page, gathers the records of all of them, then
identifies and fans them out once, exactly as a single answer is. The engine knows no field name:
the driver spells every one, so a platform's own cursor name appears only in its driver.

```yaml
polls:
  - name: rooms
    schedule: { every: 30s }
    request:
      path: /api/rooms
      paginate:
        style: cursor
        size_param: page_size
        size: 100
        cursor_from: next_page_token     # where each answer carries the cursor
        cursor_param: next_page_token    # the query parameter it is sent back in
    records: rooms
    identity: { key: ucc-room-id, extract: { jsonpath: id } }
    emits:
      - { name: call-state, extract: { jsonpath: state } }
```

- `style: cursor`: a value read off each answer is sent on the next request. `cursor_from` is a
  dotted path in the JSON body (`meta.cursor`), or `cursor_from_header` a response header; send it back
  in `cursor_param` (a query parameter) or `cursor_header` (a request header). An absent, null or
  empty cursor is the last page.
- `style: page`: the request sends an ascending number in `page_param`, from `first_page` (1 when
  absent, 0 for a zero-based API). The walk stops at an answer with no records, or sooner when the
  answer declares its total: `pages_from` (the path of the page count) or `total_from` (the path of the
  record count; needs `size`).
- `size_param` and `size` go together and ask for a page size; absent, the platform's default applies.
- Bounds: `max_pages` (default 100, at most 1000) and `max_records` (default 10000, at most 100000)
  bound one poll. Reaching either, a cursor that answers itself, or a page that fails faults the poll
  naming the endpoint and the page, and **nothing lands**: there is no partial fan-out. A paged poll's
  `max_records` replaces the 2048 cap of an unpaged answer.

A page-number API: `paginate: { style: page, page_param: page, first_page: 1, size_param: per_page, size: 100, pages_from: meta.total_pages }`.
`paginate` rides a GET poll that declares `records`; a save refuses it anywhere else.

An identified value lands on the system or component its identity is **bound** to on this
component, never on the component itself ([shared-API
collection](/architecture/collection/#shared-api-collection-one-component-many-owners)). Bind an
identity where its values arrive, on the conduit component:

```bash
omniglass component identity-binding update hq.\$comp.ucc-platform ucc-room-id zr-huddle-north \
  --owner-kind system --owner huddle-north
```

Until it is bound, its values land on no owner and wait in the component's unbound inbox
(`omniglass component unbound-identity list hq.\$comp.ucc-platform`), where each row shows the
match key, the value, how often it was heard and the keys it carried; binding it makes the next value
land on its owner. A binding costs update on both ends: on the conduit component and on the owner.
Only collected keys fan out, and a fanned value lands at the owner's own (empty) instance, its
source naming the conduit and its endpoint (`ucc-platform/http`).

## Check, preview, and run a poll now

Three commands take the guesswork out of writing a spec, each a route an agent can call as an
[MCP tool](/reference/mcp/) too:

```sh
# Check a spec as a save would, writing nothing: every fault, each on its function
omniglass driver validate --spec "$(cat display-web.json)" | jq '.findings[] | {function, message}'

# Run its polls once through an outpost against the component, storing nothing
omniglass driver preview --outpost edge-hq --spec "$(cat display-web.json)" \
  --inputs '{"host":"10.20.4.31","login":"display-login"}' | jq '.results[] | {poll, samples, faults}'

# Once attached, run an endpoint's poll now; its samples land
omniglass endpoint poll run 'hq.west.level-2.huddle.$comp.display-1/http' identity
```

Validate needs only `driver:read`. A preview (`driver:preview`) answers with the samples each poll
extracted, each emit that did not locate, and an excerpt of the raw answer to write the next extractor
against, masked of the secret it used; a secret input must be one you may bind (`secret:bind`), since the preview
hands it to the target you name. A preview runs a poll that is its own request (http, https, snmp, a
tcp line); a poll riding a held session (telnet, ssh, a websocket, a tls line, a login session) is
attached first and run now (`endpoint:run`), which lands its samples and is refused, with the reason,
while the endpoint is gated. A preview aimed at a device already attached takes its turn with that
endpoint's own work, never sending beside it, and is refused the same way (409, naming the reason)
while the endpoint is gated; an outpost too busy to queue another answers 429, so retry in a moment.
Both are operator grants, audited
([collection](/architecture/collection/#on-demand-validate-preview-and-run-a-poll-now)).
