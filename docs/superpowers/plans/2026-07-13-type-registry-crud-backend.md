# Type registry CRUD (backend) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship an operator CRUD registry for the inventory classifiers (location/system/component types) behind one new `type` capability resource, so operators add custom types via the API/CLI instead of editing seed YAML.

**Architecture:** The three type tables (`location_type`, `system_type`, `component_type`) are flat, unscoped reference tables sharing one shape (`id`, `official`, `display_name`, `rank`, plus `icon` on location). A small shared storage primitive (`typeregistry.go`) carries the sentinels and the delete-guard (refuse official, refuse in-use); each registry gets thin create/update/delete gateway methods on top. The HTTP layer mirrors the shipped `list-location-types` route, adding create/update/delete gated by `type:create|update|delete`. Reads move to `type:read`, which every role already holds via the `viewer` `*:read` floor. `secret_type` is untouched (list-only). No migration: the tables already carry `official default false` and the parent FK.

**Tech Stack:** Go, Huma v2 (OpenAPI), pgx v5 + Postgres, dbmate (unused here), `testcontainers-go` via `internal/storage/storagetest`, seed YAML, `make gen` (openapigen + cligen + docsgen + web client).

## Global Constraints

- **Test-first.** Every behavior change lands as a test that fails before and passes after. RED before GREEN, no exceptions.
- **Integration tests use real Postgres** via `storagetest.NewDSN(t)` (ephemeral testcontainer, random port). Never mock the DB. Never bind a fixed host port.
- **`type` is capability-only and unscoped.** Routes call `a.require("type", <action>)` and never `a.scopeFor`. It is global reference data.
- **Official rows are read-only.** `official = true` rows reject update/delete (422). Operator-created rows are `official = false`.
- **Delete refused while in use** (409), with the parent FK as the backstop.
- **`make gen` drift must be committed.** A non-empty `git diff` after `make gen` fails the slice.
- **No em dashes** in any artifact; use commas, colons, periods, parentheses.
- **No AI/assistant attribution** in commits, code, or docs.
- **Head-noun-last naming** (`<qualifier>_<genus>`), matching the architecture glossary.
- **Never edit an applied migration.** (No migration is expected here; if one becomes necessary, add a new one.)
- **PR-only.** Work in a git worktree under `.claude/worktrees/`; never commit to `main`. Branch `feat/type-registry-crud` from `origin/main`.

---

### Task 1: Shared type-registry storage primitive + location_type CRUD

**Files:**
- Create: `internal/storage/typeregistry.go`
- Modify: `internal/storage/locations.go` (add `LocationTypePatch` + `CreateLocationType` / `UpdateLocationType` / `DeleteLocationType`)
- Modify: `internal/storage/storage.go:188` (extend the Gateway interface, after `ListLocationTypes`)
- Modify: `internal/storage/unimplemented.go` (add three stubs)
- Test: `internal/storage/location_types_test.go`

**Interfaces:**
- Produces (storage primitive, consumed by Tasks 3-4):
  - `var ErrTypeNotFound, ErrTypeExists, ErrTypeOfficial, ErrTypeInUse error`
  - `type typeRef struct { table string; col string }`
  - `func guardTypeMutable(ctx context.Context, q querier, table, id string) error`
  - `func countTypeRefs(ctx context.Context, q querier, ref typeRef, id string) (int, error)`
  - `func deleteTypeRow(ctx context.Context, p *PG, table, resource string, ref typeRef, actorID, id string) error`
  - `func isUniqueViolation(err error) bool`
- Produces (gateway methods, consumed by Task 2):
  - `CreateLocationType(ctx, actorID string, lt LocationType) (*LocationType, error)`
  - `UpdateLocationType(ctx, actorID, id string, patch LocationTypePatch) (*LocationType, error)`
  - `DeleteLocationType(ctx, actorID, id string) error`
  - `type LocationTypePatch struct { DisplayName *string; Rank *int; Icon *string }`
