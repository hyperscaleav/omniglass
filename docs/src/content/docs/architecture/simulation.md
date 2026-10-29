---
title: Simulation
description: How Omniglass simulates a fleet of devices on loopback addresses, so collection, alarms and the console can be exercised and taught without real gear.
sidebar:
  badge:
    text: Partial
    variant: note
---

Omniglass ships a device simulator in the same binary. It serves a whole fleet of fictional AV
and IT devices, each on its own loopback address, speaking the protocols real gear speaks (HTTP
and HTTPS, SNMP, SSH, Telnet, raw TCP line protocols and the same over TLS, UDP) from one live state per device. A
collector polls a simulated device as it would hardware, and the device answers from that state:
a write over one protocol shows in every protocol that reads what it wrote. That makes the simulator both a
test bed (the end-to-end and screenshot harnesses get a real fleet to collect from) and a
teaching surface ([learning tool](/contributing/learning-tool/)): an operator can watch the
monitoring loop work against a fleet they can break on purpose.

:::note[Partial]
Built: the engine in `internal/sim` (ported from the retired omniglass-lab project, #886): the
embedded catalog of device and system profiles, response templates and fleet manifests; the
fleet compiler, which expands a manifest into devices with stable hostnames, serials and
addresses; and the engine, which serves every protocol surface a device declares, with behaviors and
computed fields over shared state, follows a changing fleet in place, and reports every
connection and request it answers on one hit stream. Also built: the `sim run` mode beside a node
(#887), which pulls the devices placed on the node, serves them, and reports hits the server shows
at `GET /nodes/{name}/simulation`. Also built: the simulated dev fleet (#888), where every dev-seed
component a profile serves gets an address and endpoints on the dev node, so the dev node
collects for real. Also built: impairments (#889), an operator unplugging, slowing or degrading a
simulated device through the API and CLI, applied by a simulator run with `--impairments` and
`NET_ADMIN`, with ping made truthful. Also built: the console surfaces (#890): the fleet view, a
device's API surface and firing any operation at it, adding a device, and the Simulation page,
drawer and component section over them. Also built: the simulator in the node's own process,
`omniglass node run --simulate` (#1198), and the [lab](/guides/lab/) (#1194), which runs that node
in a container of its own with every privilege the simulator can use. Still `Design`: the Helm
sidecar (#892).
:::

## The catalog

Everything a simulated device is comes from the embedded catalog (`internal/sim/profiles`), so
the binary needs nothing on disk:

- a **device profile** declares a model's identity, its protocol surfaces (protocol, port, auth, the
  routes, commands, OIDs and subscription subjects each one answers), its behaviors (values that
  drift on an interval), its computed fields (values derived on read, such as uptime from a boot
  epoch) and its default state;
- a **system profile** declares a room archetype: which device profiles fill which roles;
- an **fleet manifest** declares sites, buildings and rooms and which system profile fills each,
  with an address block per site;
- **templates** render responses from live state, so a device's HTTP status route and its CLI
  `show channels` read the same values.

A device profile is named for the product it simulates, and may list other products that are the
same kind of device (the same component type), each with its own identity (a model name) over the
profile's defaults: the Boreal Edge displays in every size share one profile. A different kind of
device never stands in, even on the same driver: a touch panel's surface is a subset of a control
processor's, so each has its own. Every
seeded product on the network is served by a profile or listed, with its issue, in the catalog's
`coverage.yaml`, and a test holds the two catalogs to that. Products with no network interface (a
passive speaker, an analog microphone) have nothing to collect from and are not simulated:
`coverage.yaml` lists them as `passive`, each with how the fleet watches it instead (an amplifier
channel's load, a DSP input's level), and a test refuses a passive product that has a driver. A service
can be on the network with no driver, so the converse does not hold. A second test holds the lab's own
fleet to the same accounting: every component the dev seed places (its simulator-only ones included) is
simulated, listed with its issue, passive, or one of the fleet's few productless components (#1359).
The lab then reaches "every seeded component is collected" one profile at a time, and the list of what
is left is always current.

The catalog names only fictional vendors (Kestrel, Polaris, Newtron, Boreal, Tessellate, Shinra Cloud), the same
rule the boot seed follows, and a test holds it to that.

## Fleets

The compiler expands a manifest against the catalog into a flat device list. Every device gets a
location-based hostname, a stable slug, a serial and MAC derived from the slug, and the next
address in its site's block. A recompile of the same manifest yields the same fleet, so a
simulated device keeps its identity across restarts.

## One device, every protocol, one state

The engine gives each device a listener per declared protocol surface on the device's own address, and
every listener reads and writes the same state tree. The DSP's HTTP gain route writes
`status.channels.0.gain`; the SSH `show channels` renders it; a line-protocol session subscribed to
that path receives a push; SNMP answers its mapped OIDs from the same tree. Routes and commands
match exactly. SNMP is read-only (v1 and v2c GET, GETNEXT, GETBULK and walk; no SET, no traps),
and a UDP face is an echo unless it is persistent, when it speaks a line protocol as datagrams
([datagram faces](#datagram-faces)).

Every request a device answers (an HTTP route, an SNMP GET, a CLI command) arrives on one hit
stream with the device, protocol, operation, port and status, plus the peer address where the
protocol exposes it (SNMP does not). Session lifecycle (a line-protocol session opened, closed or
pushed to) is a separate stream. The engine does not know who consumes either; the run mode will.

## Addresses and reachability

A device's address is a loopback address (`127.x.y.z`), so the whole fleet shares one host's
network with the collector beside it, with no routing between them: on Linux, no containers are
required either (macOS needs an alias per address, and impairments need a namespace of their own,
which is why the [lab](/guides/lab/#why-docker-is-required) runs in Docker). On Linux every
`127.0.0.0/8` address is local without configuration, and the kernel routes each connection by its
destination, so a device's TCP and UDP listeners answer on its address alone: a TCP connect to an
address with no device is refused, and a UDP datagram draws a port-unreachable.

Two facts shape where the simulator runs:

- **Every loopback address answers ping**, device or not, because the kernel answers ICMP. For
  ping to tell the truth, addresses without a device must be dropped by a firewall rule, which
  needs `NET_ADMIN`.
- **Ports below 1024 need a privilege** (SNMP 161, SSH 22, HTTP 80 and 443). The engine can lift
  the ports below 1024 at listen time, which is how a simulator on a workstation serves unprivileged; the tests
  sidestep the question with ephemeral ports (below).

### Fixed ports and ephemeral ports

Each protocol surface a simulated component serves is a **face**: one listener, realizing one
surface its profile declares. The engine asks every face for its port through one port map,
which the caller picks; the face binds what it is asked for and records the address it actually
got (`Device.Faces`, each with the surface's name, protocol, declared port and bound address).
There is one bind path, so the two choices differ only in the port asked for:

- **Fixed** (no map, or `sim.PrivilegedPortShift(offset)`): every face binds the port its
  profile declares, ports below 1024 lifted by the offset. This is the operator-facing simulator:
  `omniglass sim run`, `omniglass node run --simulate` and the dev fleet, where an operator, a
  seeded endpoint and these docs all name the same port (a display's SNMP on 161, or 10161 when a
  simulator lifts it by 10000; a panel's line protocol on 51325, above the lift).
- **Ephemeral** (`sim.EphemeralPorts`): every face binds port 0, so the kernel hands it a free
  port, and the face reports the one it got. Tests serve this way and aim their endpoints and
  clients at the reported address, so two test runs on one host (two worktrees, or a `-count`
  rerun) never collide over a port, however many faces the protocol work adds.

Whichever it binds, a face reports the port it bound everywhere it reports one: its hits, its
session events, and the request detail SNMP publishes.

### Faces behind TLS

A raw TCP line face may declare `tls: true` (#1035): it serves the same line protocol behind a real
TLS handshake, and reports itself as a tcp face with `TLS` set. The simulated panel's `nvp-tls` face
on 51326 is the proving face for the `tls` transport. Its certificate is self-signed for the
simulated component's hostname and address, and its key is **derived from its id**, so it is the same
on every run: an endpoint that pinned it stays up across a simulator restart, where a key minted per run
would reject every pinned endpoint each time the simulator came back. (The simulator protects
nothing, and a key anyone can derive from a public id says so.) The certificate's validity window is
the profile's `tls_validity_days` (a year when unset), counted from the face's first start, so a
profile can serve a certificate ten days from expiry. A test replaces the key (`RotateTLSKey`, a
replaced component) or the whole certificate (`SetTLSCertificate`, one a test root vouches for),
restarts the face on its port (`StartTLSSession`), and counts the handshakes it was offered
(`TLSHandshakes`, so a bare TCP connect is told from a handshake); these are test-harness hooks, not
operator-facing simulator operations.

An http face declaring `tls: true` (the Kestrel bar's API on 443) serves the simulated component's own **https
identity** (#902): an ECDSA key derived from its id, the same on every run, so an `https` endpoint
that pinned it stays up across a simulator restart (an ECDSA key, not the line face's Ed25519 one,
because a browser refuses an Ed25519 certificate and an operator opens the bar's page in one). A test
replaces it with `RotateHTTPSKey` and reads it with `HTTPSCertificate`. An http face's `auth` may be
`basic`, `header` (an api key in a named header), `bearer` (an `Authorization: Bearer` token) or
`query` (an api key in the query parameter `query` names, `apikey` by default), each answering a
wrong or missing credential with 401; `HTTPRequests` and `HTTPRefusals` count what the faces were
sent and what the gate refused, so a test tells a gated collector from a retrying one. A face's
`auth` may also sign in (#1114): `form` (a sign-in page at `login_path`, `/login` by default, whose
post opens a session cookie; a request with no live session is redirected there, a wrong password
back to it with `?error=1`), `token` (a JSON login at `login_path`, `/api/login` by default,
answering a token and its `expires_in`, `session_ttl` seconds), or `digest` (RFC 7616, `algorithm`
MD5 or SHA-256, its nonce going stale on a replayed count or a rotation). The sessions live in the
component's memory; the console expires them all (and rotates the digest nonce) by firing `POST` at
the face's `expire_path`, as a reboot would, so a collector's re-login can be watched. The Newtron
panel's https API signs in with a form and the Boreal display's web admin answers digest;
`HTTPLogins` and `HTTPDigestAnswers` count the logins and the answered challenges a test reads. An
http face may also declare `events` (#1038), a server-sent events stream at its `path`, behind the
face's auth: every change to the state paths it names (each with everything beneath it) is one
numbered event (`event: change`, its data the path and the new value as JSON), the last 256 kept,
and a reconnect's `Last-Event-ID` replays from that event (the event itself included, as some
servers resend it, so a collector's dedup is exercised on every resume); `retry_ms` is the
reconnection time it asks for, and the console drops every stream by firing `POST` at its
`drop_path`, as a restart would. The Kestrel bar streams its call, volume, mute and power;
`EventStreams`, `EventStreamAttempts`, `DropEventStreams` and `HTTPCredentialRefusals` are what a
test reads and drives. A `websocket` face (#1034) is a control socket of its own, plain or (with
`tls`) under the device's https identity, its upgrade at the `websocket` block's `path` behind the
face's own auth (`basic`, `header`, `bearer` or `query`): a message per JSON text frame, where a
`subscribe` is acknowledged and then pushes every change to the paths it names (among the block's
`paths`), a `get` is answered by a `result` carrying the request's `id`, a `set` on one of the
block's `settable` paths pushes the change to every subscriber before its `result`, and anything
else is an `error` carrying the id. The console drops every socket with a going-away close by
firing `POST` at its `drop_path`. The Kestrel bar serves its control socket over wss on 9443;
`WebSocketAttempts`, `WebSocketUpgrades`, `WebSocketRefusals`, `WebSocketSubscribes` and
`DropWebSockets` are what a test reads and drives. The DSP serves
its status as namespaced XML (`GET /api/status.xml`) beside its JSON and its HTML dashboard, the
three formats an http driver reads. A test that drives the run mode out of
process (the end-to-end tests run `omniglass sim run` beside a node) cannot take an ephemeral
port: the run mode binds each face at its profile's port, lifted by `--port-offset`, on the
address the endpoint targets, and has no way to tell the endpoints a port it picked, so the
endpoints must target the profile's ports. Such a test keeps the fixed ports and claims a
loopback address no other test holds instead (`simtest.ClaimLoopback`).

### SNMP faces

A v2c face's community is strict (#821): it answers its own community alone and drops any other
silently, as a real v2c agent drops a wrong community (the library underneath, configured with no
security, would answer any community, so the face judges the community itself before the library
sees a request). A face may also declare a `refused_community` under its `auth`, a community it
knows and refuses, which it answers with an `authorizationError` response, so the fleet can show
the one auth refusal v2c makes detectable. The catalog's faces answer `public` and refuse
`revoked`. A test counts the requests a face was sent (`SNMPRequests`, a test-harness hook), to
prove a refused community is not sent again.

### Datagram faces

A udp interface declared `persistent: true` (#1037) serves the line protocol as datagrams, one line
each, where without it the face is a bare echo (the dsp's `udp-echo`). Each source address is its own
session, run by the same shell loop as the tcp face, so a source that subscribes is the destination
its pushes are sent to, and a session ends after five minutes with no datagram from its source. The
simulated panel's `nvp-udp` face on 51327 is the proving face for the `udp` transport. The face drops
a share of datagrams each way, the profile's `loss_pct` (a udp face only, 0 to 100), so a driver's
retries are exercised on a real wire with no `NET_ADMIN`. The loss is **seeded per datagram**: a
datagram's fate is fixed by the device, the face, its direction, its bytes and how many times those
bytes have crossed, so the third `SET INPUT hdmi2` in meets the same fate on every run however other
traffic interleaves with it, and a test on a lossy face repeats. The panel declares 5%, so the dev
fleet's udp endpoint always has retries at work; a test sets the share (`SetDatagramLoss`, which
restarts the counts), a test-harness hook like the TLS ones.

## Beside a node

The simulator is its own run mode, `omniglass sim run`, that runs beside a node and shares its
loopback, holding that node's credential: it reads the same `OMNIGLASS_SERVER`,
`OMNIGLASS_NODE_NAME` and `OMNIGLASS_NODE_TOKEN` as `node run`, so the node's subject grant is its
grant too. It can pull the devices behind its own node, report hits on them, and answer the
operations the server asks of its node's simulator, never another node's
([ADR-0142](/architecture/decisions/#adr-0142-the-simulator-is-a-run-mode-beside-its-node-holding-that-nodes-identity)).

On every pull (every ten seconds by default) it asks the server for each component with an
endpoint placed on the node, with the component's product and the host its endpoints target, and
serves the ones it can: a product a profile serves, at a loopback address.
The rest it logs and leaves alone. A device added on the server starts serving on the next pull
and a device removed goes dark, with no restart and no disturbance to the others. Each pull states
what the simulator can do where it runs (bind ports below 1024, hold `NET_ADMIN`, the lift it
applies to ports below 1024 with `--port-offset` when it cannot bind them), and about once a second it
publishes the hits it saw and the devices it is serving.

The server accepts hits only for components it handed that node, keeps them in memory (a live
signal, not stored history), and joins them to the node's devices at
`GET /nodes/{name}/simulation`, which the CLI reads as `omniglass node simulation <node>`. Each
component carries a `status`, what the simulator is doing with it: `running`; `starting` (added since
its last pull, or handed over in the last five seconds); `failed` (handed over and not started); or
`offline` (the simulator stopped reporting). The words are the simulator's own, so they never borrow
reachability's up and down or health's verdicts. A
connection a device accepts counts as a hit, so a collector's bare reachability probe shows there
as `tcp`, beside the requests it answers.

### In the node's own process

`omniglass node run --simulate` runs the same simulator inside the node's process: the node's run
loop and the simulator side by side, under one context (`simnode.RunWithNode`). Loopback addresses
belong to a network namespace, and the kernel never routes `127.0.0.0/8` off `lo`, so a separate
`sim run` must be arranged into the node's namespace; in one process the simulated components sit on
the loopback the node collects from by construction. Both halves already claim with the node's name
and enrollment token, so in process they share that identity: no second enrollment and no second
grant, and the simulator stays inside the node's subject grant, as
[ADR-0142](/architecture/decisions/#adr-0142-the-simulator-is-a-run-mode-beside-its-node-holding-that-nodes-identity)
requires. `--impairments`, `--port-offset` and `--poll` are the simulator's flags and the node refuses
them without `--simulate`. The two halves live and die together: when either stops, the other is
stopped, and the first real failure is the one reported.

With `--impairments`, root and `NET_ADMIN` sit on the node process itself (a distroless image grants
added capabilities to root only), so the mode belongs in a container of the node's own, where the
privilege reaches only that container's namespace. The impairments touch only the simulated
components' addresses on `lo`; the node's traffic to the server leaves through the container's own
interface and is never shaped or dropped. `omniglass sim run` stays, for a simulator as a process of
its own beside a node, and for the Helm sidecar (#892).

## The dev fleet

The dev seed simulates its own fleet. Every component whose product a profile serves gets an
address in `127.60.0.0/16` (a component keeps the address it was given, and a new one takes the
lowest address no endpoint already targets) and endpoints on the dev node, `edge-hq`. A product whose driver
has a spec gets that driver attached: SNMP products through `snmp-generic` (with a seeded platform
community secret), the Newtron panels through `newtron-nvp`'s line protocol, the Kestrel bars
through `kestrel-api` over https (a seeded basic-auth login, the bar's key pinned on first use), and
the Tessellate sensors through `http-json` (a seeded api key in its header). The huddle's panel
also carries its line protocol over telnet behind a login (`newtron-nvp-telnet`, the
rejected-session demonstration) and over TLS (`newtron-nvp-tls` on 51326, pinning the panel's key
on first use). A product whose driver
is still a stub gets a reachability probe on the port its profile serves. The endpoints target the
profiles' real ports, which a simulator in a container binds; for a simulator on a host that may not
bind below 1024, `omniglass seed-dev --sim-port-offset 10000` aims them at the lifted ports instead.

The [lab](/guides/lab/) runs the dev fleet with the node `edge-hq` serving it in process, so the
whole loop is one command: the node polls the simulated components, the samples land on the seeded
components (the worked reachability check's boardroom DSP among them, re-aimed at a simulated DSP so a
live node finds it up), and a command issued to a panel (`omniglass component command issue <panel>
--command-type set-input --value hdmi3`) changes the simulated component. The lab is where the
simulator gets every privilege it can use: the real ports, the ping sysctl, and `NET_ADMIN` for
impairments and truthful ping, all confined to the node's own container.

:::design[Target design: the simulator as a sidecar, tracked in #892]
In a deployment the simulator is a sidecar container in the node's pod, granted `NET_ADMIN` and the
unprivileged-port sysctl, so it binds the profiles' real ports and can make ping tell the truth.
:::

## Breaking a device on purpose

An **impairment** is what an operator has broken on a simulated device, recorded on the component and
applied by the simulator beside its node, so a collector sees exactly what a failing device would
show:

- **unplugged**: dark on every protocol. A connect times out (not refused), a datagram vanishes, a
  ping goes unanswered;
- **latency** and **jitter**: a delay, with variation, on traffic toward the device, which shows in
  the collector's connect times and round trips;
- **loss**: a share of packets toward the device dropped (to hundredths of a percent);
- **rate**: a bandwidth limit on the device's link, both ways, so it slows what a collector
  downloads as well as what it sends. The return path is limited but not delayed, so a latency
  still counts once per round trip.

`PUT /components/{name}/impairment` sets it (`omniglass component impairment set <component>
--unplugged`, `--latency-ms 300`, ...) and `GET` reads it back. A set replaces the whole
impairment: a field left out reads as zero, so setting a latency on an unplugged device plugs it
back in, and an empty set repairs it. Both
are gated (`simulation:update`, which operators hold, and `simulation:read`), resolve the component
in the caller's component scope, and the write is audited. The node's simulation view shows each
device's impairment and the simulator's account of applying it.

The simulator applies impairments only when started with `--impairments` and holding `NET_ADMIN`,
in a network namespace of its own (a pod or container): it rewrites that namespace's firewall and
traffic shaping, through netlink with no `nft` or `tc` binary. It owns one nftables table and one
root qdisc on loopback and touches nothing else, though while any device is degraded all loopback
traffic passes that root (unclassified traffic unshaped). It clears both when it stops, and sweeps
any a killed run left behind when it starts. Without the flag or the capability it applies
nothing and says why, in the simulation view. With them, **ping tells the truth**: an ICMP echo to
a loopback address that is no device is dropped, so only devices answer.

What the simulator cannot do yet is break one protocol while the others answer, or pin a value so a
collector reads plausible but wrong data (#909). An unplugged component's **system verdict** moves on its own: the official
`reachability-lost` [event rule](/architecture/alarms-actions/#the-event_rule) opens a `high` alarm on it
once its endpoint has read `down` for the sustain, and clears it when the endpoint reads `up` again.

## Exercising a simulated component

Every simulated component publishes its whole API surface, read from its profile:
`GET /components/{name}/simulation` lists each protocol surface (its protocol, the port the simulator binds,
whether it holds a session open) with its HTTP routes, its commands and its SNMP OIDs, the same
surface its listeners serve. An operator fires any of them with
`POST /components/{name}/simulation:fire` (or `omniglass component simulation fire <component>
--surface nvp --command "SET INPUT hdmi2"`): the server forwards the operation over the bus to
the simulator beside the component's node, on a subject of that node's own grant, and the
simulator answers it.

The simulator answers **in process**, from the component's live state, through the same routes,
commands and OID map its listeners use: a command's change sticks, so the collector reads it on its
next poll, and the operation counts as a hit, so its row lights up. Because it runs beside the
listeners rather than through the network, an impairment does not stop it: the console can still
inspect a component it unplugged. A profile that enforces HTTP authentication is called with its
own credentials, as an authorized client would; a face that signs in (a form, a token, digest)
trusts the console's own operations, as it would an operator at the component's screen.

A route may also report **named events** on the component's events face once its changes apply
(#1145): the Kestrel bar's dial route (its body names the call's `id` and its `participants`) and
its disconnect route report `call-started` and `call-ended` carrying the call's id, as a codec's
xAPI reports a call starting and ending, beside the state changes the feed streams. The Newtron
panel's NVP face pushes its diagnostics log (`SUBSCRIBE LOG`, a line its diagnostics route writes
arriving as `LOG <severity> <text>`) and its scene changes
(`SUBSCRIBE SCENE`, `EVT SCENE <scene>`) beside its input changes, the faces the shipped drivers'
event and log line emits are proven against ([ADR-1145](/architecture/decisions/#adr-1145-a-driver-emit-lands-on-any-of-the-four-lanes-and-a-log-lines-severity-is-rfc-5424s)).

A route, or a behavior, may also **post a webhook** once its changes apply (#1024), signed as a
real sender signs one: its `emit` names the URL, the body and headers, and a `sign` block (the
scheme, `hmac`, `token`, `basic` or `none`; the secret; the header the signature or token rides
and its prefix; the header carrying the signed unix timestamp; the header carrying each post's
delivery id), each value a template over the device's state. The UCC service is the face the
webhook transport is proven against: its admin API takes a **webhook subscription**
(its webhooks route takes the receiver's `url`, the `scheme`, the `secret` and an optional
`fault`), and its meeting routes (a start naming the meeting's `id` and `room`, and an end) post
`meeting.started` and `meeting.ended` to it, hmac sha256 over the
timestamp, a dot and the body. A fault posts as a sender gone wrong: `bad-signature` signs with the
wrong key, `stale` signs a timestamp ten minutes old, and `redeliver` sends one delivery twice. The
shipped `ucc-webhook` driver maps them; the dev fleet carries the service at headquarters with its
webhook endpoint, which a subscription to the endpoint's URL and the fleet's signing key
(`sim-ucc-signing`) brings to life ([ADR-1024](/architecture/decisions/#adr-1024-omniglass-receives-webhooks-on-the-server-and-a-webhook-token-is-a-sealed-secret)).

The **UCC platform** (`ucc-platform`) is the same vendor's account-wide plane, the face the
[fan-out](/architecture/collection/#shared-api-collection-one-component-many-owners) is proven
against (#1282): one service that speaks for every room on the account. Its room list answers
every room's meeting state and participant count in one read, its meeting routes name the room by
id (`room.meeting.started` and `room.meeting.ended`), and a report route posts one batched
`rooms.state` naming every room at once, each signed as the UCC service signs. A room id the
account has not seen joins it, the way a room added on the platform would, and a request's room id
reaches state only as one path key (a template's `segment` function refuses any other). The
shipped `ucc-rooms` driver polls the room list as records and the `ucc-webhook` driver's
room-naming listeners read the posts, each declaring the `ucc-room-id` identity. The dev fleet
carries the platform at headquarters with both endpoints and binds four of its rooms to their
systems; its fifth room, `zr-boardroom-b`, is bound to nothing, so its values land in the conduit's
unbound inbox until an operator binds it ([ADR-1282](/architecture/decisions/#adr-1282-a-conduit-fans-each-identified-value-out-to-the-owner-its-binding-names-and-a-binding-costs-a-write-on-both-of-its-ends)).
The lab's per-room UCC services stay beside it until #1390 retires them.

## Adding a simulated component

Adding a simulated component is adding a component the simulator can serve:
`POST /nodes/{name}/simulation/components` (`omniglass simulation add-component <node> --name panel-9
--product newtron-panel-7`) creates a component of a simulated product and the endpoint the
collector reaches it through, placed on the node. The plan is the dev seed's, made a primitive
(`internal/simfleet`): the lowest address in `127.60.0.0/16` no endpoint already targets, a driver
attach where the driver's inputs are known (an SNMP attach only when the `sim-community` secret
exists for it to reference), otherwise a reachability probe on a port the profile serves, lifted
as that node's simulator lifts its ports. The simulator serves it from its next pull, with no
restart. Changing its product among the products its profile simulates keeps its endpoints valid
(they share a driver), and deleting the component removes it from the simulation.

## Adding a simulated system

A simulated system is built to a **standard**, the platform's own archetype, not to a separate
simulator vocabulary. `GET /simulation/standards/{standard}:plan` (`omniglass simulation plan-system
<standard>`) staffs each of the standard's roles with the first simulated product that fits its
accepted types and pins, as many as its quorum and at least one, capped at its capacity, and takes one
alternate per choice (the first that can be fully staffed, else the one that staffs the most). A role no simulated product fits is left
short, with the reason: that list is what the simulator's catalog still lacks.
`POST /nodes/{name}/simulation/systems` (`omniglass simulation add-system <node> --standard <standard>
--system-type <type>`) then creates the system, a component per staffed slot (named as an unnamed
create names it), each at its own free address with its planned endpoint on the node, and each role
assignment, in one transaction under the address lock, so the system lands whole or not at all. Every
piece is the ordinary create with the caller's own scope and audit row. The simulator serves the
components from its next pull. The plan and the write are the **planned-system primitive** a
[system template](/architecture/templates/) also builds on: the pure planner
(`internal/systemplan`) takes each role's candidate products as an input (here the products the
simulator serves), and the planned create (`storage.CreatePlan`) lands places, systems, their
components, endpoints, declared values and role assignments in one transaction. The lab's room archetypes in the catalog (`profiles/systems`) remain
only as the input of the simulator's internal fleet compiler (#914); they are not how an operator
adds a system ([ADR-0143](/architecture/decisions/#adr-0143-a-simulated-system-is-built-to-a-standard-with-platform-minted-names)).

## In the console

Everything above has a console surface, shown only while a simulator is connected: the
**Simulation** page lists every simulated component in a table, a row lighting up when the
collector reaches it, and adds a simulated component or a whole simulated system (previewing the
standard's plan); the simulation drawer carries its status, its impairment controls and its
API surface, with a form to fire any operation; and a simulated component's own page carries a
Simulation section that opens the same drawer. The walk-through is the
[simulation guide](/guides/operator/simulation/).
