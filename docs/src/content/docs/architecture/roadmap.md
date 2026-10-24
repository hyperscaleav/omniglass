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

The platform half is well past its midpoint. The single binary runs in `server` / `node` / `migrate`
modes over a BYO Postgres, and the tiers below are shipped, tested, and live in the console; the
per-slice detail is on [implementation status](/architecture/status/).

- **Identity and access, end to end.** Password, session-cookie, and API-token auth with expiry,
  lockout, and password policy; the full principal lifecycle (disable, archive, purge); impersonation
  with dual-actor audit; principal groups with grant-by-group; grants with scope operators; and the
  append-only [audit](/architecture/audit/) trail with its admin read surface.
- **The fleet and its catalogs.** The `location` / `system` / `component` trees on the shared
  scoped-CRUD primitive, `system_member` multi-membership, system roles with the typed-slot assignment
  guard, the standard / product / vendor / driver / component_type registries, and location types with
  `allowed_parent_types` placement rules.
- **The values cascade.** [Tags](/architecture/tags/), [variables](/architecture/variables/),
  envelope-encrypted secrets, [files](/architecture/files/) over the content-addressed blob store,
  the [settings](/architecture/settings/) engine (platform rung), and the unified resolution panel
  that explains which value won and why.
- **The collection vertical.** The [node](/architecture/nodes/) runtime with enrollment and claim,
  embedded NATS/JetStream with per-node subject isolation, the transition-only reachability verdict
  and its panel, endpoint and task authoring, and the raw-log lane with node self-logs
  ([ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events)). The
  stateless arc of [#603](https://github.com/hyperscaleav/omniglass/issues/603) (#824) made transports
  code and completed the layer-7 ladder: an `http` probe draws a real response and an `ssh` probe runs a
  real key exchange and tries the endpoint credential, so an endpoint reports reached, responsive, and
  authenticated, not just an open port. The same arc shipped the declarative driver spec, poll
  interpretation, and the command wire that actuates a device and settles against what it reports. The
  stateful arc followed: the line protocol holds a session whose listeners land the device's pushes
  (#816, #817), polls run on their declared schedule (#822), and an attached endpoint's tasks
  re-derive when its driver's spec changes (#823).
- **The telemetry ontology.** The `metric_type` / `property_type` / `event_type` / `command_type`
  registries, the sample lanes (`metric`, `property`, `event`, `log_line`), current values derived
  from the series, and the
  [command](/architecture/commands/) pillar with computed settlement
  ([ADR-0063](/architecture/decisions/#adr-0063-the-telemetry-model-is-typed-registries-over-bare-noun-data-tables)).
- **Expressions and rules.** The [expression engine](/architecture/expressions/) (#975,
  [ADR-0146](/architecture/decisions/#adr-0146-expressions-run-on-pinned-expr-with-an-allowlist-and-per-site-budgets-and-rule-criteria-are-expressions))
  is the one language the rules share. [Event rules](/architecture/alarms-actions/#the-event_rule) judge
  it on the ingest path and raise alarms at a severity level (#936,
  [ADR-0144](/architecture/decisions/#adr-0144-alarms-are-raised-by-event-rules-at-a-severity-level-from-a-ranked-registry)),
  recording each edge as a derived event, with or without an alarm (#955). [Calc rules](/architecture/calculations/)
  derive samples from samples (#1017,
  [ADR-0148](/architecture/decisions/#adr-0148-calc-rules-run-on-a-durable-pending-set-and-a-server-tick-not-a-jetstream-consumer)).
- **Delivery and the pipeline.** `make gen` emits the OpenAPI document, the cobra CLI, the typed SPA
  client, the CLI reference, and the generated ERD; goreleaser builds the release matrix and the
  multi-arch image; the Helm chart deploys production and powers per-PR preview environments.

## Near-term epics

The work currently scoped, each tracked as a GitHub epic. Outcomes are summarized; the epic is
authoritative.

| Epic | Outcome | Lands in |
|---|---|---|
| [Release 0.1.0 (#825)](https://github.com/hyperscaleav/omniglass/issues/825) | Ahead: the first public tag. The expression engine (#524) and the event rule tier (#819) are done, as is the stateless arc of [drivers, listeners, and the command wire (#603)](https://github.com/hyperscaleav/omniglass/issues/603). **#603's stateful arc closes the epic**: [sessions (#816)](https://github.com/hyperscaleav/omniglass/issues/816) that hold a connection and recover it, [listeners (#817)](https://github.com/hyperscaleav/omniglass/issues/817) that take device-pushed payloads over a held session, scheduled polls (#822), re-derived driver tasks (#823), and [the collection decision set (#818)](https://github.com/hyperscaleav/omniglass/issues/818). Beside it ride the delivery half of [telemetry integrity (#430)](https://github.com/hyperscaleav/omniglass/issues/430) and the [role staffing bugs (#504)](https://github.com/hyperscaleav/omniglass/issues/504); the epic lists the pre-tag hygiene. | [collection](/architecture/collection/), [nodes](/architecture/nodes/) |
| [Groups as scope and the cross-tier cascade (#10)](https://github.com/hyperscaleav/omniglass/issues/10) | Ahead: entity-groups as scope anchors and dynamic-membership scope, plus the cross-tier cascade (a location scope reaching its systems and components). Principal groups as grant subjects shipped; group-as-scope has not. | [identity and access](/architecture/identity-access/), [groups](/architecture/groups/), [cascade](/architecture/cascade/) |
| [Public releases (#57)](https://github.com/hyperscaleav/omniglass/issues/57) | Ahead: signed and notarized binaries for every major OS/arch and one-line installs (Homebrew / Scoop / winget), so a first-time user runs with no security warning. | [scaling and deployment](/architecture/scaling/) |
| [Embedded Postgres run mode (#19)](https://github.com/hyperscaleav/omniglass/issues/19) | Ahead: an opt-in single-binary mode with a managed embedded Postgres, for edge, demo, and learning installs with zero external database. | [scaling and deployment](/architecture/scaling/) |

The 2026-07-30 drift-correction program ([#428](https://github.com/hyperscaleav/omniglass/issues/428) through
[#434](https://github.com/hyperscaleav/omniglass/issues/434)) has closed except for the telemetry-integrity half of
[#430](https://github.com/hyperscaleav/omniglass/issues/430), which rides the release. The current workstream is
[release 0.1.0 (#825)](https://github.com/hyperscaleav/omniglass/issues/825). The template layer that follows the
collection epic is tracked on [#827](https://github.com/hyperscaleav/omniglass/issues/827).

## The architectural arc ahead

The spine has converged over most of its lower half; what remains `Design` is concentrated in the
automation and read tier. The broad order the remaining work follows, each band pointing at the page
that describes the target:

1. **Collection at the edge.** Substantially landed: the [node](/architecture/nodes/) runtime, the
   embedded bus, the probes up the layer-7 ladder (`http` and `ssh` prove a real answer, not only an
   open port), the declarative driver spec with poll interpretation, the command wire, and
   [endpoint](/architecture/collection/) and task authoring all shipped, and so did the stateful half:
   held line sessions and the listeners that ride them (#603), now over `tcp`, `tls`, `telnet` and
   `ssh`, each authenticating before it acts and stopping on a refusal it cannot fix (#1049, #1035,
   #1033, #1032), the same line protocol as datagrams over `udp` (#1037), snmp's reachability
   rung, a standard get (#821), and http and https polls and commands that read JSON, XML and HTML
   through one extraction primitive, authenticated by a declared scheme (#902, the first slice of
   #1113). Still ahead: [templates](/architecture/templates/) (the reusable
   device shape, #827), SNMP v3 (#1083), the held transports beyond those (#1034,
   #1036, #1038 to #1041), protocols that move whole files (#1043 to
   #1047), and server-hosted listeners (webhook, trap, syslog ingress, #1024).
2. **The data model.** Landed except one piece: [properties](/architecture/properties/) (the
   canonical-signal registry and the exclusive-arc owner columns), the sample sinks,
   [config and variables](/architecture/variables/) resolved down the
   [cascade](/architecture/cascade/) all shipped, and so has the
   [expression engine](/architecture/expressions/) the rules share. Still ahead on this band: the
   rest of the expression design (the Omniglass functions, unit conversion, and the sites beyond rule
   criteria and calc).
3. **Detection and verdict.** [Events](/architecture/events/) shipped (their registry, sink, and
   lineage columns), as did [commands](/architecture/commands/) with settlement, and the **verdict half
   has landed**: an alarm impairs a component's own verdict, an impaired role sinks its system by its
   impact, and the transitions record when it changed. What **produces** an alarm has landed too: the
   `event_rule` tier raises and clears alarms at a severity level, and component
   [calc rules](/architecture/calculations/) derive the samples rules judge. What remains is what
   **acts** on an alarm ([alarms and actions](/architecture/alarms-actions/), the automation tier).
4. **The machinery underneath.** The [messaging](/architecture/messaging/) subject contract, per-node
   subject isolation, and the durable JetStream telemetry consumer
   ([workers](/architecture/workers/)) shipped, beside three server loops that are not consumers (the
   command settlement sweep, the event rule sweep, and the calc tick). Still ahead: the raw/trusted
   two-lane data plane, [time](/architecture/time/) as a primitive (#419, where those loops converge),
   and the CDC bridge on
   [storage](/architecture/storage/).
5. **The read side, in full.** [Views](/architecture/views/) and the `ViewResult` renderer, composable
   dashboards, and the exploration surfaces on the [UI](/architecture/ui/), plus the
   [MCP](/architecture/api/#also-an-mcp-surface) and [AI](/architecture/ai/) seams over the same gateway.

This arc is a reading order, not a schedule. Each numbered band becomes one or more epics with their own
scope and approval before any branch; when a slice in it ships, the relevant spine page moves off
`Design` and, if the build differs from the prose, the difference is logged in
[decisions](/architecture/decisions/). That is how directional intent becomes built architecture without
ever maintaining two copies of it.
