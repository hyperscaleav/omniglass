# component_model registry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `component_model` (a make+model product catalog) full-stack: storage, dev-seed, API, generated CLI/client, a Models console directory (make column + make filter + front/back image upload), docs, tests. Slice 2 of epic #254 (issue #257).

**Architecture:** Mirror the merged `component_make` registry end to end (it is the direct precedent, on this branch's base). `component_model` adds a required `make_id` FK to `component_make`, nullable `front_image_id`/`back_image_id` FKs to `file` (the #246 files primitive), and no type/classification field (deferred to the roles slice #256). This slice also lands the make-in-use delete guard deferred from #255. Reference files to copy structure from: `internal/storage/component_makes.go`, `internal/api/component_makes.go`, `internal/seed/*` (for wiring patterns), `internal/devseed/fixtures.yaml`, `web/src/lib/component_makes.ts`, `web/src/pages/ComponentMakes.tsx`, and the files upload flow in `web/src/pages/Files.tsx` / `web/src/lib/files.ts`.

**Tech Stack:** Go (Huma, pgx), PostgreSQL via dbmate + testcontainers-go, SolidJS + daisyUI SPA, generated openapi-fetch client + cobra CLI.

## Global Constraints

- API first: Huma structs are the source of truth; `make gen` regenerates OpenAPI + typed client + CLI. A route change without a matching `make gen` is drift and fails CI.
- Test first: every behavior ships a test that failed before and passes after. `make test` is green before commit.
- No mocking the database: integration tests use `testcontainers-go` (ephemeral PG, random port).
- Migrations run once, never edited after applied, pure idempotent DDL, NO seed rows (seed lives in the boot seed phase; dev examples live in `internal/devseed`).
- Authorization is an invariant: every route carries a `<resource>:<action>` permission. `model:read` sits on the viewer read-floor exactly as `make:read` does; `model:create|update|delete` at the admin tier exactly as `make:*` mutations. `component_model` is tenant-wide (no ABAC scope), like `component_make`.
- House style: no em dashes, no AI/assistant attribution, head-noun-last naming, PR title + commit subjects lowercase after the conventional-commit prefix.
- Every operator-facing change ships live screenshots in the PR (`/ship-slice`).

### Decisions (from the spec, do not deviate)

- **No `type`/classification field on the model** this slice (roles model is #256).
- **`official boolean`** (read-only when true), mirroring `component_make`.
- Images are **file FKs** (`front_image_id`/`back_image_id` -> `file`, nullable); the UI uploads via the existing files API then sets the id; the model API only takes the FKs.
- The **make-in-use delete guard** lands here: `DeleteComponentMake` now returns 409 while a `component_model` references the make.

---

### Task 1: Migration + storage CRUD + Gateway + make-in-use guard

**Files:**
- Create: `db/migrations/<TS>_component_models.sql` (`<TS>` = a 14-digit UTC stamp AFTER the latest in `db/migrations/`; run `ls db/migrations | tail -3` and bump past it)
- Create: `internal/storage/component_models.go` (struct, CRUD, Upsert, guard)
- Modify: `internal/storage/component_makes.go` (add the in-use guard to `DeleteComponentMake`)
- Modify: the `Gateway` interface in `internal/storage/storage.go` (+ the `UnimplementedGateway` stub) with the 6 `ComponentModel` methods
- Test: `internal/storage/component_models_test.go`; extend `internal/storage/component_makes_test.go` with the in-use-guard case

**Interfaces (produced, consumed by Tasks 2-4):**
- `type ComponentModel struct { ID, DisplayName, MakeID, ModelNumber, Family string; ReleasedAt, EosAt, EolAt *time.Time; FrontImageID, BackImageID *string; Official bool; CreatedAt, UpdatedAt time.Time }`
- `type ComponentModelPatch struct { DisplayName, ModelNumber, Family, FrontImageID, BackImageID *string; ReleasedAt, EosAt, EolAt *time.Time }`
- `CreateComponentModel(ctx, actorID string, m ComponentModel) (*ComponentModel, error)`, `GetComponentModel(ctx, id) (*ComponentModel, error)`, `ListComponentModels(ctx) ([]ComponentModel, error)`, `UpdateComponentModel(ctx, actorID, id, ComponentModelPatch) (*ComponentModel, error)`, `DeleteComponentModel(ctx, actorID, id) error`, `UpsertComponentModel(ctx, ComponentModel) error` (dev-seed path). Mirror the exact signature shape of the `ComponentMake` methods (actorID-first mutations, actor-less Upsert, pointer returns, shared `ErrType*` sentinels).

- [ ] **Step 1: Write the failing integration test**

Mirror `internal/storage/component_makes_test.go`. `internal/storage/component_models_test.go`:

```go
func TestComponentModelCRUD(t *testing.T) {
	pg := storagetest.NewPG(t)                 // use whatever helper component_makes_test.go uses
	ctx := context.Background()
	require.NoError(t, seed.Run(ctx, pg))      // seeds makes (crestron etc.) this model references

	// create referencing a seeded make
	m, err := pg.CreateComponentModel(ctx, "tester", storage.ComponentModel{
		ID: "acme-123a", DisplayName: "Acme 123A", MakeID: "crestron", ModelNumber: "123A", Family: "1xx-series",
	})
	require.NoError(t, err)
	require.Equal(t, "crestron", m.MakeID)
	require.False(t, m.Official)

	// create with a nonexistent make -> FK error (map to 422/404 at API; assert an error here)
	_, err = pg.CreateComponentModel(ctx, "tester", storage.ComponentModel{ID: "bad", DisplayName: "Bad", MakeID: "nope", ModelNumber: "X"})
	require.Error(t, err)

	// get + list contains our model
	got, err := pg.GetComponentModel(ctx, "acme-123a")
	require.NoError(t, err)
	require.Equal(t, "Acme 123A", got.DisplayName)
	all, err := pg.ListComponentModels(ctx)
	require.NoError(t, err)
	require.True(t, containsModel(all, "acme-123a"))

	// patch (family), other fields unchanged
	fam := "2xx-series"
	upd, err := pg.UpdateComponentModel(ctx, "tester", "acme-123a", storage.ComponentModelPatch{Family: &fam})
	require.NoError(t, err)
	require.Equal(t, "2xx-series", upd.Family)
	require.Equal(t, "123A", upd.ModelNumber)

	// delete
	require.NoError(t, pg.DeleteComponentModel(ctx, "tester", "acme-123a"))
	_, err = pg.GetComponentModel(ctx, "acme-123a")
	require.ErrorIs(t, err, storage.ErrTypeNotFound)
}
```

And in `component_makes_test.go`, a make-in-use guard case:

```go
func TestDeleteComponentMakeInUse(t *testing.T) {
	pg := storagetest.NewPG(t)
	ctx := context.Background()
	require.NoError(t, seed.Run(ctx, pg))
	_, err := pg.CreateComponentModel(ctx, "tester", storage.ComponentModel{ID: "m1", DisplayName: "M1", MakeID: "crestron", ModelNumber: "1"})
	require.NoError(t, err)
	// deleting a make a model references is refused
	require.ErrorIs(t, pg.DeleteComponentMake(ctx, "tester", "crestron"), storage.ErrTypeInUse) // or the sentinel the type registry uses for 409
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./internal/storage/ -run 'TestComponentModelCRUD|TestDeleteComponentMakeInUse' -count=1`
Expected: FAIL (undefined methods; DeleteComponentMake does not yet guard).

- [ ] **Step 3: Write the migration**

`db/migrations/<TS>_component_models.sql`:

```sql
-- migrate:up
CREATE TABLE IF NOT EXISTS component_model (
    id             text PRIMARY KEY,
    display_name   text NOT NULL,
    make_id        text NOT NULL REFERENCES component_make(id),
    model_number   text NOT NULL DEFAULT '',
    family         text NOT NULL DEFAULT '',
    released_at    timestamptz,
    eos_at         timestamptz,
    eol_at         timestamptz,
    front_image_id text REFERENCES file(id),
    back_image_id  text REFERENCES file(id),
    official       boolean NOT NULL DEFAULT false,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS component_model_make_id_idx ON component_model(make_id);

-- migrate:down
DROP TABLE IF EXISTS component_model;
```
(Confirm the `file` table PK column name and type by reading the #246 migration; adjust the FK if it is not `file(id)`.)

- [ ] **Step 4: Write the storage impl + make-in-use guard**

Create `internal/storage/component_models.go` mirroring `ComponentType`/`ComponentMake` CRUD (official guard, audited mutations via `writeAuditRes`, `ORDER BY display_name, id`, patch-only-provided-fields, `scanComponentModel`). Add the 6 methods to the `Gateway` interface (`storage.go`) and the `UnimplementedGateway` stub. In `component_makes.go`, add to `DeleteComponentMake` a referential guard: `SELECT count(*) FROM component_model WHERE make_id = $1` before delete; if > 0 return the in-use sentinel (409). Use the same `typeRef`/count pattern the type registry uses for its in-use guard.

- [ ] **Step 5: Run tests, verify pass**

Run: `go test ./internal/storage/... -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add db/migrations internal/storage
git commit -m "feat: component_model storage with make-in-use delete guard"
```

---

### Task 2: Dev-seed example models

**Files:**
- Modify: `internal/devseed/fixtures.yaml` (add a few example models referencing seeded makes)
- Modify: the devseed loader if models need explicit wiring (mirror how other entities in fixtures.yaml are applied); Test: extend the devseed test if one exists.

- [ ] **Step 1: Write/extend the failing dev-seed test**

Mirror the existing devseed test (find it: `internal/devseed/*_test.go`). Assert that after applying dev-seed, `ListComponentModels` returns the example models and each references an existing make.

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./internal/devseed/... -count=1`
Expected: FAIL (no models seeded).

- [ ] **Step 3: Add fixtures + wiring**

Add 2-3 example models to `internal/devseed/fixtures.yaml` referencing seeded makes (`make_id: crestron`, `make_id: biamp`), no images. Wire them the same way the file's other entities are applied (idempotent upsert via `UpsertComponentModel`).

- [ ] **Step 4: Run test, verify pass**

Run: `go test ./internal/devseed/... -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/devseed
git commit -m "feat: dev-seed example component models"
```

---

### Task 3: API + model permission + generated client/CLI

**Files:**
- Create: `internal/api/component_models.go` (wire structs, 5 handlers, `registerComponentModelRoutes`)
- Modify: `internal/api/api.go` (call it next to `registerComponentMakeRoutes`)
- Modify: the permission grant registry (`internal/seed/roles.yaml`) to add `model:create,update,delete` at admin, exactly beside `make:create,update,delete`; confirm `model:read` rides the viewer `*:read` floor (add `model` to `sensitiveResources` only if it should NOT, which it should not, so leave it out) mirroring `make`
- Regenerate: client + CLI via `make gen`
- Test: `internal/api/component_models_e2e_test.go`

**Interfaces (wire shape):** `GET /component-models` -> `{models: ModelBody[]}`; `POST/GET/PATCH/DELETE /component-models/{id}`; `ModelBody = {id, display_name, make_id, model_number, family, released_at, eos_at, eol_at, front_image_id, back_image_id, official}`. Errors: not-found 404, dup 409, official 422, unknown make 422 (or 404), make-in-use surfaces on the make delete route.

- [ ] **Step 1: Write the failing e2e test**

Mirror `internal/api/component_makes_e2e_test.go`:

```go
func TestComponentModelsAPI(t *testing.T) {
	srv := newTestServer(t)  // same helper component_makes_e2e_test.go uses (real PG + seed)
	admin := srv.principalWithGrants(t, "owner")
	viewer := srv.principalWithGrants(t, "viewer")

	srv.get(t, "/component-models", viewer, 200, nil)                                   // viewer reads (floor)
	srv.post(t, "/component-models", viewer, map[string]any{"id":"x","display_name":"X","make_id":"crestron","model_number":"1"}, 403, nil) // viewer cannot create
	var created map[string]any
	srv.post(t, "/component-models", admin, map[string]any{"id":"acme-123a","display_name":"Acme 123A","make_id":"crestron","model_number":"123A"}, 201, &created)
	srv.post(t, "/component-models", admin, map[string]any{"id":"bad","display_name":"Bad","make_id":"nope","model_number":"1"}, 422, nil) // unknown make
	srv.delete(t, "/component-makes/crestron", admin, 409)                              // make now in use
	srv.delete(t, "/component-models/acme-123a", admin, 204)
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./internal/api/ -run TestComponentModelsAPI -count=1`
Expected: FAIL (no route).

- [ ] **Step 3: Implement the API + permission**

Create `internal/api/component_models.go` copying `component_makes.go` (same `mapTypeErr`, per-route `a.require("model", <action>)`, `actorID(ctx)` audit on mutations). Map an unknown-make FK violation to 422. Register `model:*` in `roles.yaml` beside `make:*`. Wire `registerComponentModelRoutes` in `api.go`.

- [ ] **Step 4: Regenerate**

Run: `make gen` (expect `/component-models` in `internal/cli/api_gen.go`, `web/src/api/schema.gen.ts`, `api/openapi.*`, `docs/.../reference/cli/index.md`). Verify no unrelated drift.

- [ ] **Step 5: Run e2e, verify pass; confirm no gen drift**

Run: `go test ./internal/api/ -run TestComponentModelsAPI -count=1` (PASS), then `make gen && git diff --exit-code` (clean).

- [ ] **Step 6: Commit**

```bash
git add internal/api internal/cli internal/seed/roles.yaml api/ web/src/api docs/src/content/docs/reference/cli
git commit -m "feat: component_model API, model permission, generated client and CLI"
```

---

### Task 4: Models console page

**REQUIRED SUB-SKILL:** Use the `add-inventory-view` skill.

**Files:**
- Create: `web/src/lib/component_models.ts` (data layer: list/create/update/delete)
- Create: `web/src/pages/ComponentModels.tsx` (flat directory + Make column + make filter + detail blade with create/edit/delete + front/back image upload, official read-only)
- Modify: `web/src/lib/nav.ts` (add a **Models** entry under Catalog, beside Makes: `{ label: "Models", path: "/component-models", live: true, resource: "model", hint: "..." }`)
- Modify: `web/src/index.tsx` (route `/component-models` -> the page)
- Test: `web/src/pages/ComponentModels.test.tsx`

- [ ] **Step 1: Write the failing UI test**

Mirror `web/src/pages/ComponentMakes.test.tsx`, plus assert the Make column renders and the make filter narrows the list:

```tsx
test("lists models with make column and filters by make", async () => {
  renderWithProviders(() => <ComponentModels />, { seededModels: [
    { id: "acme-123a", display_name: "Acme 123A", make_id: "crestron", official: false },
    { id: "biamp-x", display_name: "Biamp X", make_id: "biamp", official: false },
  ], seededMakes: [{id:"crestron",display_name:"Crestron"},{id:"biamp",display_name:"Biamp"}]});
  expect(await screen.findByText("Acme 123A")).toBeInTheDocument();
  expect(screen.getByText("Crestron")).toBeInTheDocument();          // make column
  // filtering to crestron hides the biamp model (drive the facet the way tags/directory filtering does)
});
```

- [ ] **Step 2: Run it, verify it fails**

Run: `cd web && npx vitest run src/pages/ComponentModels.test.tsx`
Expected: FAIL (module not found).

- [ ] **Step 3: Build data layer + page + filter + image upload + nav + route**

Follow `add-inventory-view` and copy `ComponentMakes.tsx` for the flat entity; add a **Make** column (join the make display name from the makes list) and a **make filter** (mirror the directory's existing facet/filter mechanism, e.g. how tags or the inventory directory filters). For images, reuse the files upload flow (`web/src/pages/Files.tsx` / `web/src/lib/files.ts`): the create/edit form uploads front/back through the files API and sets `front_image_id`/`back_image_id`. Official rows render read-only. Add nav + route.

- [ ] **Step 4: Run the UI test, then the full suite**

Run: `cd web && npx vitest run src/pages/ComponentModels.test.tsx` (PASS), then `cd web && npx vitest run` (all green).

- [ ] **Step 5: Commit**

```bash
git add web/src
git commit -m "feat: component models catalog page with make column and image upload"
```

---

### Task 5: Docs + ship

**Files:**
- Create: `docs/src/content/docs/guides/admin/models.md` (operator guide, mirror `makes.md`)
- Modify: `docs/src/content/docs/architecture/api.md` (the `component-models` routes, mirror the `component-makes` block added in #255)
- Modify: `docs/src/content/docs/guides/cli.md` (a `## Component models` section, mirror `## Component makes`)
- Modify: `docs/src/content/docs/architecture/core-entities.md` (the model layer above the make), `status.mdx` (build note), `decisions.md` (no-type-this-slice, images-via-files, Models-primary IA, make-in-use-guard-lands-here)
- Modify: `docs/astro.config.mjs` (sidebar registration for models.md)
- Track onto the branch: this plan and the spec `docs/superpowers/specs/2026-07-14-component-model-registry-design.md`

- [ ] **Step 1: Write docs**, no em dashes, accurate (only the model registry shipped; roles/type still deferred to #256).
- [ ] **Step 2: Build docs** (`cd docs && npm run build`) to verify links + sidebar.
- [ ] **Step 3: Full gate** `make test` green; `make gen && git diff --exit-code` clean.
- [ ] **Step 4: Commit docs.**
- [ ] **Step 5: `/ship-slice`** with live screenshots (Models directory with make column, a model detail/create blade with image upload, an official read-only model, the make-filter applied). Its ship-review becomes the PR body.
- [ ] **Step 6: Open PR** closing #257, refs #254.

---

## Self-review notes

- **Spec coverage:** component_model entity (make FK, image FKs, lifecycle, official, no type), Models-primary IA (make column + filter), make-in-use guard, dev-seed, docs. Covered by Tasks 1-5.
- **Executor lookups (read the named precedent):** the storagetest PG helper name; the e2e server/token helper; the `file` PK column (from the #246 migration); the in-use sentinel name the type registry uses for 409; the directory facet/filter mechanism for the make filter; the files upload flow for image upload; the migration timestamp (unique + latest).
- **Security lens for ship:** authz on all 5 model routes + the viewer floor for `model:read`; if any model field is rendered as an `href`/`src` (image URLs are rendered as `<img src>` from file blobs, not user URLs, so lower risk than the make website field, but confirm no user-supplied string reaches an attribute sink).
