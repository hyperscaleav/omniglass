---
title: Nodes and reachability
description: "Enrolling a collection node, adding a transport-named endpoint to a component, and reading the per-endpoint reachability and the log events a node reports."
walkthroughs:
  - id: set-up-monitoring
  - id: fix-an-endpoint-in-place
  - id: fix-a-rejected-session
  - id: trust-a-changed-host-key
---

Collection is how the fleet learns whether a device is reachable and what it reports. An **edge
node** runs the probes, a component's **endpoint** is the API the node reaches for, the component's
**Reachability** panel shows the verdict, and its **Events** and **Logs** panels show recent occurrences
the node ships back. This page walks the console surfaces; the model behind them is
[data collection](/architecture/collection/), and every action here has the same
[scope](/guides/operator/#what-you-see-is-your-scope) and permission checks as the rest of the console.

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
  [tags](/guides/admin/tags/) (keys whose vocabulary allows nodes), the same tag editor the
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
    responded-but-not-authenticated are visible states), `udp` proves its rung with its
    driver's request, so attach a driver rather than probe it bare, and `snmp` has no standalone
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
- With `endpoint:update`, editing an endpoint changes its **node placement** and where it
  connects: a bare probe's **target**, or an attached driver's **inputs** (its host, its port, the
  name of the secret it authenticates with), edited in place and checked against the driver, with
  no re-attach. The transport (and so the name) is fixed at creation.
- With `endpoint:delete`, deleting an endpoint removes it and **cascades its derived task**.

Setting up monitoring on a component nothing collects from yet is both faces in turn: the SNMP
driver for what the component reports, then a probe for reachability, each placed on a node, with
reachability and samples arriving on the component's view seconds later. The view refreshes on its
own as they land, so there is nothing to reload. The walkthrough runs against a simulated
component, so the node is really collecting.

