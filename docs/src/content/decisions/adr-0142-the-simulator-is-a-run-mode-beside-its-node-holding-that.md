---
title: "The simulator is a run mode beside its node, holding that node's identity"
date: 2026-10-18
status: "Accepted"
summary: "The simulator is `omniglass sim run`, a run mode beside its node that holds the node's identity and simulates the components placed on it; impairments are opt-in (`--impairments` and `NET_ADMIN`), the console's operations run in process, a rate limit holds both ways, hits are live, not stored, and `simulation:read` (with component scope) is the whole gate on the simulated fleet"
---

- **Date:** 2026-10-18 | **Status:** Accepted | **Pages:** [simulation](/architecture/simulation/), [API](/architecture/api/)
- **Decision:** Omniglass simulates its own fleet with `omniglass sim run`, a run mode of the one
  binary that runs beside a collection node and shares its loopback (the same host, or the same
  container or pod network namespace). It holds the node's identity and rides the node's subject
  grant, so it can only pull the devices placed on its node, report hits on them, and answer the
  operations the server asks of that node's simulator. Its fleet is the inventory: it pulls each
  component placed on the node whose product a profile simulates and follows changes without a
  restart, so adding a simulated device is adding a component (through a planner shared with the
  dev seed). Four rulings bound it:
  1. **Impairments are opt-in.** A simulator rewrites its namespace's firewall and traffic shaping
     only when started with `--impairments` and holding `NET_ADMIN`; holding the capability alone is
     not consent, because a simulator run as root on a workstation would otherwise rewrite the host's
     loopback rules.
  2. **The console's operations run in process.** An operation fired from the console is forwarded
     over the bus and answered by the device from its live state through the same routes, commands
     and OID map its listeners use, not by a client dialing the device, so an unplugged device can
     still be inspected and every protocol (SSH and Telnet included) is reachable without a client
     per protocol.
  3. **A rate limit holds both ways; latency and loss toward the device only.** The return path is
     limited but not delayed, so a latency still counts once per round trip.
  4. **Hits are live, not stored.** The server keeps them in memory per process, as a signal that
     the collector is reaching the device, not as telemetry.
  5. **`simulation:read` is the whole gate on the simulated fleet.** A caller sees a node's
     simulator when its component scope holds at least one component that node simulates, and
     only those components; no node read is required, so a location-scoped technician sees the
     simulation of their own places, and learns the simulator's node name and capabilities.
- **Context:** omniglass-lab simulated devices as a second platform (its own director, Postgres,
  gRPC edge plane and console). Epic #885 brought the simulation core home, and the architect took
  the recommended option on each open decision: the simulator pulls its device list from the server
  as a node-like principal, impairments live on the simulated device, a pod's simulator container
  gets `NET_ADMIN` and the unprivileged-port sysctl, and the Simulation page is a sidebar entry shown
  only while a simulator is connected. The opt-in flag and the in-process operations were settled
  while building it (#889, #890): the first after seeing what `NET_ADMIN` on a host namespace would
  do, the second because a per-protocol client would have bypassed nothing an operator needs and
  could not reach a device that is broken on purpose.
