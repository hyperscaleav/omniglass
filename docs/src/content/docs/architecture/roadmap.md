---
title: Roadmap
description: "The directional layer: the epics and the architectural arc ahead, and how forward-looking work converges into the spine as it ships."
---

The [architecture](/architecture/) is written as one timeless design and converges **in place** as the
code catches up: a page moves `Design` to `Partial` to `Built`, it is never forked into a separate
"future" copy that later gets deleted. So this page is not a shadow architecture. It is the **readable
index of direction**: the epics in flight, the big architectural arc still ahead, and pointers to where
each lands in the spine.

Three surfaces carry the time axis, and this page ties them together:

- **[Implementation status](/architecture/status/)** is the live map of what is built (the per-page badge
  and the per-slice log).
- **[Decision log](/architecture/decisions/)** is the dated history of calls, reversals, and divergences.
- **GitHub epics** are the **source of truth** for scope and sequencing; this page links them and is not a
  substitute for them. Nothing here is a commitment that a detail ships unchanged; an epic is where the
  scope is actually argued and approved ([no branch before the issue](/contributing/slice-workflow/)).

## What has landed

The foundation and the first vertical tiers are built: the single binary with `server` / `migrate` run
modes, the auth and Storage Gateway foundation (capability + per-action ABAC scope), the
[generation pipeline](/contributing/api-first/) (OpenAPI to typed CLI and SPA client), and the structural
[estate](/architecture/core-entities/) (`location`, `system`, `component` as scoped trees, their
classifier catalogs and property contracts, the roles a system needs filled, and the
[health](/architecture/health/) verdict rolled up over them) with the operator console's inventory
surfaces. The per-slice detail is on [implementation status](/architecture/status/).

## Near-term epics

The work currently scoped, each tracked as a GitHub epic. Outcomes are summarized; the epic is
authoritative.

| Epic | Outcome | Lands in |
|---|---|---|
| [Identity tier (#27)](https://github.com/hyperscaleav/omniglass/issues/27) | Password login over an httpOnly cookie session, self-service profile and password change, and admin user / grant management. Closes the bearer-only and bootstrap divergences ([ADR-0004](/architecture/decisions/#adr-0004-credentials-ship-bearer-only), [ADR-0005](/architecture/decisions/#adr-0005-the-first-owner-is-omniglass-bootstrap)). | [identity and access](/architecture/identity-access/) |
| [Estate model: groups + dynamic scope (#10)](https://github.com/hyperscaleav/omniglass/issues/10) | Slice 5: entity-groups as scope anchors and dynamic-membership scope, plus the cross-tier cascade (a location scope reaching its systems and components). | [identity and access](/architecture/identity-access/), [groups](/architecture/groups/), [cascade](/architecture/cascade/) |
| [Deploy spine: PR previews (#41)](https://github.com/hyperscaleav/omniglass/issues/41) | Every open PR gets an ephemeral, Access-gated preview of the console, provisioned by Argo CD from the Helm chart (the chart is also the production deploy artifact). | [scaling and deployment](/architecture/scaling/) |
| [Public releases (#57)](https://github.com/hyperscaleav/omniglass/issues/57) | Signed and notarized binaries for every major OS/arch and one-line installs (Homebrew / Scoop / winget), so a first-time user runs with no security warning. | [scaling and deployment](/architecture/scaling/) |
| [Embedded Postgres run mode (#19)](https://github.com/hyperscaleav/omniglass/issues/19) | An opt-in single-binary mode with a managed embedded Postgres, for edge, demo, and learning installs with zero external database. | [scaling and deployment](/architecture/scaling/) |

## The architectural arc ahead

Most of the spine is still `Design`: the platform's reason for being, the monitoring pipeline, has its
data model and edge runtime specified but not yet built. The broad order the slices will follow, each
pointing at the page that already describes the target:

1. **Collection at the edge.** The [node](/architecture/nodes/) runtime, [templates](/architecture/templates/)
   (the reusable device shape), and [interfaces](/architecture/collection/), so a reading can come off real
   gear and be parsed at the edge.
2. **The data model.** [Datapoints](/architecture/datapoints/) (the canonical-signal registry and the
   exclusive-arc owner columns), [config and variables](/architecture/variables/) resolved down the
   [cascade](/architecture/cascade/), and the [expression engine](/architecture/expressions/) the rules and
   filters share.
3. **Detection and verdict.** [Events](/architecture/events/), [alarms and actions](/architecture/alarms-actions/),
   [calculations](/architecture/calculations/), and the [health](/architecture/health/) rollup that answers
   "is this system working?". The **verdict half has landed**: an alarm degrades a capability, an impaired
   role sinks its system by its impact, and the transitions record when it changed. What remains here is
   what **produces** an alarm (the `event_rule` tier) and what **acts** on one.
4. **The machinery underneath.** [Messaging](/architecture/messaging/) (the NATS subject contract and the
   data lane), the [workers](/architecture/workers/) draining the worklists, [time](/architecture/time/) as
   a primitive, and the CDC bridge on [storage](/architecture/storage/).
5. **The read side, in full.** [Views](/architecture/views/) and the `ViewResult` renderer, composable
   dashboards, and the exploration surfaces on the [UI](/architecture/ui/), plus the
   [MCP](/architecture/api/#also-an-mcp-surface) and [AI](/architecture/ai/) seams over the same gateway.

This arc is a reading order, not a schedule. Each numbered band becomes one or more epics with their own
scope and approval before any branch; when a slice in it ships, the relevant spine page moves off
`Design` and, if the build differs from the prose, the difference is logged in
[decisions](/architecture/decisions/). That is how directional intent becomes built architecture without
ever maintaining two copies of it.
