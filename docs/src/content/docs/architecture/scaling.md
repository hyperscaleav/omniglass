---
title: Scaling and deployment
description: "One binary that runs a laptop demo or a Kubernetes fleet: the modular monolith, the run modes, horizontal scale, high availability, the distributed edge, and per-database multi-tenancy."
sidebar:
  badge:
    text: Design
    variant: caution
---

Omniglass is **one Go binary**, and that is a packaging decision, not a scale ceiling. The same artifact
runs an all-in-one container on a laptop and a horizontally-scaled fleet on Kubernetes; you scale by
**topology**, not by swapping products. This page is the deployment and scale model: the modular
monolith, the run modes, what replicates, high availability, the distributed edge, and multi-tenancy.

## A modular monolith, run by mode

The binary is a **modular monolith**: one codebase, one artifact, with **run modes** that each run a
focused job (`server`, `node`, `migrate`, and more as they land). In a small deployment one process runs
everything; at scale the same binary is launched **per mode**, each replicated and sized for its job.
The modules are already separated by clean seams, the Storage Gateway is the only path to the database,
the worker worklists are Postgres-backed, collection runs at the edge, so splitting a mode onto its own
pods is a **deployment choice, not a rewrite**.

## The deployment spectrum

- **Single container (the 80% case, a small estate).** One process runs the API / server, an in-process
  node for central collection, and applies migrations, against a BYO PostgreSQL. No external dependency
  beyond Postgres. This is the laptop demo and the small site.
- **Kubernetes at scale.** The same binary, spread out: the **server** (the API and the views read path)
  replicated behind a load balancer; the **workers** (rule engine, outbox, clock, reconcile) replicated;
  **edge nodes** distributed one per site for collection; Postgres as a managed or CNPG cluster. Each
  tier scales independently.

## Horizontal scale: what replicates

- **Server (API and reads) is stateless**: replicate it freely behind a load balancer. State lives in
  Postgres, not the process.
- **Workers scale by adding replicas.** Every worklist is drained `SELECT ... FOR UPDATE SKIP LOCKED`
  ([workers](/architecture/workers/)), so **Postgres is the coordinator**: more workers means more
  throughput with no leader and no cross-worker chatter. The same row lock makes a job **single-fire
  across replicas** ([time](/architecture/time/)), so duplicates are impossible as you scale out.
- **Edge nodes: distribution is the design.** Collection is already pushed to the edge, one node per site
  (or many), each shipping its firehose in over the [node-server protocol](/architecture/nodes/). Adding
  sites adds nodes, and the server fans them in. The edge is the natural horizontal axis for collection.
- **Singletons** (the clock that fires schedules) run **single-fire across replicas** via that same row
  lock, so there is no separate leader-election service to operate.

## Vertical scale and high availability

Replicas are also the **HA** story: the server and worker tiers have no single point of failure (any
replica can serve or drain), Postgres HA is the database's concern (CNPG, a managed cluster), and the
**edge survives a WAN outage on its own** (the bounded buffer and the durable server-side command queue,
[nodes](/architecture/nodes/)). Vertical scale is the simple first lever (a bigger Postgres, more worker
CPU); horizontal is what removes the ceiling.

## Coordination and messaging

In a single binary, modules talk over **in-process channels**, and the cross-process coordination that
exists rides **Postgres**: the SKIP-LOCKED worklists (durable work) and `LISTEN/NOTIFY` (cache
invalidation, [identity and access](/architecture/identity-access/)). That keeps the small deployment
dependency-free beyond Postgres.

At scale across many replicas, the low-volume **fan-out** signals (cache invalidation, cross-replica
notifications) outgrow `LISTEN/NOTIFY`, and a real bus earns its place. The durable **work** stays in
Postgres (the SKIP-LOCKED worklists are already distributed and exactly-once); only the **ephemeral
fan-out** would move to the bus.

:::caution[Open question]
The messaging substrate at scale: `LISTEN/NOTIFY` versus an event broker like **NATS**, and whether NATS
is **embedded in the binary** (so single-binary mode keeps zero external dependencies and one code path)
or whether single-binary mode uses **in-process channels** and a broker appears **only when distributed**
(two implementations behind one seam). Either way the dividing line is the same: ephemeral fan-out on the
bus, durable work in Postgres.
:::

## Multi-tenancy: per database, per deployment

Tenant isolation is **physical, not a row predicate**: there is no `tenant_id` column anywhere, and the
cross-tenant boundary is the **database boundary itself**
([identity and access](/architecture/identity-access/)). At scale this is a **separate Postgres and
Omniglass deployment per tenant** (CNPG-per-tenant): the data model stays single-tenant-shaped, and
multi-tenancy lives at the orchestration layer. One noisy or compromised tenant cannot reach another,
because there is no shared row store to reach across.

## The one-binary promise

The same binary and the same code paths run the demo and the fleet. You do not adopt a different product
to scale: you run more modes, on more pods, against a bigger database, with more edge nodes. Simplicity at
the small end, a real horizontal ceiling at the large end, one artifact across the whole range.