::walkthrough{#set-up-monitoring}

A driver attached with a mistake (an address typed one octet short) is fixed on the endpoint itself:
correct the input, save, and the node reconnects at its next check-in, so samples arrive seconds
later.

::walkthrough{#fix-an-endpoint-in-place}

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
pill** (responding, down, stale, or unknown; a verdict is stored only when it changes, so the
pill ages an endpoint by when the server last received anything its task delivered, its **last
seen**, and an endpoint answering every probe stays responding however long ago it last changed
state), an **availability strip** drawn from the
verdict's up/down transitions over time, and an expandable **gate breakdown** with each probe's signal and timing, then the composed
verdict (the endpoint is up only when every applicable probe passed). Every row also wears its
**reachability ladder**, the rungs a connection is checked on bottom up: **reachable** (a ping,
informational only, so its chip is dashed and a component that blocks ping never reads down for
it), **responsive** (the transport answering: a connect, the protocol's handshake, or, where an open
port cannot be detected, a named request answered; this is the gate, and nothing above it is tried
until it passes) and **authenticated** (the login, shown once a
credential was actually tried). Hover or focus a rung for what it checks and what it says now. A
rung a refusal holds wears the refusal glyph and the rungs above it read untried, while the rungs
below keep their readings: an endpoint whose login was refused still shows its ping and its
transport answering. A down endpoint also shows a plain-language **why** line. Every value is a
real reading from the node, and the panel is also the authoring surface: its header carries **Add
endpoint** (with `endpoint:create`) and each row that maps to an endpoint a **Manage** affordance
opening that endpoint's detail.

A **tls** endpoint's breakdown adds three rows, each teaching itself on hover: the **handshake** (the
transport rung, the TLS handshake answered, and how long it took), the **certificate** (its days
left, amber inside a month and red once expired, and its expiry date; while a certificate is
refused it says so rather than show the last accepted one's) and **trust**, how the endpoint
judges the certificate: verified against the node's roots, pinned on first use (with the key it
pins), or **not verified**. Skipping verification is an operator's choice, never a driver's default,
so its row reads it in amber and the endpoint's row wears **unverified** without being expanded. The
mode is one of the endpoint's driver inputs (`tls-mode`), edited on the endpoint's detail like its
host or port. A pinned key that changes reads **rejected** with class `certificate`, naming both
keys; once you know the component was replaced, `omniglass endpoint retry <id> --certificate <the
offered key>` trusts the new one ([tls](/architecture/collection/#tls)).

A **udp** endpoint has no port to open, so its breakdown lists its **request** rung beside the ping:
the driver's named request, answered or not, which is the layer its verdict follows. A component
that answers ping but not the request reads down, and its why line says the request goes
unanswered (the service is down, the port is closed, or the datagrams are lost); one that blocks
ping but answers reads up. A lost datagram is re-sent within the endpoint's timeout, so a little
loss never reads down. The endpoint binds its own port on the node (its driver's `listen-port`
input, else one the node picks), which the component's pushes arrive on; edit it on the endpoint's
detail like its host or port ([udp](/architecture/collection/#udp)).

To author a reachability check, add an **endpoint** to the component (above), from this
panel's own header: a bare probe endpoint is the reachability check, and **attaching a
driver** is the authoring flow for real collection (the spec's functions become the
endpoint's tasks, and their samples land on the component). There is no standalone
Endpoints page.

### Held sessions

An endpoint whose driver holds its connection open (a line protocol over `tcp`, `tls`, `telnet` or
`ssh`) is a **session**, and its state wears a pill beside the ladder on the component's
**Collection** card, on the **Endpoints** panel, and on the endpoint's own detail: **establishing**
(the first connection, dialing then logging in), **up** (held and logged in; polls and commands
borrow it), **recovering** (it dropped and the node is redialing on a backoff), **down** (five
attempts in a row failed; the node keeps redialing slowly, so a component that returns comes back
on its own) or **rejected** (the component refused in a way retrying cannot fix, such as a wrong
password, a changed host key or a certificate; the node dials no more). Rejected is amber with a refusal glyph,
apart from down's red, because it is the one state the node will not leave without someone acting.
Beside the pill is how long ago the state changed. A state is stored only when it changes, so the
pill is judged by the endpoint's last seen: an endpoint that has delivered nothing for a while
turns the pill grey, and its tooltip says what it last read.

A **down** or **rejected** session also says **why**, in the node's words for down and, for
rejected, the refusal's class and the component's own text (`auth: auth rejected`), scrubbed by the
node of anything it sent, so a credential never reaches the console. A rejected session offers two
ways out:

- **Retry**, with `endpoint:retry` (operators hold it), redials once with the endpoint as it is: the
  right move when the component itself was fixed (its account unlocked, its password reset to what
  the secret holds). It is audited, and the button holds its spinner until the session moves.
  Without the permission the button is greyed and its tooltip says so, and the server refuses the
  call anyway.
- **Fix the endpoint**: on the Collection card and the Endpoints panel the notice opens the
  endpoint, whose own notice offers **Edit credential** (with `endpoint:update`), its edit face with
  the secret input focused. Pointing the login at the secret holding the right credential, rotating
  that secret's value, or correcting the address or port changes what the endpoint connects with,
  so the refusal no longer holds and the node tries again at its next check-in, with no retry.

A component whose console password was rotated while its endpoint kept the old secret reads
rejected; pointing the login at the rotated secret brings the session up seconds later. The
walkthrough runs against a simulated touch panel, so the node really logs in.

::walkthrough{#fix-a-rejected-session}

#### A changed key

An `ssh` endpoint pins the host key its component presents: the node trusts the first key it sees,
and every later connection must present the same one. A component that presents another (it was
reinstalled, its hardware was swapped, or something sits in the middle answering in its place)
reads **rejected** before anything logs in, and the notice says **The host key changed** and names
both keys: the **pinned** one and the one the component **offered** instead. Both come from the
node's refusal as data, so what the console shows is exactly what the node refused.

**Trust this key**, with `endpoint:retry`, makes the offered key the new pin. It confirms first:
the confirm shows both keys again, and **Why a key changes** names the three causes, each
explained on hover, since only the first two are safe to trust. Compare the offered key with the
one the component's own console shows before trusting a change nobody can account for. On
confirming, the node redials onto the new pin once and the session comes up; the audit log records
the key replaced, the key trusted, and who trusted it. The confirm sends exactly the key it shows:
if the offer on record moves while it is open, the confirm says so and will not send, and the server
refuses any key other than the one the refusal on record names. That record is the node's last word,
and a refused node dials no more, so a component that changed again since (its own console shows a
third key) needs a **Retry** first: the node reads the key it offers now, and that is the one to
trust. Without the permission the
button is greyed and its tooltip says so, while the two keys still read, so a viewer can tell an
operator what changed. **Retry** stays beside it for the rarer case where the original component
came back.

In the simulated fleet, the touch panel in the place named `briefing` pins the key it had before it
was reimaged, so it reads rejected for a changed host key; trusting the offered key brings it up.

::walkthrough{#trust-a-changed-host-key}

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
"what did it *say*, and when". Both land under the same [scope](/guides/operator/#what-you-see-is-your-scope) and owner
checks as every other reading, so an out-of-scope component's events and logs are a non-disclosing 404,
exactly like its reachability. Every value is a real occurrence from the node.
