---
title: "A client applies a plan in one call under one import id"
date: 2026-11-23
status: "Accepted"
summary: "POST /plans:apply exposes storage.CreatePlan to clients: a plan they built (standard templates, locations by key, systems with slot-filling components, standalone components, declared values) lands in one transaction, each piece the ordinary create in the caller's scope. validate_only rolls it back; an apply stamps one import id into every audit row as from_import; a refusal names its path in the plan; a plan over 1000 rows is a 413. The route is gated by location:read and names any further permission the pieces need; there is no import capability."
refs: [2368, 827, 522, 2369]
---

- **Date:** 2026-11-23 | **Status:** Accepted | **Pages:** [templates](/architecture/templates/#a-client-applies-a-plan-directly), [the CLI](/developer/cli/#importing-a-plan)
- **Decision:** (#2368, extends [ADR-0827](#adr-0827-the-template-layer-owns-the-clone-model-and-the-componenttemplate-authoring-model-is-superseded-by-the-driver-spec), whose planned create had only the template routes and the simulator as consumers.)
  1. **A route of its own.** `POST /plans:apply` takes the plan's shape directly, with no
     template: `standards` (standard template names, each with its parent chain), `locations`
     (a `key` the plan refers to, under a `parent_key` of the plan or an existing `parent`),
     `systems` (at a `location_key` or an existing `location`, each component filling a named
     `slot`), standalone `components`, and `properties` on any of them. It lands through
     `storage.CreatePlan` unchanged in kind: every piece the ordinary create, the caller's own
     scope, its own audit row, one transaction.
  2. **A dry run in the primitive.** `Plan.ValidateOnly` runs the same transaction and rolls it
     back instead of committing, so a dry run refuses exactly what the apply would and reports the
     names it would mint. The response leaves the ids out, since the rows they named are gone.
  3. **Provenance in the audit detail.** `storage.WithImport(id)` sits beside `WithTemplate`, and
     the audit writer stamps both (`from_import`, `from_template`) into each row's new image. The id
     is a uuidv7 the handler mints per apply and returns; nothing else stores it.
  4. **A refusal names its piece.** `PlanError.Path` is the piece's place in the plan
     (`locations[2]`, `systems[1].components[0]`, `components[3]`), leading the mapped detail and
     set as the error's location; the status is the piece's own route's. A plan that refers to
     its own pieces wrongly (a `parent_key` or `location_key` it does not hold, a key given twice)
     is `storage.ErrPlanKey`, a 422 naming the row.
  5. **Bounded.** At most 1000 rows (each standard, location, system, component and declared value
     one); over it is a 413 naming the limit. A client chunks by building, each chunk its own plan
     and import id.
  6. **No new capability.** The gate is `location:read`, the read every placed piece needs; the
     handler names any of `location:create`, `system:create`, `system:read`, `system:update`,
     `component:create`, `component:read` and `component:update` the plan's pieces need before
     anything is read, and the storage checks each piece's scope as its create does.
- **Context:** Importing a fleet that already exists elsewhere (a spreadsheet, another system's records)
  one create at a time is thousands of calls with no rollback; the planned create already lands a
  composite whole and was one route away from serving it. CSV parsing and the agent recipe are
  #2369.
- **Not chosen:**
  - **An `import:create` permission.** It would let an import do what its caller could not do one
    create at a time, or else duplicate every per-piece check; the pieces' own permissions are the
    whole story.
  - **Endpoints in the plan.** An attach carries a driver's inputs and a secret reference; the
    template routes keep that, and an imported component attaches afterwards.
  - **Idempotent re-apply or resume.** A refused chunk lands nothing, so it is fixed and applied
    again; re-applying a chunk that landed duplicates it. Natural-key upsert is unfiled.
  - **A separate `:validate` verb.** The body is the same and AIP's `validate_only` field is the
    convention for it; the one route answers 200 for a dry run and 201 for an apply, so a dry run
    never reads as a create.
