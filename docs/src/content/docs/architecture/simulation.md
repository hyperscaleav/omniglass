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
addresses; and the engine, which serves every device's declared interfaces with behaviors and
computed fields over shared state, follows a changing fleet in place, and reports every
connection and request it answers on one hit stream. Also built: the `sim run` mode beside a node
(#887), which pulls the devices placed on the node, serves them, and reports hits the server shows
at `GET /nodes/{name}/simulation`, locally as `make dev-sim`. Still `Design`: the Helm sidecar
(#892), simulating the seeded inventory (#888), impairments (#889), and the console surfaces (#890).
:::

## The catalog

Everything a simulated device is comes from the embedded catalog (`internal/sim/profiles`), so
the binary needs nothing on disk:

- a **device profile** declares a model's identity, its interfaces (protocol, port, auth, the
  routes, commands, OIDs and subscription subjects each one answers), its behaviors (values that
  drift on an interval), its computed fields (values derived on read, such as uptime from a boot
  epoch) and its default state;
- a **system profile** declares a room archetype: which device profiles fill which roles;
- an **fleet manifest** declares sites, buildings and rooms and which system profile fills each,
  with an address block per site;
- **templates** render responses from live state, so a device's HTTP status route and its CLI
  `show channels` read the same values.

A device profile is named for the product it simulates, and may list other products of its family
that speak the same driver, each with its own identity (a model name) over the profile's defaults:
the Boreal Edge line shares one display profile, the Newtron panels and processor share one. Every
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

The engine gives each device a listener per declared interface on the device's own address, and
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
- **Ports below 1024 need a privilege** (SNMP 161, SSH 22, HTTP 80 and 443). The engine can remap
  every profile port at listen time, which is how the tests serve a fleet unprivileged.

## Beside a node

The simulator is its own run mode, `omniglass sim run`, that runs beside a node and shares its
loopback, holding that node's credential: it reads the same `OMNIGLASS_SERVER`,
`OMNIGLASS_NODE_NAME` and `OMNIGLASS_NODE_TOKEN` as `node run`, so the node's subject grant is its
grant too. It can pull the devices behind its own node and report hits on them, never another
node's.

On every pull (every ten seconds by default) it asks the server for each component with an
interface placed on the node, with the component's product and the host its interfaces target, and
serves the ones it can: a product a profile serves, at a loopback address.
The rest it logs and leaves alone. A device added on the server starts serving on the next pull
and a device removed goes dark, with no restart and no disturbance to the others. Each pull states
what the simulator can do where it runs (bind ports below 1024, hold `NET_ADMIN`, the shift it
applies to every port with `--port-offset` when it can do neither), and about once a second it
publishes the hits it saw and the devices it is serving.

The server accepts hits only for components it handed that node, keeps them in memory (a live
signal, not stored history), and joins them to the node's devices at
`GET /nodes/{name}/simulation`, which the CLI reads as `omniglass node simulation <node>`. A
connection a device accepts counts as a hit, so a collector's bare reachability probe shows there
as `tcp`, beside the requests it answers. Locally, `make dev-sim` runs the simulator beside
`make dev`'s fleet.

:::design[Target design: the simulator as a sidecar, tracked in #892]
In a deployment the simulator is a sidecar container in the node's pod, granted `NET_ADMIN` and the
unprivileged-port sysctl, so it binds the profiles' real ports and can make ping tell the truth.
:::

:::design[Target design: impairments, tracked in #889]
An impairment is a fault set on a simulated device: dark (no answer on any protocol, ping
included), latency, loss, a protocol refusing, a value pinned. Impairments live on the simulated
device and are applied through the firewall and traffic-control rules the run mode holds
`NET_ADMIN` for, so the collector sees exactly what a failing device would show.
:::

:::design[Target design: the console, tracked in #890]
A Simulation page lists every simulated device and blinks a device when the collector reaches it.
A simulated device's drawer shows its API surface and lets the operator fire any operation, and
the fleet can be edited from the console.
:::
