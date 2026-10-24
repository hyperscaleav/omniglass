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
component a profile serves gets an address and endpoints on the dev node, so `make dev-sim`
collects for real. Also built: impairments (#889), an operator unplugging, slowing or degrading a
simulated device through the API and CLI, applied by a simulator run with `--impairments` and
`NET_ADMIN`, with ping made truthful. Also built: the console surfaces (#890): the fleet view, a
device's API surface and firing any operation at it, adding a device, and the Simulation page,
drawer and component section over them. Still `Design`: the Helm sidecar (#892).
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
`coverage.yaml`, and a test holds the two catalogs to that. Products with no driver (a passive speaker,
an analog microphone) are not on the network and are not simulated.

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
network with the collector beside it, no containers or routing required. On Linux every
`127.0.0.0/8` address is local without configuration, and the kernel routes each connection by its
destination, so a device's TCP and UDP listeners answer on its address alone: a TCP connect to an
address with no device is refused, and a UDP datagram draws a port-unreachable.

Two facts shape where the simulator runs:

- **Every loopback address answers ping**, device or not, because the kernel answers ICMP. For
  ping to tell the truth, addresses without a device must be dropped by a firewall rule, which
  needs `NET_ADMIN`.
- **Ports below 1024 need a privilege** (SNMP 161, SSH 22, HTTP 80 and 443). The engine can lift
  the ports below 1024 at listen time, which is how the dev fleet serves unprivileged; the tests
  sidestep the question with ephemeral ports (below).

### Fixed ports and ephemeral ports

Each protocol surface a simulated component serves is a **face**: one listener, realizing one
surface its profile declares. The engine asks every face for its port through one port map,
which the caller picks; the face binds what it is asked for and records the address it actually
got (`Device.Faces`, each with the surface's name, protocol, declared port and bound address).
There is one bind path, so the two choices differ only in the port asked for:

- **Fixed** (no map, or `sim.PrivilegedPortShift(offset)`): every face binds the port its
  profile declares, ports below 1024 lifted by the offset. This is the operator-facing simulator:
  `omniglass sim run`, `make dev-sim` and the dev fleet, where an operator, a seeded endpoint and
  these docs all name the same port (a display's SNMP on 161, lifted to 10161 in the dev fleet;
  a panel's line protocol on 51325, above the lift).
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
operator-facing simulator operations. A test that drives the run mode out of
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

## The dev fleet

The dev seed simulates its own fleet. Every component whose product a profile serves gets an
address in `127.60.0.0/16` (a component keeps the address it was given, and a new one takes the
lowest address no endpoint already targets) and endpoints on the dev node, `edge-hq`. A product whose driver
has a spec gets that driver attached: SNMP products through `snmp-generic` (with a seeded platform
community secret), the Newtron panels through `newtron-nvp`'s line protocol. The huddle's panel
also carries its line protocol over telnet behind a login (`newtron-nvp-telnet`, the
rejected-session demonstration) and over TLS (`newtron-nvp-tls` on 51326, pinning the panel's key
on first use). A product whose driver
is still a stub gets a reachability probe on the port its profile serves. The endpoints target the
devices' ports below 1024 lifted by 10000 (`devseed.SimPortOffset`), so the simulator runs without
a privilege.

`make dev-sim`, run beside `make dev`, runs the simulator and the node side by side as `edge-hq`, so
the whole loop is local: the node polls the simulated devices, the samples land on the seeded
components (the worked reachability check's boardroom DSP among them, re-aimed at a simulated DSP so a
live node finds it up), and a command issued to a panel (`omniglass component command issue <panel>
--command-type set-input --value hdmi3`) changes the simulated device.

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
own credentials, as an authorized client would.

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
components from its next pull. The lab's room archetypes in the catalog (`profiles/systems`) remain
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
