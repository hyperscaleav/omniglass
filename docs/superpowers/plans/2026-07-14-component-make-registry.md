# component_make registry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `component_make` (a flat, normalized manufacturer registry) full-stack: storage, seed, API, generated CLI/client, a Catalog console page, docs, tests. Slice 1 of epic #254 (issue #255).

**Architecture:** Mirror the existing `component_type` registry end to end. A new `component_make` table, a Storage Gateway CRUD surface with the official-rows-read-only guard, five Huma routes gated by a new `make:<action>` permission, the generated typed client + cobra CLI from `make gen`, and a Catalog directory page built on the existing inventory-view primitive. Reference entity to copy structure from: `component_type` in `internal/storage/components.go`, `internal/api/components.go`, `internal/seed/`, `web/src/pages/Types.tsx`.

**Tech Stack:** Go (Huma API, pgx), PostgreSQL via dbmate migrations + testcontainers-go, SolidJS + daisyUI SPA, generated openapi-fetch client + cobra CLI.

## Global Constraints

- API first: the Huma structs are the source of truth; `make gen` regenerates the OpenAPI, the typed client, and the CLI. A route change without a matching `make gen` is drift and fails CI.
- Test first: every behavior change ships a test that failed before and passes after. `make test` (or `make test-short` to iterate) is green before commit.
- No mocking the database: integration tests use `testcontainers-go` (ephemeral PG on a random port, never a fixed host port).
- Migrations run exactly once, are never edited after applied, are pure idempotent DDL, and carry NO seed rows (seed rows live in the boot seed phase).
- House style: no em dashes in any artifact; no AI/assistant attribution anywhere; head-noun-last naming (`<qualifier>_<genus>`); PR title and commit subjects lowercase the word after the conventional-commit prefix (`feat: component_make ...`).
- Authorization is two layers on every applicable route: a `<resource>:<action>` permission check, and (where the entity is scoped) ABAC scope from the Storage Gateway. `component_make` is tenant-wide (no scope), so only the permission layer applies, exactly as `component_type` is all-scope.
- Every operator-facing change ships live screenshots in the PR (`make dev`), per `/ship-slice`.

### Column decision (flag for Fred)

The spec named `origin (official | seed | custom)`. To match the shipped `component_type` precedent exactly (which uses an `official boolean`), this slice uses **`official boolean`** (true = seed-owned, read-only). The three-way `origin` enum is not built here; if Fred wants it, it is a small follow-up that also touches `component_type`. Note this deviation in the decision log.

### Referential guard (deferred to slice 3)

