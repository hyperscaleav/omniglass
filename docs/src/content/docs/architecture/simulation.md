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
and HTTPS, SNMP, SSH, Telnet, raw TCP line protocols, UDP) from one live state per device. A
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
and UDP is an echo.

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
  the ports below 1024 at listen time, which is how the tests and the dev fleet serve unprivileged.

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
`GET /nodes/{name}/simulation`, which the CLI reads as `omniglass node simulation <node>`. A
connection a device accepts counts as a hit, so a collector's bare reachability probe shows there
as `tcp`, beside the requests it answers.

## The dev fleet

The dev seed simulates its own fleet. Every component whose product a profile serves gets an
address in `127.60.0.0/16` (a component keeps the address it was given, and a new one takes the
lowest address no endpoint already targets) and endpoints on the dev node, `edge-hq`. A product whose driver
has a spec gets that driver attached: SNMP products through `snmp-generic` (with a seeded platform
community secret), the Newtron panels through `newtron-nvp`'s line protocol. A product whose driver
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
collector reads plausible but wrong data (#909). And an unplugged device's **system verdict** does not
move on its own: a component's condition is its active alarms, and raising one from reachability is
the event rule tier (#819).

## Exercising a device

Every simulated device publishes its whole API surface, read from its profile:
`GET /components/{name}/simulation` lists each protocol surface (its protocol, the port the simulator binds,
whether it holds a session open) with its HTTP routes, its commands and its SNMP OIDs, the same
surface the device's listeners serve. An operator fires any of them with
`POST /components/{name}/simulation:fire` (or `omniglass component simulation fire <component>
--surface nvp --command "SET INPUT hdmi2"`): the server forwards the operation over the bus to
the simulator beside the component's node, on a subject of that node's own grant, and the
simulator answers it from the device.

The device answers **in process**, from its live state, through the same routes, commands and OID
map its listeners use: a command's change sticks, so the collector reads it on its next poll, and
the operation counts as a hit, so the device blinks. Because it runs beside the listeners rather
than through the network, an impairment does not stop it: the console can still inspect a device
it unplugged. A device that enforces HTTP authentication is called with its own credentials, as an
authorized client would.

## Adding a device

A simulated device is a component, so adding one is adding a component the simulator can serve:
`POST /nodes/{name}/simulation/devices` (`omniglass simulation add-device <node> --name panel-9
--product newtron-panel-7`) creates a component of a simulated product and the endpoint the
collector reaches it through, placed on the node. The plan is the dev seed's, made a primitive
(`internal/simfleet`): the lowest address in `127.60.0.0/16` no endpoint already targets, a driver
attach where the driver's inputs are known (an SNMP attach only when the `sim-community` secret
exists for it to reference), otherwise a reachability probe on a port the profile serves, lifted
as that node's simulator lifts its ports. The simulator serves the device from its next pull, with
no restart. Changing a device's product among the products its profile simulates keeps its
endpoints valid (they share a driver), and deleting the component removes the device.

## In the console

Everything above has a console surface, shown only while a simulator is connected: the
**Simulation** page lists every simulated device by node and blinks a device when the collector
reaches it; a device's drawer carries its status, its impairment controls and its API surface,
with a form to fire any operation; and a simulated component's own page carries a Simulation
section that opens the same drawer. The walk-through is the
[simulation guide](/guides/operator/simulation/).
