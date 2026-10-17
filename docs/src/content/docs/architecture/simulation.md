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
and HTTPS, SNMP v2c, SSH, Telnet, raw TCP line protocols, UDP) from one live state per device. A
collector pointed at a simulated device cannot tell it from hardware: it polls, the device
answers, and a write over one protocol shows in every other. That makes the simulator both a
test bed (the end-to-end and screenshot harnesses get a real fleet to collect from) and a
teaching surface ([learning tool](/contributing/learning-tool/)): an operator can watch the
monitoring loop work against a fleet they can break on purpose.

:::note[Partial]
Built: the engine in `internal/sim` (ported from the retired omniglass-lab project, #886). The
embedded catalog of device and system profiles, response templates and fleet manifests; the
fleet compiler, which expands a manifest into devices with stable hostnames, serials and
addresses; and the engine, which serves every device's declared interfaces with behaviors and
computed fields over shared state, reporting each hit through a function seam. It runs
in-process under test today. Still `Design`: the sim run mode and its pod and
`make dev` placement (#887), simulating the seeded inventory (#888), impairments (#889), and the
console surfaces (#890).
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

The catalog names only fictional vendors (Kestrel, Polaris, Newtron, Boreal, Tessellate), the same
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
match exactly, and SNMP is v2c GET, GETNEXT and walk.

Each hit (a request answered, a command run, a session opened or pushed to) is reported through a
function seam on the engine, with the device, protocol, operation and raw peer address. The
engine does not know who consumes hits; the run mode will.

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

:::design[Target design: the sim run mode, tracked in #887]
The simulator is its own run mode that runs beside a node and shares its
loopback: as a sidecar container in the node's pod (granted `NET_ADMIN` and the unprivileged-port
sysctl), or locally as one more host process next to `server` and `node`. It pulls its device list
from the server as a node-like principal, so the seeded inventory is the simulated fleet, and
reports hits back so the console can show the collector reaching each device.
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