`component_type` refuses delete while an inventory row references it (409). Nothing references `component_make` until `component_model` lands in slice 3 (#257). So this slice ships **no in-use delete guard**; slice 3 adds the `component_make` reference to the guard. Delete here only refuses official rows (422).

---

### Task 1: Migration + storage CRUD + Gateway interface

**Files:**
- Create: `db/migrations/<TS>_component_makes.sql` (pick `<TS>` = a 14-digit UTC stamp AFTER the latest in `db/migrations/`; run `ls db/migrations/ | tail -3` and bump past it, e.g. `20260715090000`)
- Create: `internal/storage/component_makes.go` (struct, CRUD, Upsert, guard)
- Modify: `internal/storage/main.go` (add 5 method signatures to the `Gateway` interface, and to the in-memory double if `main.go`/a `mem*.go` declares one)
- Test: `internal/storage/component_makes_test.go`

**Interfaces:**
- Produces (storage domain, consumed by Tasks 2 and 3):
  - `type ComponentMake struct { ID, DisplayName, Icon, SupportPhone, Website string; Official bool; CreatedAt, UpdatedAt time.Time }`
  - `type ComponentMakePatch struct { DisplayName, Icon, SupportPhone, Website *string }`
  - `Gateway.CreateComponentMake(ctx, ComponentMake) (ComponentMake, error)`
  - `Gateway.GetComponentMake(ctx, id string) (ComponentMake, error)`
  - `Gateway.ListComponentMakes(ctx) ([]ComponentMake, error)`
  - `Gateway.UpdateComponentMake(ctx, id string, ComponentMakePatch) (ComponentMake, error)`
  - `Gateway.DeleteComponentMake(ctx, id string) error`
  - `Gateway.UpsertComponentMake(ctx, ComponentMake) error` (boot seed path)
  - Reuses the existing `ErrTypeOfficial` sentinel (rename-agnostic: use the same official-guard error the registry already returns for a read-only row; if it is registry-specific, add `ErrMakeOfficial` mapping to 422).

- [ ] **Step 1: Write the failing integration test**

Mirror `internal/storage/component_types_test.go`. Create `internal/storage/component_makes_test.go`:

```go
func TestComponentMakeCRUD(t *testing.T) {
	gw := newTestGateway(t) // same testcontainer helper the type tests use
	ctx := testCtx(t)

	// create
	m, err := gw.CreateComponentMake(ctx, ComponentMake{
		ID: "acme", DisplayName: "Acme", Website: "https://acme.example",
	})
	require.NoError(t, err)
	require.Equal(t, "acme", m.ID)
	require.False(t, m.Official)

	// get + list
	got, err := gw.GetComponentMake(ctx, "acme")
	require.NoError(t, err)
	require.Equal(t, "Acme", got.DisplayName)
	all, err := gw.ListComponentMakes(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)

	// update patch (display + support phone)
	dn, ph := "Acme Inc.", "+1-555-0100"
	upd, err := gw.UpdateComponentMake(ctx, "acme", ComponentMakePatch{DisplayName: &dn, SupportPhone: &ph})
	require.NoError(t, err)
	require.Equal(t, "Acme Inc.", upd.DisplayName)
	require.Equal(t, "+1-555-0100", upd.SupportPhone)

	// official row is read-only
	require.NoError(t, gw.UpsertComponentMake(ctx, ComponentMake{ID: "official-co", DisplayName: "Official Co", Official: true}))
	_, err = gw.UpdateComponentMake(ctx, "official-co", ComponentMakePatch{DisplayName: &dn})
	require.ErrorIs(t, err, ErrTypeOfficial) // or ErrMakeOfficial
	require.ErrorIs(t, gw.DeleteComponentMake(ctx, "official-co"), ErrTypeOfficial)

	// delete a custom row
	require.NoError(t, gw.DeleteComponentMake(ctx, "acme"))
	_, err = gw.GetComponentMake(ctx, "acme")
	require.ErrorIs(t, err, ErrNotFound) // same not-found sentinel the type registry uses
}
```

- [ ] **Step 2: Run it, verify it fails to compile/find the methods**

Run: `go test ./internal/storage/ -run TestComponentMakeCRUD -count=1`
Expected: FAIL (undefined: `CreateComponentMake`, `ComponentMake`, etc).

- [ ] **Step 3: Write the migration**

Pure idempotent DDL, no seed rows. `db/migrations/<TS>_component_makes.sql`:

```sql
-- migrate:up
CREATE TABLE IF NOT EXISTS component_make (
    id            text PRIMARY KEY,
    display_name  text NOT NULL,
    icon          text NOT NULL DEFAULT '',
    support_phone text NOT NULL DEFAULT '',
    website       text NOT NULL DEFAULT '',
    official      boolean NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- migrate:down
DROP TABLE IF EXISTS component_make;
```

- [ ] **Step 4: Write the storage implementation**

Create `internal/storage/component_makes.go`, copying the shape of the `ComponentType` CRUD funcs in `internal/storage/components.go` (same `guardTypeMutable` official check, same audit call on delete, same `ORDER BY display_name, id` on list, same patch-only-provided-fields pattern). Add the 6 method signatures to the `Gateway` interface in `internal/storage/main.go` and implement them on the in-memory double if one exists.

- [ ] **Step 5: Run the test, verify it passes**

Run: `go test ./internal/storage/ -run TestComponentMakeCRUD -count=1`
Expected: PASS.

- [ ] **Step 6: Apply the migration locally and sanity-check**

Run: `make migrate` (or the project's migrate run-mode) against the dev DB; confirm `component_make` exists. Do NOT commit any generated schema dump unless the repo tracks one.

- [ ] **Step 7: Commit**

```bash
git add db/migrations internal/storage
git commit -m "feat: component_make storage registry with official-read-only guard"
```

---

### Task 2: Seed official makes

**Files:**
- Create: `internal/seed/component_makes.yaml`
- Modify: `internal/seed/seed.go` (add `//go:embed component_makes.yaml`, a `componentMakesYAML` var, and a `seedComponentMakes()` that upserts, wired into the seed entrypoint next to `seedComponentTypes()`)
- Test: extend `internal/seed`'s existing seed test (or `internal/storage/component_makes_test.go`) with an idempotency assertion.

**Interfaces:**
- Consumes: `Gateway.UpsertComponentMake` (Task 1).

- [ ] **Step 1: Write the failing seed idempotency test**

Add to the seed test suite (mirror how `seedComponentTypes` is tested):

```go
func TestSeedComponentMakesIdempotent(t *testing.T) {
	gw := newTestGateway(t)
	ctx := testCtx(t)
	require.NoError(t, seedComponentMakes(ctx, gw))
	require.NoError(t, seedComponentMakes(ctx, gw)) // second run must not error or duplicate
	all, err := gw.ListComponentMakes(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, all)
	for _, m := range all {
		require.True(t, m.Official, "seeded makes are official/read-only")
	}
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./internal/seed/ -run TestSeedComponentMakesIdempotent -count=1`
Expected: FAIL (undefined `seedComponentMakes`).

- [ ] **Step 3: Write the seed YAML**

`internal/seed/component_makes.yaml` (a small starter set of common AV/IT makes; `official: true` is implied by the seed path). Keep it short and real:

```yaml
- id: crestron
  display_name: Crestron
  website: https://www.crestron.com
- id: biamp
  display_name: Biamp
  website: https://www.biamp.com
- id: qsc
  display_name: QSC
  website: https://www.qsc.com
- id: shure
  display_name: Shure
  website: https://www.shure.com
- id: cisco
  display_name: Cisco
  website: https://www.cisco.com
- id: extron
  display_name: Extron
  website: https://www.extron.com
- id: sony
  display_name: Sony
  website: https://www.sony.com
- id: samsung
  display_name: Samsung
  website: https://www.samsung.com
```

- [ ] **Step 4: Wire the seed function**

In `internal/seed/seed.go`, mirror `seedComponentTypes`: embed the YAML, unmarshal, set `Official: true` on each row, call `gw.UpsertComponentMake`, and invoke `seedComponentMakes` from the same place `seedComponentTypes` is invoked.

- [ ] **Step 5: Run the test, verify it passes**

Run: `go test ./internal/seed/ -run TestSeedComponentMakesIdempotent -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/seed
git commit -m "feat: seed official component makes on boot"
```

---

### Task 3: Huma API + permission + generated client/CLI

**Files:**
- Create: `internal/api/component_makes.go` (wire structs, 5 handlers, `registerComponentMakeRoutes`)
- Modify: `internal/api/api.go` (call `registerComponentMakeRoutes` next to `registerComponentRoutes`)
- Modify: the permission/resource registry (locate it as `component_type` did; grep for where `type` actions are declared, e.g. a permissions catalog file) to add `make:read|create|update|delete`, with `make:read` in the viewer read-floor and create/update/delete at the admin tier (mirror `type:create` exactly)
- Regenerate: `internal/api_gen.go` / the generated client + CLI via `make gen`
- Test: `internal/api/component_makes_e2e_test.go`

**Interfaces:**
- Consumes: the `Gateway` methods (Task 1).
- Produces (wire shape, consumed by Task 4 via the generated client):
  - `GET /component-makes` -> `{ makes: MakeBody[] }`
  - `POST /component-makes` (body: id, display_name, icon?, support_phone?, website?)
  - `GET /component-makes/{id}` -> `MakeBody`
  - `PATCH /component-makes/{id}` (body: display_name?, icon?, support_phone?, website?)
  - `DELETE /component-makes/{id}`
  - `MakeBody = { id, display_name, icon, support_phone, website, official }`
  - Route path noun matches house convention (single-noun collections like `/files`, `/tags`); if the codebase prefers `/makes`, use that consistently across API, client, CLI, and Task 4.

- [ ] **Step 1: Write the failing e2e test**

Mirror `internal/api/component_types_e2e_test.go`:

```go
func TestComponentMakesAPI(t *testing.T) {
	srv := newTestServer(t) // real PG + seed, same helper the type e2e uses
	admin := srv.token(t, "owner")
	viewer := srv.token(t, "viewer")

	// viewer can read (read-floor)
	var listed struct{ Makes []map[string]any `json:"makes"` }
	srv.get(t, "/component-makes", viewer, 200, &listed)
	require.NotEmpty(t, listed.Makes) // seeded makes present

	// viewer cannot create (403)
	srv.post(t, "/component-makes", viewer, map[string]any{"id": "nope", "display_name": "Nope"}, 403, nil)

	// admin creates
	var created map[string]any
	srv.post(t, "/component-makes", admin, map[string]any{"id": "acme", "display_name": "Acme"}, 201, &created)
	require.Equal(t, "acme", created["id"])

	// official (seeded) row is read-only -> 422
	srv.patch(t, "/component-makes/crestron", admin, map[string]any{"display_name": "X"}, 422, nil)

	// admin deletes the custom row
	srv.delete(t, "/component-makes/acme", admin, 204)
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./internal/api/ -run TestComponentMakesAPI -count=1`
Expected: FAIL (404 no route / undefined handler).

- [ ] **Step 3: Implement the API + permission**

Create `internal/api/component_makes.go` copying `component_type`'s handler structure (same `mapTypeErr` for 404/409/422, same per-route `huma.Register` with the permission middleware). Register the `make:*` permission resource where `type:*` is declared, with `make:read` in the viewer floor and mutations at the admin tier. Wire `registerComponentMakeRoutes` into `internal/api/api.go`.

- [ ] **Step 4: Regenerate the client + CLI**

Run: `make gen`
Expected: `internal/api_gen.go` and the generated TS client/CLI now include the `component-makes` operations. Verify no unrelated drift.

- [ ] **Step 5: Run the e2e test, verify it passes**

Run: `go test ./internal/api/ -run TestComponentMakesAPI -count=1`
Expected: PASS.

- [ ] **Step 6: Confirm no gen drift**

Run: `make gen && git diff --exit-code` (generated files) — must be clean.

- [ ] **Step 7: Commit**

```bash
git add internal/api internal/api_gen.go web/src # generated client
git commit -m "feat: component_make API, make permission, generated client and CLI"
```

---

### Task 4: Catalog console page

**REQUIRED SUB-SKILL:** Use the `add-inventory-view` skill for this task (it encodes the rail: generated client + hand-written ListView config, data layer, page config, routing/nav, the test rail, the invariants).

**Files:**
- Create: `web/src/lib/component_makes.ts` (data layer over the generated client: `listMakes`, `createMake`, `updateMake`, `deleteMake`)
- Create: `web/src/pages/ComponentMakes.tsx` (flat directory + detail blade + create/edit/delete, official rows read-only)
- Modify: `web/src/lib/nav.ts` (add a Makes entry under the Catalog group)
- Modify: `web/src/index.tsx` (route `/component-makes` -> the page)
- Test: `web/src/pages/ComponentMakes.test.tsx`

**Interfaces:**
- Consumes: the generated client operations from Task 3.

- [ ] **Step 1: Write the failing UI test**

Mirror the Types/tags page test. `web/src/pages/ComponentMakes.test.tsx`:

```tsx
test("lists makes and opens the create form", async () => {
  renderWithProviders(() => <ComponentMakes />, { seededMakes: [{ id: "crestron", display_name: "Crestron", official: true }] });
  expect(await screen.findByText("Crestron")).toBeInTheDocument();
  // official row has no edit/delete
  fireEvent.click(screen.getByText("Crestron"));
  expect(screen.queryByRole("button", { name: /delete/i })).not.toBeInTheDocument();
  // create is available to an admin
  fireEvent.click(screen.getByRole("button", { name: /new make/i }));
  expect(screen.getByLabelText(/display name/i)).toBeInTheDocument();
});
```

- [ ] **Step 2: Run it, verify it fails**

Run: `cd web && npx vitest run src/pages/ComponentMakes.test.tsx`
Expected: FAIL (module not found).

- [ ] **Step 3: Build the data layer + page + nav + route**

Follow `add-inventory-view`. Copy `web/src/lib/types.ts` + `web/src/pages/Types.tsx` structure for a FLAT (non-tree) entity; add the nav entry under Catalog and the route. Icon/support/website are plain form fields; official rows render read-only (no edit/delete), matching the Types page.

- [ ] **Step 4: Run the UI test, verify it passes**

Run: `cd web && npx vitest run src/pages/ComponentMakes.test.tsx`
Expected: PASS.

- [ ] **Step 5: Run the full web suite**

Run: `cd web && npx vitest run`
Expected: all green (no regression in the existing suite).

- [ ] **Step 6: Commit**

```bash
git add web/src
git commit -m "feat: component makes catalog page"
```

---

### Task 5: Docs, spec/plan check-in, and ship

**Files:**
- Create: `docs/src/content/docs/guides/admin/makes.md` (or the Catalog guide location matching the Types guide) — the operator page for the makes registry
- Modify: `docs/src/content/docs/architecture/core-entities.md` (note the make layer above `component_type`, mark it as the first landed piece of the catalog)
- Modify: `docs/src/content/docs/architecture/status.mdx` (build-progress note) and the relevant status badge to its new floor
- Modify: the decision log `docs/src/content/docs/architecture/decisions.md` (the `official boolean` vs `origin enum` deviation, and the deferred referential guard)
- Modify: `docs/astro.config.mjs` sidebar (register the new guide page — the easy-to-miss step)
- Move into the branch: this plan and `docs/superpowers/specs/2026-07-14-component-make-model-catalog-design.md` (they ride slice 1)

- [ ] **Step 1: Write the docs**

Author the makes guide (what the registry is, official-vs-custom, create/edit/delete, admin-gated), the core-entities note, the status.mdx line, and the decision-log entries. Register the guide in the Starlight sidebar config. No em dashes.

- [ ] **Step 2: Build the docs to verify no broken links/sidebar**

Run: `cd docs && npm run build` (or the repo's docs build)
Expected: build succeeds; the new page is linked.

- [ ] **Step 3: Full gate**

Run: `make test`
Expected: all green. Run `make gen && git diff --exit-code` — clean.

- [ ] **Step 4: Commit docs**

```bash
git add docs
git commit -m "docs: component makes registry guide, core-entities note, status, decisions"
```

- [ ] **Step 5: Run `/ship-slice`**

Invoke the `ship-slice` skill: fresh `make test` with output pasted, `make gen` drift check, em-dash + attribution scan, reviewer pass, docs-with-everything, and **live screenshots** of the makes directory + create form + an official read-only blade (driven against `make dev`). Its emitted ship-review becomes the PR body.

- [ ] **Step 6: Open the PR**

```bash
git push -u origin feat/component-make
gh pr create --repo hyperscaleav/omniglass --title "feat: component_make manufacturer registry" --body-file <ship-review>
```

PR closes #255, refs epic #254.

---

## Self-review notes

- **Spec coverage:** slice 1 of the spec = `component_make` registry (name, display_name, icon, support_phone, website, origin, scoped CRUD, CLI, client, Catalog surface, seed official makes, official read-only, delete-refused-while-referenced). Covered by Tasks 1-5, with two explicit, logged deviations: `official boolean` in place of the `origin` enum (consistency with `component_type`), and the referential delete guard deferred to slice 3 (nothing references makes yet). Both flagged for Fred in Global Constraints and the decision log.
- **Open lookups for the executor (not placeholders, but require a grep against the live tree):** the exact permission/resource registry file where `type:*` is declared; whether an in-memory Gateway double exists to update; the exact test helpers (`newTestGateway`, `newTestServer`, `renderWithProviders`) names — use whatever the `component_type` tests use verbatim; the migration timestamp (must be unique and latest). Each is resolved by reading the named precedent file.