- Consumes: `querier` ([locations.go:252](../../../internal/storage/locations.go#L252)), `writeAuditRes` ([locations.go:259](../../../internal/storage/locations.go#L259)), `LocationType` ([locations.go:61](../../../internal/storage/locations.go#L61)), `storagetest.NewDSN`.

- [ ] **Step 1: Write the failing integration test**

Create `internal/storage/location_types_test.go`:

```go
package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func TestLocationTypeCRUD(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Create a custom type; it is official=false and appears in the list.
	lt, err := gw.CreateLocationType(ctx, "", storage.LocationType{ID: "wing", DisplayName: "Wing", Rank: 15, Icon: "layers"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if lt.Official {
		t.Fatalf("new type official=true, want false")
	}

	// Duplicate id is ErrTypeExists.
	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{ID: "wing", DisplayName: "Dup"}); !errors.Is(err, storage.ErrTypeExists) {
		t.Fatalf("dup create err = %v, want ErrTypeExists", err)
	}

	// Update mutates display_name; rank/icon unchanged when omitted.
	name := "West Wing"
	if _, err := gw.UpdateLocationType(ctx, "", "wing", storage.LocationTypePatch{DisplayName: &name}); err != nil {
		t.Fatalf("update: %v", err)
	}

	// Official rows are read-only.
	if _, err := gw.UpdateLocationType(ctx, "", "campus", storage.LocationTypePatch{DisplayName: &name}); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("update official err = %v, want ErrTypeOfficial", err)
	}
	if err := gw.DeleteLocationType(ctx, "", "campus"); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("delete official err = %v, want ErrTypeOfficial", err)
	}

	// In-use delete is refused: place a location of type wing, then delete.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "w1", LocationType: "wing"}, allScope()); err != nil {
		t.Fatalf("create location: %v", err)
	}
	if err := gw.DeleteLocationType(ctx, "", "wing"); !errors.Is(err, storage.ErrTypeInUse) {
		t.Fatalf("delete in-use err = %v, want ErrTypeInUse", err)
	}

	// Unknown id is ErrTypeNotFound.
	if err := gw.DeleteLocationType(ctx, "", "nope"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("delete unknown err = %v, want ErrTypeNotFound", err)
	}
}
```

Note: `allScope()` is an all-scope `scope.Set` test helper. Check `internal/storage/*_test.go` for an existing one (e.g. a helper returning `scope.Set{All: true}`); reuse it. If none exists, add at the bottom of this file:

```go
func allScope() scope.Set { return scope.Set{All: true} }
```

with `import "github.com/hyperscaleav/omniglass/internal/scope"`.

- [ ] **Step 2: Run the test, verify it fails to compile**

Run: `go test ./internal/storage/ -run TestLocationTypeCRUD`
Expected: FAIL, build error `gw.CreateLocationType undefined` (and `ErrTypeExists` etc. undefined).

- [ ] **Step 3: Create the shared primitive**

Create `internal/storage/typeregistry.go`:

```go
package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// The type registries (location_type, system_type, component_type) are flat,
// unscoped reference tables sharing one shape: a stable id, an official flag
// (seed-owned rows), a display_name, and a rank (plus an icon on location). They
// are not scoped-tree entities, so they use these registry helpers rather than
// scopedcrud. Operator rows are official=false; seeded rows are official=true and
// read-only. A row cannot be deleted while inventory still references it (the
// parent FK also enforces this; the pre-count turns the raw FK error into a clean
// ErrTypeInUse).
var (
	ErrTypeNotFound = errors.New("storage: type not found")
	ErrTypeExists   = errors.New("storage: type id already exists")
	ErrTypeOfficial = errors.New("storage: official type is read-only")
	ErrTypeInUse    = errors.New("storage: type is referenced by existing rows")
)

// typeRef names the parent table and column that reference a type id, for the
// delete-in-use guard (e.g. {"location", "location_type"}).
type typeRef struct {
	table string
	col   string
}

// isUniqueViolation reports whether err is a Postgres unique_violation (23505),
// used to turn a duplicate type id into ErrTypeExists.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// guardTypeMutable loads a type row's official flag by id: ErrTypeNotFound if
// absent, ErrTypeOfficial if seed-owned. Update and delete call it first.
func guardTypeMutable(ctx context.Context, q querier, table, id string) error {
	var official bool
	err := q.QueryRow(ctx, `select official from `+table+` where id = $1`, id).Scan(&official)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTypeNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: load %s %q: %w", table, id, err)
	}
	if official {
		return ErrTypeOfficial
	}
	return nil
}

// countTypeRefs counts inventory rows referencing a type id, for the
// delete-in-use guard.
func countTypeRefs(ctx context.Context, q querier, ref typeRef, id string) (int, error) {
	var n int
	if err := q.QueryRow(ctx, `select count(*) from `+ref.table+` where `+ref.col+` = $1`, id).Scan(&n); err != nil {
		return 0, fmt.Errorf("storage: count %s refs: %w", ref.table, err)
	}
	return n, nil
}

// deleteTypeRow removes a custom type row by id in one transaction: refuses an
// official row (ErrTypeOfficial), refuses a row still referenced by inventory
// (ErrTypeInUse), deletes, and audits. resource is the audit label
// (e.g. "location_type").
func deleteTypeRow(ctx context.Context, p *PG, table, resource string, ref typeRef, actorID, id string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete %s: %w", table, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, table, id); err != nil {
		return err
	}
	n, err := countTypeRefs(ctx, tx, ref, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrTypeInUse
	}
	if _, err := tx.Exec(ctx, `delete from `+table+` where id = $1`, id); err != nil {
		return fmt.Errorf("storage: delete %s %q: %w", table, id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", resource, id, map[string]string{"id": id}, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete %s: %w", table, err)
	}
	return nil
}
```

Note: `guardTypeMutable` references `pgx.ErrNoRows`; add `"github.com/jackc/pgx/v5"` to the import block.

- [ ] **Step 4: Add the location_type gateway methods**

In `internal/storage/locations.go`, directly after `ListLocationTypes` (ends [locations.go:105](../../../internal/storage/locations.go#L105)), add:

```go
// LocationTypePatch carries the mutable fields of a location_type update; a nil
// field is left unchanged.
type LocationTypePatch struct {
	DisplayName *string
	Rank        *int
	Icon        *string
}

// CreateLocationType inserts a custom (official=false) location_type and audits
// it. A duplicate id (including a seed-owned official id) is ErrTypeExists.
func (p *PG) CreateLocationType(ctx context.Context, actorID string, lt LocationType) (*LocationType, error) {
	lt.Official = false
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create location_type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`insert into location_type (id, official, display_name, rank, icon) values ($1, false, $2, $3, $4)`,
		lt.ID, lt.DisplayName, lt.Rank, lt.Icon); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrTypeExists
		}
		return nil, fmt.Errorf("storage: insert location_type %q: %w", lt.ID, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "location_type", lt.ID, nil, lt); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create location_type: %w", err)
	}
	return &lt, nil
}

// UpdateLocationType patches a custom location_type's display_name, rank, or icon
// (nil fields unchanged) and audits it. Official rows are read-only (ErrTypeOfficial);
// an unknown id is ErrTypeNotFound.
func (p *PG) UpdateLocationType(ctx context.Context, actorID, id string, patch LocationTypePatch) (*LocationType, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update location_type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "location_type", id); err != nil {
		return nil, err
	}
	var lt LocationType
	if err := tx.QueryRow(ctx, `
		update location_type set
			display_name = coalesce($2, display_name),
			rank         = coalesce($3, rank),
			icon         = coalesce($4, icon)
		where id = $1
		returning id, official, display_name, rank, icon`,
		id, patch.DisplayName, patch.Rank, patch.Icon).
		Scan(&lt.ID, &lt.Official, &lt.DisplayName, &lt.Rank, &lt.Icon); err != nil {
		return nil, fmt.Errorf("storage: update location_type %q: %w", id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "location_type", id, nil, lt); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update location_type: %w", err)
	}
	return &lt, nil
}

// DeleteLocationType removes a custom location_type, refusing an official row and
// a row still referenced by a location.
func (p *PG) DeleteLocationType(ctx context.Context, actorID, id string) error {
	return deleteTypeRow(ctx, p, "location_type", "location_type", typeRef{table: "location", col: "location_type"}, actorID, id)
}
```

- [ ] **Step 5: Extend the Gateway interface + the unimplemented stub**

In `internal/storage/storage.go`, after `ListLocationTypes(ctx context.Context) ([]LocationType, error)` ([storage.go:188](../../../internal/storage/storage.go#L188)), add:

```go
	// The location_type registry CRUD (capability-only, unscoped). Create writes a
	// custom (official=false) row; update/delete refuse official rows and delete
	// refuses a row still referenced by a location.
	CreateLocationType(ctx context.Context, actorID string, lt LocationType) (*LocationType, error)
	UpdateLocationType(ctx context.Context, actorID, id string, patch LocationTypePatch) (*LocationType, error)
	DeleteLocationType(ctx context.Context, actorID, id string) error
```

In `internal/storage/unimplemented.go`, near the other `LocationType` stubs, add:

```go
func (UnimplementedGateway) CreateLocationType(context.Context, string, LocationType) (*LocationType, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateLocationType(context.Context, string, string, LocationTypePatch) (*LocationType, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteLocationType(context.Context, string, string) error { return nil }
```

- [ ] **Step 6: Run the test, verify it passes**

Run: `go test ./internal/storage/ -run TestLocationTypeCRUD`
Expected: PASS.

- [ ] **Step 7: Run the storage package tests to confirm no regression**

Run: `go test ./internal/storage/`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/storage/typeregistry.go internal/storage/locations.go internal/storage/storage.go internal/storage/unimplemented.go internal/storage/location_types_test.go
git commit -m "feat: location_type registry CRUD in the storage gateway"
```

---

### Task 2: location_type CRUD API + `type` authz grant + read-gate rework

**Files:**
- Create: `internal/api/types.go` (shared `mapTypeErr`)
- Modify: `internal/api/locations.go` (add create/update/delete routes; move list gate to `type:read`)
- Modify: `internal/seed/roles.yaml` (grant `type:create,update,delete` to admin)
- Test: `internal/api/location_types_e2e_test.go` (add `TestLocationTypeCRUDAPI`)

**Interfaces:**
- Produces (consumed by Tasks 3-4): `func mapTypeErr(err error, kind string) error`
- Consumes: `a.require` ([auth.go:232](../../../internal/api/auth.go#L232)), `a.authn`, `actorID` ([locations.go:215](../../../internal/api/locations.go#L215)), `locationTypeBody` ([locations.go:41](../../../internal/api/locations.go#L41)), the Task 1 gateway methods, `apiClient.do` ([locations_e2e_test.go:150](../../../internal/api/locations_e2e_test.go#L150)), `bootstrapOwnerTok` ([authz_matrix_test.go:195](../../../internal/api/authz_matrix_test.go#L195)).

- [ ] **Step 1: Write the failing e2e test**

Append to `internal/api/location_types_e2e_test.go`:

```go
func TestLocationTypeCRUDAPI(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ownerTok := bootstrapOwnerTok(t, ctx, gw)

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Create a custom type (201), then it appears in the list.
	c.do(ownerTok, http.MethodPost, "/location-types",
		map[string]any{"id": "wing", "display_name": "Wing", "rank": 15, "icon": "layers"}, http.StatusCreated)

	// Update it (200).
	c.do(ownerTok, http.MethodPatch, "/location-types/wing",
		map[string]any{"display_name": "West Wing"}, http.StatusOK)

	// Official rows are read-only (422 on update and delete).
	c.do(ownerTok, http.MethodPatch, "/location-types/campus",
		map[string]any{"display_name": "X"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodDelete, "/location-types/campus", nil, http.StatusUnprocessableEntity)

	// In use: place a location of type wing, delete is refused (409).
	c.do(ownerTok, http.MethodPost, "/locations",
		map[string]any{"name": "w1", "location_type": "wing"}, http.StatusCreated)
	c.do(ownerTok, http.MethodDelete, "/location-types/wing", nil, http.StatusConflict)

	// Remove the location, then the type deletes (204).
	c.do(ownerTok, http.MethodDelete, "/locations/w1", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodDelete, "/location-types/wing", nil, http.StatusNoContent)
}
```

(The 403-without-`type` case is covered generically by `TestEveryRouteIsGated`, [authz_guard_test.go:46](../../../internal/api/authz_guard_test.go#L46), which auto-discovers the new routes.)

- [ ] **Step 2: Run the test, verify it fails**

Run: `go test ./internal/api/ -run TestLocationTypeCRUDAPI`
Expected: FAIL. The POST returns 404 (route not registered), so `do` fatals on the status mismatch.

- [ ] **Step 3: Add the shared error mapper**

Create `internal/api/types.go`:

```go
package api

import (
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// mapTypeErr translates the shared type-registry storage sentinels into HTTP
// status. kind is the wire label used in the message (e.g. "location_type").
// Shared by the location/system/component type routes.
func mapTypeErr(err error, kind string) error {
	switch {
	case errors.Is(err, storage.ErrTypeNotFound):
		return huma.Error404NotFound(kind + " not found")
	case errors.Is(err, storage.ErrTypeExists):
		return huma.Error409Conflict(kind + " id already exists")
	case errors.Is(err, storage.ErrTypeOfficial):
		return huma.Error422UnprocessableEntity("official " + kind + " is read-only")
	case errors.Is(err, storage.ErrTypeInUse):
		return huma.Error409Conflict(kind + " is referenced by existing rows")
	default:
		return huma.Error500InternalServerError("type operation failed")
	}
}
```

- [ ] **Step 4: Add the location_type write routes + move the list gate**

In `internal/api/locations.go`, change the `list-location-types` middleware from `a.require("location", "read")` to `a.require("type", "read")` ([locations.go:126](../../../internal/api/locations.go#L126)) and update its `Description` to end `Gated by type:read.`

Add these input/output types near `listLocationTypesOutput` ([locations.go:49](../../../internal/api/locations.go#L49)):

```go
type locationTypePathInput struct {
	ID string `path:"id" doc:"The location_type id"`
}

type createLocationTypeInput struct {
	Body struct {
		ID          string `json:"id" minLength:"1" doc:"Globally unique type id (kebab, e.g. wing)"`
		DisplayName string `json:"display_name" minLength:"1"`
		Rank        int    `json:"rank,omitempty" doc:"Ordering rank; lower sorts first"`
		Icon        string `json:"icon,omitempty" doc:"A glyph key; the console falls back to map-pin when empty"`
	}
}

type updateLocationTypeInput struct {
	ID   string `path:"id"`
	Body struct {
		DisplayName *string `json:"display_name,omitempty"`
		Rank        *int    `json:"rank,omitempty"`
		Icon        *string `json:"icon,omitempty"`
	}
}

type locationTypeOutput struct {
	Body locationTypeBody
}
```

Inside `registerLocationRoutes`, directly after the `list-location-types` registration ([locations.go:140](../../../internal/api/locations.go#L140)), add:

```go
	huma.Register(api, huma.Operation{
		OperationID:   "create-location-type",
		Method:        http.MethodPost,
		Path:          "/location-types",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a location type",
		Description:   "Creates a custom (non-official) location_type. Gated by type:create.",
		Middlewares:   huma.Middlewares{a.authn, a.require("type", "create")},
	}, func(ctx context.Context, in *createLocationTypeInput) (*locationTypeOutput, error) {
		lt, err := gw.CreateLocationType(ctx, actorID(ctx), storage.LocationType{
			ID: in.Body.ID, DisplayName: in.Body.DisplayName, Rank: in.Body.Rank, Icon: in.Body.Icon,
		})
		if err != nil {
			return nil, mapTypeErr(err, "location_type")
		}
		return &locationTypeOutput{Body: locationTypeBody{ID: lt.ID, DisplayName: lt.DisplayName, Rank: lt.Rank, Icon: lt.Icon, Official: lt.Official}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-location-type",
		Method:      http.MethodPatch,
		Path:        "/location-types/{id}",
		Summary:     "Update a location type",
		Description: "Patches a custom location_type's display_name, rank, or icon. Official types are read-only (422). Gated by type:update.",
		Middlewares: huma.Middlewares{a.authn, a.require("type", "update")},
	}, func(ctx context.Context, in *updateLocationTypeInput) (*locationTypeOutput, error) {
		lt, err := gw.UpdateLocationType(ctx, actorID(ctx), in.ID, storage.LocationTypePatch{
			DisplayName: in.Body.DisplayName, Rank: in.Body.Rank, Icon: in.Body.Icon,
		})
		if err != nil {
			return nil, mapTypeErr(err, "location_type")
		}
		return &locationTypeOutput{Body: locationTypeBody{ID: lt.ID, DisplayName: lt.DisplayName, Rank: lt.Rank, Icon: lt.Icon, Official: lt.Official}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-location-type",
		Method:        http.MethodDelete,
		Path:          "/location-types/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a location type",
		Description:   "Deletes a custom location_type, refused if official (422) or still referenced by a location (409). Gated by type:delete.",
		Middlewares:   huma.Middlewares{a.authn, a.require("type", "delete")},
	}, func(ctx context.Context, in *locationTypePathInput) (*struct{}, error) {
		if err := gw.DeleteLocationType(ctx, actorID(ctx), in.ID); err != nil {
			return nil, mapTypeErr(err, "location_type")
		}
		return nil, nil
	})
```

- [ ] **Step 5: Grant `type` writes to admin**

In `internal/seed/roles.yaml`, in the `admin` role's `permissions` list (near `datapoint_type:create`, [roles.yaml:72](../../../internal/seed/roles.yaml#L72)), add:

```yaml
      - "type:create,update,delete"
```

(Reads need no grant: `viewer`'s `*:read` floor already covers `type:read`.)

- [ ] **Step 6: Run the test, verify it passes**

Run: `go test ./internal/api/ -run TestLocationTypeCRUDAPI`
Expected: PASS.

- [ ] **Step 7: Run the API gating conformance + the read test**

Run: `go test ./internal/api/ -run 'TestEveryRouteIsGated|TestLocationTypesAPI'`
Expected: PASS (the new routes are auto-discovered and gated; the existing read test still passes with the `type:read` gate because owner holds `>`).

- [ ] **Step 8: Commit**

```bash
git add internal/api/types.go internal/api/locations.go internal/api/location_types_e2e_test.go internal/seed/roles.yaml
git commit -m "feat: location_type CRUD API gated by the type resource"
```

---

### Task 3: system_type CRUD (storage + API)

**Files:**
- Modify: `internal/storage/systems.go` (add `SystemTypePatch` + create/update/delete)
- Modify: `internal/storage/storage.go:208` (extend the interface after `ListSystemTypes`)
- Modify: `internal/storage/unimplemented.go` (three stubs)
- Modify: `internal/api/systems.go` (add `list-system-types` + create/update/delete)
- Test: `internal/storage/system_types_test.go`, `internal/api/system_types_e2e_test.go`

**Interfaces:**
- Produces: `CreateSystemType` / `UpdateSystemType` / `DeleteSystemType`; `type SystemTypePatch struct { DisplayName *string; Rank *int }`; wire `systemTypeBody`; route `list-system-types` at `GET /system-types`.
- Consumes: the Task 1 primitive (`deleteTypeRow`, `guardTypeMutable`, `isUniqueViolation`, sentinels), `mapTypeErr` (Task 2), `SystemType` ([systems.go:27](../../../internal/storage/systems.go#L27)).

- [ ] **Step 1: Write the failing storage test**

Create `internal/storage/system_types_test.go`:

```go
package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func TestSystemTypeCRUD(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	st, err := gw.CreateSystemType(ctx, "", storage.SystemType{ID: "kiosk", DisplayName: "Kiosk", Rank: 15})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if st.Official {
		t.Fatalf("new type official=true, want false")
	}
	if _, err := gw.CreateSystemType(ctx, "", storage.SystemType{ID: "kiosk", DisplayName: "Dup"}); !errors.Is(err, storage.ErrTypeExists) {
		t.Fatalf("dup create err = %v, want ErrTypeExists", err)
	}
	name := "Info Kiosk"
	if _, err := gw.UpdateSystemType(ctx, "", "kiosk", storage.SystemTypePatch{DisplayName: &name}); err != nil {
		t.Fatalf("update: %v", err)
	}
	// Place a system of type kiosk, delete refused (in use).
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "k1", SystemType: "kiosk"}, allScope()); err != nil {
		t.Fatalf("create system: %v", err)
	}
	if err := gw.DeleteSystemType(ctx, "", "kiosk"); !errors.Is(err, storage.ErrTypeInUse) {
		t.Fatalf("delete in-use err = %v, want ErrTypeInUse", err)
	}
}
```

Note: confirm the seeded system_type set does not already include `kiosk`; if it does, pick an unseeded id. Check `internal/seed/system_types.yaml`.

- [ ] **Step 2: Run the test, verify it fails to compile**

Run: `go test ./internal/storage/ -run TestSystemTypeCRUD`
Expected: FAIL, `gw.CreateSystemType undefined`.

- [ ] **Step 3: Add the system_type gateway methods**

In `internal/storage/systems.go`, after `ListSystemTypes` ([systems.go:96](../../../internal/storage/systems.go#L96)), add:

```go
// SystemTypePatch carries the mutable fields of a system_type update; a nil field
// is left unchanged.
type SystemTypePatch struct {
	DisplayName *string
	Rank        *int
}

// CreateSystemType inserts a custom (official=false) system_type and audits it. A
// duplicate id is ErrTypeExists.
func (p *PG) CreateSystemType(ctx context.Context, actorID string, st SystemType) (*SystemType, error) {
	st.Official = false
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create system_type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`insert into system_type (id, official, display_name, rank) values ($1, false, $2, $3)`,
		st.ID, st.DisplayName, st.Rank); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrTypeExists
		}
		return nil, fmt.Errorf("storage: insert system_type %q: %w", st.ID, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "system_type", st.ID, nil, st); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create system_type: %w", err)
	}
	return &st, nil
}

// UpdateSystemType patches a custom system_type (nil fields unchanged) and audits
// it. Official rows are read-only; an unknown id is ErrTypeNotFound.
func (p *PG) UpdateSystemType(ctx context.Context, actorID, id string, patch SystemTypePatch) (*SystemType, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update system_type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "system_type", id); err != nil {
		return nil, err
	}
	var st SystemType
	if err := tx.QueryRow(ctx, `
		update system_type set
			display_name = coalesce($2, display_name),
			rank         = coalesce($3, rank)
		where id = $1
		returning id, official, display_name, rank`,
		id, patch.DisplayName, patch.Rank).
		Scan(&st.ID, &st.Official, &st.DisplayName, &st.Rank); err != nil {
		return nil, fmt.Errorf("storage: update system_type %q: %w", id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "system_type", id, nil, st); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update system_type: %w", err)
	}
	return &st, nil
}

// DeleteSystemType removes a custom system_type, refusing an official row and a
// row still referenced by a system.
func (p *PG) DeleteSystemType(ctx context.Context, actorID, id string) error {
	return deleteTypeRow(ctx, p, "system_type", "system_type", typeRef{table: "system", col: "system_type"}, actorID, id)
}
```

Then in `internal/storage/storage.go`, after `ListSystemTypes` ([storage.go:208](../../../internal/storage/storage.go#L208)), add:

```go
	CreateSystemType(ctx context.Context, actorID string, st SystemType) (*SystemType, error)
	UpdateSystemType(ctx context.Context, actorID, id string, patch SystemTypePatch) (*SystemType, error)
	DeleteSystemType(ctx context.Context, actorID, id string) error
```

And in `internal/storage/unimplemented.go`, near the `SystemType` stubs:

```go
func (UnimplementedGateway) CreateSystemType(context.Context, string, SystemType) (*SystemType, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateSystemType(context.Context, string, string, SystemTypePatch) (*SystemType, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteSystemType(context.Context, string, string) error { return nil }
```

- [ ] **Step 4: Run the storage test, verify it passes**

Run: `go test ./internal/storage/ -run TestSystemTypeCRUD`
Expected: PASS.

- [ ] **Step 5: Write the failing e2e test**

Create `internal/api/system_types_e2e_test.go`:

```go
package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func TestSystemTypesAPI(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ownerTok := bootstrapOwnerTok(t, ctx, gw)

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// The seeded official types list under type:read.
	out := c.do(ownerTok, http.MethodGet, "/system-types", nil, http.StatusOK)
	var body struct {
		SystemTypes []struct {
			ID       string `json:"id"`
			Official bool   `json:"official"`
		} `json:"system_types"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.SystemTypes) == 0 {
		t.Fatalf("system_types empty, want seeded rows")
	}

	// CRUD a custom type; in-use delete refused.
	c.do(ownerTok, http.MethodPost, "/system-types",
		map[string]any{"id": "kiosk", "display_name": "Kiosk", "rank": 15}, http.StatusCreated)
	c.do(ownerTok, http.MethodPatch, "/system-types/kiosk",
		map[string]any{"display_name": "Info Kiosk"}, http.StatusOK)
	c.do(ownerTok, http.MethodPost, "/systems",
		map[string]any{"name": "k1", "system_type": "kiosk"}, http.StatusCreated)
	c.do(ownerTok, http.MethodDelete, "/system-types/kiosk", nil, http.StatusConflict)
	c.do(ownerTok, http.MethodDelete, "/systems/k1", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodDelete, "/system-types/kiosk", nil, http.StatusNoContent)
}
```

- [ ] **Step 6: Run the e2e test, verify it fails**

Run: `go test ./internal/api/ -run TestSystemTypesAPI`
Expected: FAIL (GET `/system-types` is 404, route not registered).

- [ ] **Step 7: Add the system_type routes**

In `internal/api/systems.go`, add the wire type and output near the other system types, and inside `registerSystemRoutes` add a `list-system-types` route plus create/update/delete, mirroring the location_type routes. First the types (place after the existing `listSystemsOutput` block):

```go
// systemTypeBody is the wire shape of a system_type registry row.
type systemTypeBody struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Rank        int    `json:"rank"`
	Official    bool   `json:"official"`
}

type listSystemTypesOutput struct {
	Body struct {
		SystemTypes []systemTypeBody `json:"system_types"`
	}
}

type systemTypePathInput struct {
	ID string `path:"id" doc:"The system_type id"`
}

type createSystemTypeInput struct {
	Body struct {
		ID          string `json:"id" minLength:"1" doc:"Globally unique type id"`
		DisplayName string `json:"display_name" minLength:"1"`
		Rank        int    `json:"rank,omitempty"`
	}
}

type updateSystemTypeInput struct {
	ID   string `path:"id"`
	Body struct {
		DisplayName *string `json:"display_name,omitempty"`
		Rank        *int    `json:"rank,omitempty"`
	}
}

type systemTypeOutput struct {
	Body systemTypeBody
}
```

Then inside `registerSystemRoutes` (add near the top of the function body, alongside the other registrations):

```go
	huma.Register(api, huma.Operation{
		OperationID: "list-system-types",
		Method:      http.MethodGet,
		Path:        "/system-types",
		Summary:     "List system types",
		Description: "Lists the system_type registry, ordered by rank. Populates the type picker on the system form. Gated by type:read.",
		Middlewares: huma.Middlewares{a.authn, a.require("type", "read")},
	}, func(ctx context.Context, _ *struct{}) (*listSystemTypesOutput, error) {
		types, err := gw.ListSystemTypes(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("list system types")
		}
		out := &listSystemTypesOutput{}
		out.Body.SystemTypes = make([]systemTypeBody, 0, len(types))
		for i := range types {
			out.Body.SystemTypes = append(out.Body.SystemTypes, systemTypeBody{
				ID: types[i].ID, DisplayName: types[i].DisplayName, Rank: types[i].Rank, Official: types[i].Official,
			})
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-system-type",
		Method:        http.MethodPost,
		Path:          "/system-types",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a system type",
		Description:   "Creates a custom (non-official) system_type. Gated by type:create.",
		Middlewares:   huma.Middlewares{a.authn, a.require("type", "create")},
	}, func(ctx context.Context, in *createSystemTypeInput) (*systemTypeOutput, error) {
		st, err := gw.CreateSystemType(ctx, actorID(ctx), storage.SystemType{
			ID: in.Body.ID, DisplayName: in.Body.DisplayName, Rank: in.Body.Rank,
		})
		if err != nil {
			return nil, mapTypeErr(err, "system_type")
		}
		return &systemTypeOutput{Body: systemTypeBody{ID: st.ID, DisplayName: st.DisplayName, Rank: st.Rank, Official: st.Official}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-system-type",
		Method:      http.MethodPatch,
		Path:        "/system-types/{id}",
		Summary:     "Update a system type",
		Description: "Patches a custom system_type's display_name or rank. Official types are read-only (422). Gated by type:update.",
		Middlewares: huma.Middlewares{a.authn, a.require("type", "update")},
	}, func(ctx context.Context, in *updateSystemTypeInput) (*systemTypeOutput, error) {
		st, err := gw.UpdateSystemType(ctx, actorID(ctx), in.ID, storage.SystemTypePatch{
			DisplayName: in.Body.DisplayName, Rank: in.Body.Rank,
		})
		if err != nil {
			return nil, mapTypeErr(err, "system_type")
		}
		return &systemTypeOutput{Body: systemTypeBody{ID: st.ID, DisplayName: st.DisplayName, Rank: st.Rank, Official: st.Official}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-system-type",
		Method:        http.MethodDelete,
		Path:          "/system-types/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a system type",
		Description:   "Deletes a custom system_type, refused if official (422) or referenced by a system (409). Gated by type:delete.",
		Middlewares:   huma.Middlewares{a.authn, a.require("type", "delete")},
	}, func(ctx context.Context, in *systemTypePathInput) (*struct{}, error) {
		if err := gw.DeleteSystemType(ctx, actorID(ctx), in.ID); err != nil {
			return nil, mapTypeErr(err, "system_type")
		}
		return nil, nil
	})
```

- [ ] **Step 8: Run the e2e test, verify it passes**

Run: `go test ./internal/api/ -run TestSystemTypesAPI`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/storage/systems.go internal/storage/storage.go internal/storage/unimplemented.go internal/storage/system_types_test.go internal/api/systems.go internal/api/system_types_e2e_test.go
git commit -m "feat: system_type registry CRUD (storage + API)"
```

---

### Task 4: component_type CRUD (storage + API)

**Files:**
- Modify: `internal/storage/components.go` (add `ComponentTypePatch` + create/update/delete)
- Modify: `internal/storage/storage.go:217` (extend the interface after `ListComponentTypes`)
- Modify: `internal/storage/unimplemented.go` (three stubs)
- Modify: `internal/api/components.go` (add `list-component-types` + create/update/delete)
- Test: `internal/storage/component_types_test.go`, `internal/api/component_types_e2e_test.go`

**Interfaces:**
- Produces: `CreateComponentType` / `UpdateComponentType` / `DeleteComponentType`; `type ComponentTypePatch struct { DisplayName *string; Rank *int }`; wire `componentTypeBody`; route `list-component-types` at `GET /component-types`.
- Consumes: the Task 1 primitive, `mapTypeErr` (Task 2), `ComponentType` ([components.go:25](../../../internal/storage/components.go#L25)).

- [ ] **Step 1: Write the failing storage test**

Create `internal/storage/component_types_test.go`:

```go
package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func TestComponentTypeCRUD(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ct, err := gw.CreateComponentType(ctx, "", storage.ComponentType{ID: "sensor", DisplayName: "Sensor", Rank: 15})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if ct.Official {
		t.Fatalf("new type official=true, want false")
	}
	if _, err := gw.CreateComponentType(ctx, "", storage.ComponentType{ID: "sensor", DisplayName: "Dup"}); !errors.Is(err, storage.ErrTypeExists) {
		t.Fatalf("dup create err = %v, want ErrTypeExists", err)
	}
	name := "Motion Sensor"
	if _, err := gw.UpdateComponentType(ctx, "", "sensor", storage.ComponentTypePatch{DisplayName: &name}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := gw.UpdateComponentType(ctx, "", "sensor", storage.ComponentTypePatch{DisplayName: &name}); err != nil {
		t.Fatalf("update again: %v", err)
	}
	if err := gw.DeleteComponentType(ctx, "", "sensor"); err != nil {
		t.Fatalf("delete unused: %v", err)
	}
	if err := gw.DeleteComponentType(ctx, "", "sensor"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("re-delete err = %v, want ErrTypeNotFound", err)
	}
}
```

Note: confirm `sensor` is not already a seeded component_type ([internal/seed/component_types.yaml](../../../internal/seed/component_types.yaml)); pick an unseeded id if it is.

- [ ] **Step 2: Run the test, verify it fails to compile**

Run: `go test ./internal/storage/ -run TestComponentTypeCRUD`
Expected: FAIL, `gw.CreateComponentType undefined`.

- [ ] **Step 3: Add the component_type gateway methods**

In `internal/storage/components.go`, after `ListComponentTypes` ([components.go:80](../../../internal/storage/components.go#L80) and its body), add (mirrors system_type, table `component_type`, ref `component.component_type`):

```go
// ComponentTypePatch carries the mutable fields of a component_type update; a nil
// field is left unchanged.
type ComponentTypePatch struct {
	DisplayName *string
	Rank        *int
}

// CreateComponentType inserts a custom (official=false) component_type and audits
// it. A duplicate id is ErrTypeExists.
func (p *PG) CreateComponentType(ctx context.Context, actorID string, ct ComponentType) (*ComponentType, error) {
	ct.Official = false
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create component_type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`insert into component_type (id, official, display_name, rank) values ($1, false, $2, $3)`,
		ct.ID, ct.DisplayName, ct.Rank); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrTypeExists
		}
		return nil, fmt.Errorf("storage: insert component_type %q: %w", ct.ID, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "component_type", ct.ID, nil, ct); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create component_type: %w", err)
	}
	return &ct, nil
}

// UpdateComponentType patches a custom component_type (nil fields unchanged) and
// audits it. Official rows are read-only; an unknown id is ErrTypeNotFound.
func (p *PG) UpdateComponentType(ctx context.Context, actorID, id string, patch ComponentTypePatch) (*ComponentType, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update component_type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "component_type", id); err != nil {
		return nil, err
	}
	var ct ComponentType
	if err := tx.QueryRow(ctx, `
		update component_type set
			display_name = coalesce($2, display_name),
			rank         = coalesce($3, rank)
		where id = $1
		returning id, official, display_name, rank`,
		id, patch.DisplayName, patch.Rank).
		Scan(&ct.ID, &ct.Official, &ct.DisplayName, &ct.Rank); err != nil {
		return nil, fmt.Errorf("storage: update component_type %q: %w", id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "component_type", id, nil, ct); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update component_type: %w", err)
	}
	return &ct, nil
}

// DeleteComponentType removes a custom component_type, refusing an official row
// and a row still referenced by a component.
func (p *PG) DeleteComponentType(ctx context.Context, actorID, id string) error {
	return deleteTypeRow(ctx, p, "component_type", "component_type", typeRef{table: "component", col: "component_type"}, actorID, id)
}
```

Then in `internal/storage/storage.go`, after `ListComponentTypes` ([storage.go:217](../../../internal/storage/storage.go#L217)):

```go
	CreateComponentType(ctx context.Context, actorID string, ct ComponentType) (*ComponentType, error)
	UpdateComponentType(ctx context.Context, actorID, id string, patch ComponentTypePatch) (*ComponentType, error)
	DeleteComponentType(ctx context.Context, actorID, id string) error
```

And in `internal/storage/unimplemented.go`, near the `ComponentType` stubs:

```go
func (UnimplementedGateway) CreateComponentType(context.Context, string, ComponentType) (*ComponentType, error) {
	return nil, nil
}
func (UnimplementedGateway) UpdateComponentType(context.Context, string, string, ComponentTypePatch) (*ComponentType, error) {
	return nil, nil
}
func (UnimplementedGateway) DeleteComponentType(context.Context, string, string) error { return nil }
```

- [ ] **Step 4: Run the storage test, verify it passes**

Run: `go test ./internal/storage/ -run TestComponentTypeCRUD`
Expected: PASS.

- [ ] **Step 5: Write the failing e2e test**

Create `internal/api/component_types_e2e_test.go`:

```go
package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func TestComponentTypesAPI(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ownerTok := bootstrapOwnerTok(t, ctx, gw)

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	c.do(ownerTok, http.MethodGet, "/component-types", nil, http.StatusOK)
	c.do(ownerTok, http.MethodPost, "/component-types",
		map[string]any{"id": "sensor", "display_name": "Sensor", "rank": 15}, http.StatusCreated)
	c.do(ownerTok, http.MethodPatch, "/component-types/sensor",
		map[string]any{"display_name": "Motion Sensor"}, http.StatusOK)
	// Official rows are read-only (pick a seeded id, e.g. display).
	c.do(ownerTok, http.MethodDelete, "/component-types/display", nil, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodDelete, "/component-types/sensor", nil, http.StatusNoContent)
}
```

Note: confirm `display` is a seeded official component_type ([component_types.yaml](../../../internal/seed/component_types.yaml) shows `display` first); adjust if the seed changes.

- [ ] **Step 6: Run the e2e test, verify it fails**

Run: `go test ./internal/api/ -run TestComponentTypesAPI`
Expected: FAIL (GET `/component-types` is 404).

- [ ] **Step 7: Add the component_type routes**

In `internal/api/components.go`, add the wire types and the four routes inside `registerComponentRoutes`, mirroring Task 3 with `component`/`component_type`. Wire types:

```go
type componentTypeBody struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Rank        int    `json:"rank"`
	Official    bool   `json:"official"`
}

type listComponentTypesOutput struct {
	Body struct {
		ComponentTypes []componentTypeBody `json:"component_types"`
	}
}

type componentTypePathInput struct {
	ID string `path:"id" doc:"The component_type id"`
}

type createComponentTypeInput struct {
	Body struct {
		ID          string `json:"id" minLength:"1" doc:"Globally unique type id"`
		DisplayName string `json:"display_name" minLength:"1"`
		Rank        int    `json:"rank,omitempty"`
	}
}

type updateComponentTypeInput struct {
	ID   string `path:"id"`
	Body struct {
		DisplayName *string `json:"display_name,omitempty"`
		Rank        *int    `json:"rank,omitempty"`
	}
}

type componentTypeOutput struct {
	Body componentTypeBody
}
```

Routes inside `registerComponentRoutes`:

```go
	huma.Register(api, huma.Operation{
		OperationID: "list-component-types",
		Method:      http.MethodGet,
		Path:        "/component-types",
		Summary:     "List component types",
		Description: "Lists the component_type registry, ordered by rank. Populates the type picker on the component form. Gated by type:read.",
		Middlewares: huma.Middlewares{a.authn, a.require("type", "read")},
	}, func(ctx context.Context, _ *struct{}) (*listComponentTypesOutput, error) {
		types, err := gw.ListComponentTypes(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("list component types")
		}
		out := &listComponentTypesOutput{}
		out.Body.ComponentTypes = make([]componentTypeBody, 0, len(types))
		for i := range types {
			out.Body.ComponentTypes = append(out.Body.ComponentTypes, componentTypeBody{
				ID: types[i].ID, DisplayName: types[i].DisplayName, Rank: types[i].Rank, Official: types[i].Official,
			})
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-component-type",
		Method:        http.MethodPost,
		Path:          "/component-types",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a component type",
		Description:   "Creates a custom (non-official) component_type. Gated by type:create.",
		Middlewares:   huma.Middlewares{a.authn, a.require("type", "create")},
	}, func(ctx context.Context, in *createComponentTypeInput) (*componentTypeOutput, error) {
		ct, err := gw.CreateComponentType(ctx, actorID(ctx), storage.ComponentType{
			ID: in.Body.ID, DisplayName: in.Body.DisplayName, Rank: in.Body.Rank,
		})
		if err != nil {
			return nil, mapTypeErr(err, "component_type")
		}
		return &componentTypeOutput{Body: componentTypeBody{ID: ct.ID, DisplayName: ct.DisplayName, Rank: ct.Rank, Official: ct.Official}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-component-type",
		Method:      http.MethodPatch,
		Path:        "/component-types/{id}",
		Summary:     "Update a component type",
		Description: "Patches a custom component_type's display_name or rank. Official types are read-only (422). Gated by type:update.",
		Middlewares: huma.Middlewares{a.authn, a.require("type", "update")},
	}, func(ctx context.Context, in *updateComponentTypeInput) (*componentTypeOutput, error) {
		ct, err := gw.UpdateComponentType(ctx, actorID(ctx), in.ID, storage.ComponentTypePatch{
			DisplayName: in.Body.DisplayName, Rank: in.Body.Rank,
		})
		if err != nil {
			return nil, mapTypeErr(err, "component_type")
		}
		return &componentTypeOutput{Body: componentTypeBody{ID: ct.ID, DisplayName: ct.DisplayName, Rank: ct.Rank, Official: ct.Official}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-component-type",
		Method:        http.MethodDelete,
		Path:          "/component-types/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a component type",
		Description:   "Deletes a custom component_type, refused if official (422) or referenced by a component (409). Gated by type:delete.",
		Middlewares:   huma.Middlewares{a.authn, a.require("type", "delete")},
	}, func(ctx context.Context, in *componentTypePathInput) (*struct{}, error) {
		if err := gw.DeleteComponentType(ctx, actorID(ctx), in.ID); err != nil {
			return nil, mapTypeErr(err, "component_type")
		}
		return nil, nil
	})
```

- [ ] **Step 8: Run the e2e test, verify it passes**

Run: `go test ./internal/api/ -run TestComponentTypesAPI`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/storage/components.go internal/storage/storage.go internal/storage/unimplemented.go internal/storage/component_types_test.go internal/api/components.go internal/api/component_types_e2e_test.go
git commit -m "feat: component_type registry CRUD (storage + API)"
```

---

### Task 5: Regenerate clients, document, and finalize

**Files:**
- Modify (generated): `api/openapi.json`, `web/src/api/schema.gen.ts`, `internal/cli/api_gen.go`, any `docsgen` output
- Modify: an architecture docs page under `docs/src/content/docs/architecture/` (the identity-access or storage page) + the decision log if design diverged
- Add (already written, commit on branch): `docs/superpowers/specs/2026-07-13-types-catalog-design.md`, `docs/superpowers/plans/2026-07-13-type-registry-crud-backend.md`

**Interfaces:**
- Consumes: everything from Tasks 1-4 via the OpenAPI spec.

- [ ] **Step 1: Regenerate**

Run: `make gen`
Expected: updates `api/openapi.json` with the new `*-types` paths, regenerates the CLI (`internal/cli/api_gen.go` gains `create/update/delete` commands for the three type registries) and the web client (`web/src/api/schema.gen.ts`).

- [ ] **Step 2: Confirm the generated CLI compiles and the drift is real**

Run: `go build ./... && git status --porcelain`
Expected: build PASS; `git status` shows the four generated files changed. If `make gen` produced no diff, a route was mis-registered; revisit Tasks 2-4.

- [ ] **Step 3: Add the architecture docs note**

In the identity-access architecture page (`docs/src/content/docs/architecture/identity-access.md`), add a short paragraph: the `type` resource is capability-only and unscoped (global reference data), gating the location/system/component type registries; `type:read` is covered by the `viewer` `*:read` floor, and `type:create|update|delete` is granted to admin. Advance the page's status badge floor if the doctrine requires it, and add the `status.mdx` build-progress note per docs-with-everything. If the built behavior diverged from any page's design, record it in the [decision log](../../../docs/src/content/docs/architecture/decisions.md).

- [ ] **Step 4: Run the full test suite**

Run: `make test`
Expected: PASS (Go + web). Paste the tail of the output into the eventual ship-review.

- [ ] **Step 5: Commit**

```bash
git add api/openapi.json web/src/api/schema.gen.ts internal/cli/api_gen.go docs/
git add docs/superpowers/specs/2026-07-13-types-catalog-design.md docs/superpowers/plans/2026-07-13-type-registry-crud-backend.md
git commit -m "chore: regenerate client + CLI for the type registry, document the type resource"
```

- [ ] **Step 6: Run `/ship-slice` and open the PR**

Run `/ship-slice` (fresh `make test` with output pasted, `make gen` drift check, em-dash + attribution scan, reviewer pass, docs-with-everything). This is a backend slice with no operator-facing UI surface, so the Visual confirmation section is "n/a". Its emitted ship-review becomes the PR body. Push the branch and open the PR titled `feat: type registry CRUD API (location/system/component types)`, `Fixes #219`.

---

## Notes for the implementer

- **Sanity on the FK backstop:** deleting an in-use type is caught twice (the gateway pre-count `ErrTypeInUse` and, if a race slipped past it, the parent FK's `NO ACTION`). The pre-count gives the friendly 409; do not remove it in favor of catching the raw FK error.
- **Audit label vs permission resource:** the audit rows use the kind-specific label (`location_type`, `system_type`, `component_type`); the permission resource is the single `type`. Both are intentional.
- **`secret_type` is untouched.** Do not add write routes for it in this slice; its fields-schema editor is a separate follow-up.
- **Seeded id collisions in tests:** the tests assume `wing`/`kiosk`/`sensor` are not seeded and `campus`/`display` are. Verify against the current `internal/seed/*_types.yaml` before running; swap ids if the seed set changed.
