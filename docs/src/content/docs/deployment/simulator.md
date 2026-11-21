---
title: Running a simulator
description: "Run the simulator beside an outpost: the flags, the privileges each one buys, and the seeded fleets to collect from."
sidebar:
  order: 5
  badge:
    text: Partial
    variant: note
---

The simulator is the same binary, so a deployment can serve simulated components to an outpost
without a device. This page is the how-to for standing one up and granting it what it needs; the
concept is [simulation](/architecture/simulation/) and the operator's walk-through is
[simulate a fleet](/operator/simulation/). To try it with nothing to set up but Docker, use
[the lab](/developer/lab/).

The same binary simulates components. The simplest way is to let the outpost run it:
`omniglass outpost run --simulate` runs the simulator in the outpost's own process, serving every component
placed on that outpost whose product it has a profile for, each on its own loopback address
([simulation](/architecture/simulation/), and the operator's walk-through in
[simulate a fleet](/operator/simulation/)). One process means one network namespace, so the
simulated components sit on the loopback the outpost collects from by construction, and one identity:
the simulator uses the outpost's own name and token, with no second enrollment.

```bash
omniglass outpost run --name edge-hq --token "$OUTPOST_TOKEN" --simulate
```

`--impairments`, `--port-offset` and `--poll` are the simulator's flags, and the outpost refuses them
without `--simulate`. A simulator crash stops the outpost with it, so run it under something that
restarts it (a container restart policy, as the [lab](/developer/lab/) does).

To keep the simulator a process of its own beside an outpost, `omniglass sim run` does the same with the
outpost's identity. It must share the outpost's loopback: the same host, or the same container or pod
network namespace. With Docker, run it in the outpost container's namespace:

```bash
docker run -d --name omniglass-sim --network container:omniglass-outpost \
  -e OMNIGLASS_SERVER -e OMNIGLASS_OUTPOST_NAME -e OMNIGLASS_OUTPOST_TOKEN \
  ghcr.io/hyperscaleav/omniglass sim run
```

Either way the simulator runs unprivileged, and each privilege it is given buys one thing:

- **Ports below 1024** (SNMP 161, HTTP 80, SSH 22). In a container, Docker already lets any user bind
  them; in Kubernetes, set the safe sysctl `net.ipv4.ip_unprivileged_port_start=0` on the pod. A bare
  binary needs `sudo setcap cap_net_bind_service=+ep ./omniglass`. Without any of these, lift the ports
  with `--port-offset 10000` and aim the endpoints at the lifted ports
  (`omniglass seed-dev --sim-port-offset 10000` seeds the dev fleet that way).
- **Ping.** The outpost's pinger is unprivileged: the kernel must let its group send ICMP echo through
  `net.ipv4.ping_group_range` (with Docker, `--sysctl net.ipv4.ping_group_range="0 65535"`). Keep the
  range inside the groups the container's user namespace maps: rootless Docker maps 65537 of them, and
  a wider range is refused, so the container never starts.
- **Impairments and truthful ping** need `--impairments` and `NET_ADMIN`
  (`docker run --cap-add NET_ADMIN`), because they rewrite the firewall and traffic shaping of the
  network namespace the simulator runs in. Grant it only in a namespace of the simulator's own (a
  container or pod), never on a workstation's host namespace: there the same rules would apply to the
  whole machine's loopback. Without it, every loopback address answers ping and impairments are
  reported unavailable; collection still works.

On **macOS**, loopback answers only `127.0.0.1` until an address is aliased, so a bare binary needs an
alias for each simulated address it serves (the dev fleet uses `127.60.0.0/16`), for example
`for i in $(seq 1 20); do sudo ifconfig lo0 alias 127.60.0.$i up; done`. Impairments are Linux only.

:::design[Target design, tracked in #892]
A sidecar in the outpost's pod, offered by the Helm chart.
:::

## Seed a different fleet

The lab seeds the example fleet, but `omniglass seed-dev --scenario
<name>` can stand up other fleets: a fresh install with nothing in it, the example fleet having a bad
day, or the example fleet at enterprise size. The list, with what each one holds, is the command's own help, rendered in the
[CLI reference](/reference/cli/#omniglass-seed-dev).
Every scenario except the fresh install is the example fleet plus a generated expansion of it, so a
scenario never drifts from the fleet it grows from, and like the example fleet every one is
idempotent. The degraded fleet's alarms fit the device class: each event rule names the component
types it applies to (the lamp-hours warning is for projectors and displays), so a microphone or a
scheduler never reads "Lamp hours approaching end of life". Pick one with `--scenario` for the lab
(on its first run, or with `--fresh`), or
`STACK_SCENARIO` for the probe's lab (on a fresh install either is the server and its owner alone,
and no outpost runs, since the outpost's simulated components come from the example fleet):

```bash
omniglass lab run --fresh --scenario degraded
STACK_SCENARIO=large make probe
```

The large fleet copies the example fleet's self-contained rooms (a room whose devices serve only the
systems in it) under replica sites, and simulates only the example fleet's own devices, so the
collector is not load-tested by accident (a device the example fleet does not declare is never
wired by the seed, whatever the scenario). It takes a couple of minutes to seed.

Whatever the scenario, the example fleet plants one secret cascade to read. The simulated panel login
(`sim-panel-login`) exists at the platform, on Boardroom A, and on the touch panel inside it, so the
panel's [effective secrets](/developer/cli/#secrets) (the secrets resolution on the component's page in the
console, or `omniglass component effective-secret list <panel id>`) show the panel's own copy as the winner with
the room's and the platform's shadowed beneath it. Another component in Boardroom A resolves the room's
copy, and every component elsewhere the platform's. Secrets carry no system band, so there are three
tiers, not four. Re-seeding creates none of them twice.
