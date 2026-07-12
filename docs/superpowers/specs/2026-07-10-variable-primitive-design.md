# Variable primitive, slice 1 (design)

Issue: [#183](https://github.com/hyperscaleav/omniglass/issues/183). Epic: #158. Follow-up scoping: #184.

## What

The **variable** member of the config / secret / variable trio: a typed, cascade-resolved
**plaintext** value (a macro), owned on the exclusive arc and resolved most-specific-wins down the
cascade. Mirrors the shipped secret slice (#170) minus crypto, masking, and audited reads. Variables
are shown in the clear.

## Decisions (settled in brainstorming)

- **Owner arc:** `global | location | system | component`, identical to `secret`. Template scope
  deferred to #184.
- **Typing:** `value jsonb` plus a `value_type` enum (`string | int | float | bool | json`),
  validated in Go on write (no registry, matching the "operator-defined, not curated" prose).
- **Consumer deferred:** no `$var:` interpolation this slice. The `interp.go` primitive already
  parses `$var:` and stays in package `secret`, untouched (relocating it is its own later refactor).
- **secret-flag deferred:** plaintext variables only.
- **Naming:** `effective-variables` (symmetric with `effective-secrets`).

## Units

Each is small, single-purpose, testable in isolation.

### `internal/variable` (pure, no I/O)
- `ValueType` enum + `ParseValueType(string)`.
- `ValidateValue(vt ValueType, raw json.RawMessage) error`: the jsonb value matches its declared type
  (a `bool` is a JSON boolean, an `int` a JSON integer, `json` any valid JSON, etc.). Pure, fully
  unit-tested with a table of valid/invalid pairs. This is the app-level typing seam.

### Storage (`internal/storage/variables.go`)
- Migration (additive, new dbmate file): `variable` table. Columns: `id`, `name`, `owner_kind`,
  `component_id | system_id | location_id` (exclusive-arc, one non-null; partial-unique index
  `(name, <tier>_id)` per tier and a partial-unique on name where `owner_kind = 'global'`, mirroring
  `secret`), `value_type text`, `value jsonb`, `created_at`, `updated_at`.
- Gateway methods (mirror `secret`, drop the crypto): `CreateVariable`, `ListVariables`,
  `UpdateVariable` (merge: an omitted value keeps the stored one), `DeleteVariable`,
  `ResolveVariables(ctx, componentName, read)` (the recursive-CTE cascade, reused resolver shape;
  ranks by band desc, depth asc; returns winner + shadowed candidates).
- `resolveSecretOwner` has a `variable` sibling (`resolveVariableOwner`) scope-checking the owner
  against the write scope via `inScopeTree`.
- Add `variable` to `scope.applicableKinds` returning `{location, system, component}` (the fix pattern
  shipped for `secret` in #170).
- Gateway interface + `UnimplementedGateway` double updated.

### API (`internal/api/variables.go`, Huma)
- `GET /variables` (all-scope list, `variable:read` floor), `POST /variables`
  (`variable:create`), `PATCH /variables/{id}` (`variable:update`), `DELETE /variables/{id}`
  (`variable:delete`), `GET /components/{name}/effective-variables` (`component:read` on the
  component, returns the resolved cascade).
- No `:reveal`. Reads return `value` directly (typed body: value carried as the raw JSON scalar).
- `make gen` regenerates OpenAPI, the typed SPA client, and the CLI.

### Authz (seed)
- `roles.yaml`: operator gains `variable:create,update` (delete stays admin `variable:*` + owner),
  the same split secret got.

### UI (`web/src/pages/Variables.tsx` + `web/src/lib/variables.ts`, mirror Secrets)
- Variables directory: columns name, **type** (own column), owner, value. Create/edit/delete through
  `BladeStack` + `DrawerFooter`, `TreeSelect` owner picker. Value input adapts to `value_type`.
- Component detail: `effective-variables` list -> cascade blade, top-to-bottom (global to component),
  winner checked. No reveal adornments (value shown inline).
- Nav entry under Settings, next to Secrets.

## Testing (test-first per unit)

- **Unit:** `internal/variable` value-validation table (pure).
- **Integration (testcontainer):** create at several scopes, jsonb round-trip per `value_type`,
  cascade precedence (component > system > deeper-location > global, winner + shadowed), scope split.
- **API e2e:** the cascade, the authz split (scoped-operator create/update 200; viewer create/delete
  403), the update merge.
- **Scope unit:** `variable` resolves against its three owner-arc kinds; a bare read floor grants no
  create.
- **Web unit:** the `variables.ts` data layer (request shape, envelope unwrap).

## Docs (same PR)

- `variables.md`: variable member Design -> Partial; keep the page Partial (config still Design).
- `status.mdx`: build-progress entry.
- Decision log: only if the build diverges from the page design.

## Out of scope

Template owner scope, cascade groups, weighted precedence (#184); the `$var:` consumer; secret-flag
variables. Config member unchanged.
