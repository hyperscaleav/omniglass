---
title: Implementation status
description: "What is designed versus built: the living map from the timeless architecture to shipped, tested code."
---

The architecture pages describe the **target model** in present tense: the design, not a snapshot of
the code. This page is the other axis, **what is actually built**, tracked per capability and updated
as each vertical slice lands. The split is deliberate: the architecture stays a stable reference, and
build progress lives here instead of rotting the design prose with "shipped in vX" notes.

## Two axes, kept separate

- **Design certainty** ("is this decided?") lives **inline** on each page as `Open question` asides.
  A page can be almost fully settled yet still carry a few open points.
- **Implementation state** ("is this built?") lives **here**, and as each page's status badge. A page
  can be fully designed and entirely unbuilt, or anywhere in between.

Neither axis is page-binary, and they do not move together.

## Badge legend

Each architecture page carries one status badge. The badge is the page's **floor**: a page is only
`Built` once *every* capability it describes is shipped, so a page with one unbuilt corner stays
`Partial`, and the matrix below is the per-capability truth.

| Badge | Meaning |
|---|---|
| **Design** (amber) | The model is specified; little or none of it is coded yet. |
| **Partial** (blue) | Some capabilities on the page are built and tested; others are still Design. |
| **Built** (green) | Every capability the page describes is implemented and tested. |

Today the repository is **public and published ahead of the code**, so the whole architecture is
`Design`. As slices land, rows below flip to `Partial` / `Built` with the version or PR that shipped
them, and the page badges follow. This matrix is hand-maintained now and becomes generated from issue
and PR labels once the toolchain lands.

## The matrix

| Capability | Status | Since | Notes |
|---|---|---|---|
| **Core entities** (component / system / location / node / global) | Design | - | |
| **Templates** (component / system, versioning, signing, role tiers) | Design | - | |
| **Datapoints** (kinds, provenance, registry, scope, fusion) | Design | - | |
| **Events** (`event_type`, origins) | Design | - | |
| **Calculations** (`calc_rule` and families) | Design | - | |
| **Config, credentials, variables** | Design | - | |
| **Tags** (governed K:V vocabulary) | Design | - | |
| **Cascade** | Design | - | |
| **Groups** | Design | - | |
| **Health & KPIs** | Design | - | |
| **Alarms & actions** (alarm, `action_rule`, flows, suppression) | Design | - | |
| **Collection** (functions, edge parse, shared-API, `discovery_rule`, `raw_sample`) | Design | - | |
| **Nodes** (edge process, placement, node mode) | Design | - | node-server wire protocol + edge buffering still to pin |
| **Time** (schedule + timer) | Design | - | |
| **Workers** (SKIP-LOCKED worklists) | Design | - | |
| **Storage Gateway** (modes, views, partitioning, tiering) | Design | - | |
| **Identity & access** (principals incl. `agent`, RBAC + ABAC) | Design | - | |
| **Audit** | Design | - | |
| **Files & blobs** | Design | - | |
| **Expressions** (Omniglass expressions) | Design | - | |
| **AI** (`agent` principal, propose -> approve) | Design | - | |
| **UI / console** (`ViewResult` renderer) | Design | - | |
| **API contract** (verbs, lists, errors, idempotency, long-running ops) | Design | - | **not yet pinned (priority)** |
| **Views / read BFF contract** | Design | - | **thin, to be specified (priority)** |

## Priority spec gaps

Two foundational contracts the first slices stand on are not yet pinned, and lead the build order:

1. **The API contract.** The AIP-style verb catalog, list (filter / sort / pagination), the error
   envelope, idempotency keys, long-running operations (a `command` is one), and the
   OpenAPI -> typed-client -> CLI generation guarantees. API-first is the first doctrine; the slices
   need its conventions before they reinvent them.
2. **The views / read BFF contract.** How a view is declared, parameterized, scoped (gateway scope
   injection on a read), paginated, and versioned, plus the `ViewResult` guarantees. The read half of
   every entity.

Node-server protocol with edge buffering, and entity lifecycle (delete cascade, maintenance mode)
follow as the collection and CRUD slices approach.
