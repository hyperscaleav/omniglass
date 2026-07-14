# Types catalog: an operator registry for the classifiers

Status: design, approved. Date: 2026-07-13.

## Problem

Every inventory entity is classified by a type drawn from a small registry:
`location_type` (campus, building, ...), `system_type`, `component_type`, and
`secret_type` (the shape a secret takes). Those registries ship as boot seed and are
today **list-only**: `GET /location-types` and `GET /secret-types` exist, but
`system_type` and `component_type` have no route at all, so
[Systems.tsx](../../../web/src/pages/Systems.tsx) and
[Components.tsx](../../../web/src/pages/Components.tsx) fake the type picker from the
distinct values of existing rows. There is no operator surface to view the registries,
and no way to add a custom type without editing seed YAML and rebuilding.

The `Catalog` nav group already reserves a **Types** slot (stubbed,
[nav.ts](../../../web/src/lib/nav.ts)); `/types` is a `STUBS` route.

## The model in one line

One **`type`** permission resource gates a small CRUD registry API for the three
id/display/rank registries (location, system, component); a single unified **Types**
catalog page renders all four kinds behind a kind facet, editing the three writable ones
and viewing `secret_type` read-only. Seed owns the `official` rows; operators own custom
rows.

## Decisions (settled)

- **Structure:** one unified `Types` page, kind facet (Location / System / Component /
  Secret), on the `FlatList` shell that [Tags.tsx](../../../web/src/pages/Tags.tsx) uses.
- **Mutability:** full CRUD on location/system/component types; `secret_type` read-only
  this cycle.
- **Authz:** a new `type` resource, capability-only (unscoped, global reference data):
  `type:read` gates list + nav; `type:create` / `type:update` / `type:delete` gate writes.
- **Official vs custom:** seeded rows (`official = true`) are read-only (locked in the UI,
  422 on write); operators create/edit/delete only custom rows (`official = false`). The
  boot seed keeps `ON CONFLICT DO UPDATE` on official ids; operator rows are untouched.
- **Delete guard:** deleting a type still referenced by inventory is refused (409), reusing
  the `occupied` sentinel ([scopedcrud.go](../../../internal/storage/scopedcrud.go)) plus a
  reference count that names the blocking rows.

## Scope: two PRs

### PR 1 (this spec's build) - backend `type` registry CRUD

API first, no UI. Delivers the registry API + CLI (`make gen`) + storage + tests + docs,
and **reworks locations** off the borrowed gate.

- **New `type` resource** in the RBAC catalog + seed role grants + `authz_matrix`
  conformance ([internal/rbac/rbac.go](../../../internal/rbac/rbac.go)). Reads to the
  reader floor, writes to admin/editor (exact mapping pinned in the plan).
- **Per-registry routes** (own `internal/api/*.go` home, mirroring the shipped
  `list-location-types`), for location, system, component:

  | verb | route | gate |
  |---|---|---|
  | list | `GET /{kind}-types` | `type:read` |
  | create | `POST /{kind}-types` | `type:create` |
  | update | `PATCH /{kind}-types/{id}` | `type:update` |
  | delete | `DELETE /{kind}-types/{id}` | `type:delete` |

  Body `{id, display_name, rank}` (+ `icon` on location). `official` is response-only;
  create always writes `official = false`. Update/delete of an `official` row -> 422.
  Delete of an in-use row -> 409 with a reference count.
- **Locations rework:** `list-location-types` read gate moves `location:read` ->
  `type:read`; the seed grants `type:read` alongside the inventory reads so the Locations
  form picker keeps working. Add create/update/delete to location-types (list-only today).
- **Storage:** reuse `UpsertLocationType`; add `Upsert{System,Component}Type` +
  `Delete{Location,System,Component}Type`, each with the reference-count check returning the
  `occupied` 409. In-memory Gateway double updated for the interface.
- **`secret_type`:** unchanged (list stays read-only, no write routes).
- **Enables retiring the workaround:** shipping the `/system-types` and `/component-types`
  list routes gives the Systems/Components pickers a real source; the picker rewiring
  itself is a UI change and lands in PR2.
- **Migration:** none expected (tables already carry `official default false` + the parent
  FK). Open item: confirm the parent FK is `ON DELETE RESTRICT`; if not, the gateway count
  is the sole guard (acceptable) or add a migration to set RESTRICT. Decide in the plan.

### PR 2 (next spec/plan) - the Types catalog page

`web/src/pages/Types.tsx` on `FlatList`: kind facet; columns `kind`, `id`, `display_name`,
`rank`, `official` (lock badge), `icon` preview on location rows; CRUD drawer for the three
writable kinds (official rows locked); `secret_type` view-only with a "fields editor
coming" note linking the follow-up issue. Flip the `Types` nav entry `live: true`,
`resource: "type"`; drop `/types` from `STUBS`; wire the route. Point the Systems/Components
pickers at the real list endpoints. Ships operator-guide docs + screenshots.

## Testing (test-first, PR1)

- **E2E per registry**, mirroring
  [location_types_e2e_test.go](../../../internal/api/location_types_e2e_test.go): list,
  create custom, update, delete, 422 on official, 409 on in-use, 403 without `type:*`.
- **Storage:** upsert + delete + reference-count round-trips (testcontainer).
- **Authz matrix** row for the `type` resource.

## Docs (PR1)

Architecture note that Types is capability-only/unscoped reference data and how the `type`
resource relates to the inventory resources; advance the affected status badges. The
operator how-to ships with the page in PR2.

## Out of scope (follow-up issues)

- `secret_type` write (the fields-schema editor: add/remove typed fields with secret +
  origin, plus its create/update/delete API).
- Reordering / re-iconing official rows from the UI.
- Namespace-shadow custom types (the official/private registry pattern), if custom types
  should ever be namespace-scoped rather than plain `official = false` rows.
