# Inventory technical-name rename + inline name check — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an operator rename a component/system/location technical name (`name`) from the detail accordion in edit mode, with an inline check button that reports slug-format and availability before saving.

**Architecture:** `name` is a mutable `unique` column; `id uuid` is the identity and every FK/binding references it, so a rename is a one-column `UPDATE` with no cascade. A shared `ValidateEntityName` slug validator (server source of truth, mirrored client-side) gates both create and rename. A collection-level, scope-blind `POST /<entity>:checkName` powers the advisory inline check; Save stays enabled and the global unique constraint (409) is the real gate. Systems is the validated reference; Components and Locations mirror it.

**Tech Stack:** Go + Huma v2 (API), pgx + PostgreSQL (storage, dbmate migrations — none needed here), testcontainers-go (integration), SolidJS + daisyUI + openapi-fetch typed client (UI), vitest + @solidjs/testing-library (web tests). Codegen: `make gen` (OpenAPI → typed client + cobra CLI).

## Global Constraints

- **No em dashes** in any written artifact (code comments, docs, PR). Use commas, colons, periods, parentheses.
- **No AI/assistant attribution** in commits, PRs, comments, or any visible artifact.
- **PR title + commit subjects lowercase the first letter after the conventional-commit prefix** (`feat: rename technical names...`, not `feat: Rename...`).
- **Slug rule (verbatim):** `^[a-z0-9][a-z0-9-]*$`, max length 100.
- **Test-first:** every behavior change lands with a test that failed before and passes after. `make test` green before the PR.
- **API-first:** the Go Huma struct is the source of truth; `make gen` regenerates the typed client + CLI. Never hand-edit generated files.
- **Additive migrations only** (none required here; no schema change).
- **Worktree, PR-only:** branch from `origin/main` in a worktree under `.claude/worktrees/`; never commit to `main`.
- **`/ship-slice` before the PR** (fresh `make test`, `make gen` drift, em-dash + attribution scan, reviewer pass, live screenshots).

---

## File structure

- `internal/storage/names.go` — **create.** The shared `ValidateEntityName` + `ErrInvalidName`.
- `internal/storage/names_test.go` — **create.** Validator table test.
- `internal/storage/systems.go` / `components.go` / `locations.go` — **modify.** `Name *string` on each patch; validate in Create + Update; `set name`; add `SystemNameTaken` / `ComponentNameTaken` / `LocationNameTaken`.
- `internal/storage/*_rename_test.go` — **create** per entity. Rename round-trip integration tests.
- `internal/api/systems.go` / `components.go` / `locations.go` — **modify.** `Name *string` (pattern) on update input; pattern on create input; `check-*-name` operation; `ErrInvalidName` in the `map*Err`.
- `internal/api/*_test.go` — **modify/create.** Rename + checkName API tests.
- `web/src/lib/systems.ts` / `components.ts` / `locations.ts` — **modify.** `name?` on the `Update*` type; add `check*Name`.
- `web/src/pages/Systems.tsx` / `Components.tsx` / `Locations.tsx` — **modify.** Unlock the Technical-name input in edit; inline check button + status; navigate-on-rename.
- `web/src/pages/*.test.tsx` — **modify.** Inline-check states, navigate-on-rename, view stays read-only.
- `docs/src/content/docs/architecture/status.mdx` — **modify.** Build-progress entry.
- `docs/src/content/docs/guides/operator/entities.md` — **modify.** Rename + check paragraph.

---

## Task 1: Shared `ValidateEntityName` slug validator

**Files:**
- Create: `internal/storage/names.go`
- Test: `internal/storage/names_test.go`

**Interfaces:**
- Produces: `func ValidateEntityName(name string) error` (returns `ErrInvalidName` on a bad slug, else nil); `var ErrInvalidName = errors.New("storage: invalid entity name")`.

- [ ] **Step 1: Write the failing test**

```go
// internal/storage/names_test.go
package storage

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateEntityName(t *testing.T) {
	valid := []string{"a", "av-rack-3", "boardroom-a", "meeting-room", "x0", strings.Repeat("a", 100)}
	for _, n := range valid {
		if err := ValidateEntityName(n); err != nil {
			t.Errorf("ValidateEntityName(%q) = %v, want nil", n, err)
		}
	}
	invalid := []string{"", "-lead", "Uppercase", "has space", "under_score", "tab\t", "dot.name", strings.Repeat("a", 101)}
	for _, n := range invalid {
		if err := ValidateEntityName(n); !errors.Is(err, ErrInvalidName) {
			t.Errorf("ValidateEntityName(%q) = %v, want ErrInvalidName", n, err)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/ -run TestValidateEntityName`
Expected: FAIL (undefined: `ValidateEntityName`, `ErrInvalidName`).

- [ ] **Step 3: Write minimal implementation**

```go
// internal/storage/names.go
package storage

import (
	"errors"
	"regexp"
)

// ErrInvalidName is returned when a proposed entity name (the technical name /
// URL slug) does not match the slug rule. The API maps it to 422.
var ErrInvalidName = errors.New("storage: invalid entity name")

// entityNameRe is the slug rule for technical names: lowercase letters and
// digits and hyphens, starting with a letter or digit. Shared by create and
// rename so both surfaces agree; mirrored client-side for the inline check.
var entityNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ValidateEntityName enforces the slug rule and a 100-char ceiling. It is the
// server-side source of truth for a component/system/location technical name.
func ValidateEntityName(name string) error {
	if len(name) > 100 || !entityNameRe.MatchString(name) {
		return ErrInvalidName
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/storage/ -run TestValidateEntityName`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/storage/names.go internal/storage/names_test.go
git commit -m "feat: add shared entity-name slug validator"
```

---

## Task 2: Systems storage — rename + NameTaken + create tightening

**Files:**
- Modify: `internal/storage/systems.go` (`SystemPatch` ~line 61; `CreateSystem` insert ~line 245; `UpdateSystem` SQL ~line 275; add `SystemNameTaken`)
- Test: `internal/storage/systems_rename_test.go` (create)

**Interfaces:**
- Consumes: `ValidateEntityName`, `ErrInvalidName` (Task 1); existing `ErrSystemExists`, `mapSystemWriteErr`, `systemCols`, `scanSystem`.
- Produces: `SystemPatch.Name *string`; `func (p *PG) SystemNameTaken(ctx context.Context, name string) (bool, error)`.

- [ ] **Step 1: Write the failing integration test**

```go
// internal/storage/systems_rename_test.go
package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

func TestRenameSystem(t *testing.T) {
	ctx := context.Background()
	gw := newTestGateway(t) // existing helper: migrated ephemeral PG + seeded types
	all := scope.Set{All: true}

	// A system with a child, so we can prove the UUID FK survives the rename.
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "av-root", SystemType: "meeting-room"}, all); err != nil {
		t.Fatal(err)
	}
	child, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "av-child", SystemType: "meeting-room", ParentName: strptr("av-root")}, all)
	if err != nil {
		t.Fatal(err)
	}

	// Rename the parent.
	newName := "av-root-renamed"
	up, err := gw.UpdateSystem(ctx, "", "av-root", storage.SystemPatch{Name: &newName}, all, all)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if up.Name != newName {
		t.Fatalf("name = %q, want %q", up.Name, newName)
	}

	// The child's parent_id (a UUID FK) is untouched: the child still resolves and
	// its parent is the same row.
	got, err := gw.GetSystem(ctx, "av-child", all)
	if err != nil {
		t.Fatal(err)
	}
	if got.ParentID == nil || *got.ParentID != up.ID {
		t.Fatalf("child parent_id = %v, want %q (rename must not touch UUID FKs)", got.ParentID, up.ID)
	}
	_ = child

	// The old name is free; a create can reuse it.
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "av-root", SystemType: "meeting-room"}, all); err != nil {
		t.Fatalf("old name should be free after rename: %v", err)
	}

	// Renaming onto a taken name -> ErrSystemExists.
	if _, err := gw.UpdateSystem(ctx, "", "av-child", storage.SystemPatch{Name: &newName}, all, all); !errors.Is(err, storage.ErrSystemExists) {
		t.Fatalf("dup rename err = %v, want ErrSystemExists", err)
	}

	// Bad slug -> ErrInvalidName (before touching the DB).
	bad := "Bad Name"
	if _, err := gw.UpdateSystem(ctx, "", "av-child", storage.SystemPatch{Name: &bad}, all, all); !errors.Is(err, storage.ErrInvalidName) {
		t.Fatalf("bad-format rename err = %v, want ErrInvalidName", err)
	}

	// SystemNameTaken is scope-blind existence.
	if taken, err := gw.SystemNameTaken(ctx, newName); err != nil || !taken {
		t.Fatalf("SystemNameTaken(%q) = %v,%v want true,nil", newName, taken, err)
	}
	if taken, err := gw.SystemNameTaken(ctx, "nope-not-here"); err != nil || taken {
		t.Fatalf("SystemNameTaken(free) = %v,%v want false,nil", taken, err)
	}
}
```

(If `newTestGateway`/`strptr` names differ, match the helpers already used in `internal/storage/systems_scope_test.go`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/ -run TestRenameSystem`
Expected: FAIL (`SystemPatch` has no field `Name`; no method `SystemNameTaken`).

- [ ] **Step 3: Implement — patch field, validation, SQL, NameTaken**

In `internal/storage/systems.go`, add `Name` to the patch:

```go
// SystemPatch is the update input: nil fields unchanged.
type SystemPatch struct {
	Name        *string
	DisplayName *string
	SystemType  *string
}
```

In `CreateSystem`, validate the name up front (tightens create), right after `defer tx.Rollback`:

```go
	if err := ValidateEntityName(spec.Name); err != nil {
		return nil, err
	}
```

In `UpdateSystem`, validate a provided new name before the write, then add `name` to the `set`:

```go
	if patch.Name != nil {
		if err := ValidateEntityName(*patch.Name); err != nil {
			return nil, err
		}
	}
	after, err := scanSystem(tx.QueryRow(ctx, `
		update system set
			name         = coalesce($2, name),
			display_name = coalesce($3, display_name),
			system_type  = coalesce($4, system_type),
			updated_at   = now()
		where id = $1
		returning `+systemCols,
		before.ID, patch.Name, patch.DisplayName, patch.SystemType))
```

Add the scope-blind existence check (place near `systemByName`):

```go
// SystemNameTaken reports whether a system with this name exists. Scope-blind
// by design: the name unique constraint is global, so availability must be a
// global fact to match it (a scope-aware answer would false-positive on a name
// held outside the caller's scope). Gated at the API by system:update.
func (p *PG) SystemNameTaken(ctx context.Context, name string) (bool, error) {
	var exists bool
	if err := p.pool.QueryRow(ctx, `select exists(select 1 from system where name = $1)`, name).Scan(&exists); err != nil {
		return false, fmt.Errorf("storage: system name taken: %w", err)
	}
	return exists, nil
}
```

Add `SystemNameTaken` to the `Gateway` interface in `internal/storage/storage.go` (next to `UpdateSystem`) and to `UnimplementedGateway` in `internal/storage/unimplemented.go`:

```go
// storage.go (interface)
	SystemNameTaken(ctx context.Context, name string) (bool, error)
```

```go
// unimplemented.go
func (UnimplementedGateway) SystemNameTaken(context.Context, string) (bool, error) {
	return false, errUnimplemented
}
```

(Match the exact `errUnimplemented`/panic idiom already used in that file.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/storage/ -run 'TestRenameSystem|TestValidateEntityName'`
Expected: PASS. Then `go test ./internal/storage/` to confirm no existing storage test regressed (create tightening: existing fixtures use slug names, so they stay green).

- [ ] **Step 5: Commit**

```bash
git add internal/storage/systems.go internal/storage/storage.go internal/storage/unimplemented.go internal/storage/systems_rename_test.go
git commit -m "feat: rename systems by technical name, scope-blind name check"
```

---

## Task 3: Systems API — Name on update input, checkName op, error map

**Files:**
- Modify: `internal/api/systems.go` (`createSystemInput` ~line 83; `updateSystemInput` ~line 93; update-system handler ~line 258; `mapSystemErr` ~line 288; add the checkName op + its I/O structs)
- Test: `internal/api/systems_test.go` (add cases; match the file's existing harness)

**Interfaces:**
- Consumes: `storage.SystemPatch.Name`, `gw.SystemNameTaken`, `storage.ValidateEntityName`, `storage.ErrInvalidName` (Tasks 1-2); existing `a.require`, `a.scopeFor`, `mapSystemErr`, `actorID`.
- Produces: `POST /systems:checkName` (OperationID `check-system-name`) returning `{valid, available, reason}`.

- [ ] **Step 1: Write the failing API test**

Add to `internal/api/systems_test.go` (mirror the existing request helpers in that file — `newTestAPI`, an all-scoped token, a `do`/`request` helper):

```go
func TestSystemRenameAndCheckName(t *testing.T) {
	api, _ := newTestAPI(t) // existing helper: wired huma.API + seeded gateway + owner token

	// Seed a system.
	mustPOST(t, api, "/systems", `{"name":"av-one","system_type":"meeting-room"}`)

	// checkName: taken.
	r := mustPOST(t, api, "/systems:checkName", `{"name":"av-one"}`)
	assertJSON(t, r, `"available":false`)
	assertJSON(t, r, `"valid":true`)

	// checkName: available.
	r = mustPOST(t, api, "/systems:checkName", `{"name":"av-free"}`)
	assertJSON(t, r, `"available":true`)

	// checkName: bad format -> valid:false, still 200.
	r = mustPOST(t, api, "/systems:checkName", `{"name":"Bad Name"}`)
	assertJSON(t, r, `"valid":false`)

	// Rename via PATCH.
	r = mustPATCH(t, api, "/systems/av-one", `{"name":"av-renamed"}`)
	assertJSON(t, r, `"name":"av-renamed"`)

	// Dup rename -> 409.
	mustPOST(t, api, "/systems", `{"name":"av-two","system_type":"meeting-room"}`)
	assertStatus(t, mustPATCHRaw(t, api, "/systems/av-two", `{"name":"av-renamed"}`), 409)

	// Bad format via PATCH -> 422 (Huma pattern rejects at the edge).
	assertStatus(t, mustPATCHRaw(t, api, "/systems/av-two", `{"name":"Bad Name"}`), 422)
}
```

(Use whatever assert/request helpers `systems_test.go` already defines; the names above are illustrative of intent, not new helpers to invent.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestSystemRenameAndCheckName`
Expected: FAIL (no `:checkName` route; PATCH ignores `name`).

- [ ] **Step 3: Implement — input field, checkName op, error map**

Add the pattern to `createSystemInput.Body.Name`:

```go
		Name        string  `json:"name" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"Globally unique name (the address; lowercase letters, digits, hyphens)"`
```

Add `Name` to `updateSystemInput.Body`:

```go
type updateSystemInput struct {
	Name string `path:"name"`
	Body struct {
		Name        *string `json:"name,omitempty" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"A new globally unique technical name (rename)"`
		DisplayName *string `json:"display_name,omitempty"`
		SystemType  *string `json:"system_type,omitempty"`
	}
}
```

Pass it through in the update-system handler:

```go
		s, err := gw.UpdateSystem(ctx, actorID(ctx), in.Name, storage.SystemPatch{
			Name:        in.Body.Name,
			DisplayName: in.Body.DisplayName,
			SystemType:  in.Body.SystemType,
		}, a.scopeFor(ctx, "system", "read"), a.scopeFor(ctx, "system", "update"))
```

Add the checkName I/O structs (near `systemOutput`):

```go
type checkNameInput struct {
	Body struct {
		Name string `json:"name" doc:"The proposed technical name to check"`
	}
}

type checkNameOutput struct {
	Body struct {
		Valid     bool   `json:"valid" doc:"Whether the name matches the slug rule"`
		Available bool   `json:"available" doc:"Whether the name is free (scope-blind, matches the global unique constraint)"`
		Reason    string `json:"reason,omitempty" doc:"Human explanation when not valid or not available"`
	}
}
```

Register the operation inside `registerSystemRoutes` (after update-system):

```go
	huma.Register(api, huma.Operation{
		OperationID: "check-system-name",
		Method:      http.MethodPost,
		Path:        "/systems:checkName",
		Summary:     "Check a system technical name",
		Description: "Reports whether a proposed technical name is a valid slug and currently free. Advisory (Save is still gated by the unique constraint). Availability is scope-blind to match the global unique constraint. Gated by system:update.",
		Middlewares: huma.Middlewares{a.authn, a.require("system", "update")},
	}, func(ctx context.Context, in *checkNameInput) (*checkNameOutput, error) {
		out := &checkNameOutput{}
		if err := storage.ValidateEntityName(in.Body.Name); err != nil {
			out.Body.Valid = false
			out.Body.Reason = "Use lowercase letters, digits, and hyphens."
			return out, nil
		}
		out.Body.Valid = true
		taken, err := gw.SystemNameTaken(ctx, in.Body.Name)
		if err != nil {
			return nil, huma.Error500InternalServerError("check system name")
		}
		out.Body.Available = !taken
		if taken {
			out.Body.Reason = "That name is already taken."
		}
		return out, nil
	})
```

Add `ErrInvalidName` to `mapSystemErr`:

```go
	case errors.Is(err, storage.ErrInvalidName):
		return huma.Error422UnprocessableEntity("invalid name")
```

(The `checkNameInput`/`checkNameOutput` structs are shared across the three entities; define them once here and reuse in components/locations, or keep per-file — pick one and keep it consistent. If shared, name them `checkNameInput`/`checkNameOutput` in one file and do not redeclare.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestSystemRenameAndCheckName`
Expected: PASS. Then `go test ./internal/api/` for no regressions.

- [ ] **Step 5: Commit**

```bash
git add internal/api/systems.go internal/api/systems_test.go
git commit -m "feat: system rename input + collection-level name check endpoint"
```

---

## Task 4: `make gen` + Systems UI (unlock field, inline check, navigate-on-rename)

**Files:**
- Modify (generated by `make gen`, do not hand-edit): the OpenAPI + typed client + CLI.
- Modify: `web/src/lib/systems.ts` (`UpdateSystem` type ~line 46; add `checkSystemName`)
- Modify: `web/src/pages/Systems.tsx` (edit signals ~line 136; seed effect ~line 141; bound save ~line 150; the Technical-name field ~line 205)
- Modify: `web/src/pages/Systems.test.tsx`

**Interfaces:**
- Consumes: the generated `api.POST("/systems:checkName")` + the `name?` field on the PATCH body (from `make gen`).
- Produces: `updateSystem(name, { name?, display_name?, system_type? })`; `checkSystemName(name): Promise<{ valid: boolean; available: boolean; reason?: string }>`.

- [ ] **Step 1: Regenerate the client + CLI**

Run: `make gen`
Expected: the generated OpenAPI/client/CLI now carry `/systems:checkName` and the PATCH `name` field. `git status` shows generated files changed; do not hand-edit them.

- [ ] **Step 2: Write the failing web test**

Add to `web/src/pages/Systems.test.tsx` (follow the file's existing cache-seed + router harness):

```tsx
test("edit mode exposes an editable technical name with a check button", async () => {
  seedSystems([{ id: "u1", name: "av-one", system_type: "meeting-room" }]); // existing seed helper
  renderAt("/systems/av-one"); // existing router-render helper
  // enter edit via the pending-edit handoff or the Edit affordance the harness uses
  await enterEdit();
  const nameInput = await screen.findByDisplayValue("av-one");
  expect(nameInput).not.toBeDisabled();
  expect(screen.getByRole("button", { name: /check name/i })).toBeInTheDocument();
});

test("a fresh detail view keeps the technical name read-only", async () => {
  seedSystems([{ id: "u1", name: "av-one", system_type: "meeting-room" }]);
  renderAt("/systems/av-one");
  expect(screen.queryByRole("button", { name: /check name/i })).not.toBeInTheDocument();
});
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd web && npx vitest run src/pages/Systems.test.tsx`
Expected: FAIL (no editable name input / no check button in edit).

- [ ] **Step 4: Implement the client wrappers**

In `web/src/lib/systems.ts`:

```ts
export type UpdateSystem = {
  name?: string;
  display_name?: string;
  system_type?: string;
};
```

```ts
export type NameCheck = { valid: boolean; available: boolean; reason?: string };

export async function checkSystemName(name: string): Promise<NameCheck> {
  const { data, error } = await api.POST("/systems:checkName", { body: { name } });
  if (error) throw error;
  return data as NameCheck;
}
```

- [ ] **Step 5: Implement the UI in `SystemDetail`**

Add the `Search` icon to the import from `../components/icons` (line 22): `import { ArrowRight, ChevronRight, Pencil, Plus, Save, Search, X } from "../components/icons";` and `checkSystemName, type NameCheck` from `../lib/systems`.

Add signals beside `display`/`type` (after line 137):

```tsx
    const [name, setName] = createSignal(n().raw.name);
    const [nameCheck, setNameCheck] = createSignal<NameCheck | null>(null);
    const [checking, setChecking] = createSignal(false);
    async function runCheck() {
      setChecking(true);
      try { setNameCheck(await checkSystemName(name().trim())); }
      catch { setNameCheck(null); }
      finally { setChecking(false); }
    }
```

Seed `name` and clear the check when edit begins (extend the effect at line 141):

```tsx
    createEffect(on(editing, (isEditing) => {
      if (isEditing) { setDisplay(n().raw.display_name ?? ""); setType(n().raw.system_type ?? ""); setName(n().raw.name); setNameCheck(null); }
    }));
```

In the bound `save` (line 150), send `name` only when it changed and navigate on a rename:

```tsx
      save: async () => {
        setSaveErr(null);
        const renamed = name().trim() !== n().raw.name;
        try {
          await updateSystem(n().raw.name, {
            name: renamed ? name().trim() : undefined,
            display_name: display() || undefined,
            system_type: type() || undefined,
          });
          await qc.invalidateQueries({ queryKey: SYSTEMS_KEY });
          if (renamed) navigate(`/systems/${encodeURIComponent(name().trim())}`);
        } catch (e) {
          setSaveErr(describeError(e));
          throw e;
        }
      },
```

Replace the disabled Technical-name field (line 205) with an editable input + inline check button + status:

```tsx
              {ctx.field(
                "Technical name",
                <>
                  <div class="join w-full">
                    <input
                      class="input input-bordered join-item w-full font-data"
                      value={name()}
                      onInput={(e) => { setName(e.currentTarget.value); setNameCheck(null); }}
                    />
                    <button
                      type="button"
                      class="btn btn-square join-item"
                      aria-label="Check name"
                      title="Check availability"
                      disabled={checking() || !name().trim()}
                      onClick={() => void runCheck()}
                    >
                      <Search size={15} />
                    </button>
                  </div>
                  <Show when={nameCheck()}>
                    {(c) => (
                      <span
                        class="text-[11px]"
                        classList={{ "text-success": c().valid && c().available, "text-error": !c().valid || !c().available }}
                      >
                        {!c().valid ? (c().reason ?? "Use lowercase, digits, hyphens.") : c().available ? "Available" : (c().reason ?? "Taken")}
                      </span>
                    )}
                  </Show>
                </>,
                "Renaming changes the address; existing links to the old name stop resolving.",
              )}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd web && npx vitest run src/pages/Systems.test.tsx && npx tsc --noEmit`
Expected: PASS; tsc clean (ignore the pre-existing `theme-badges.test` node-import errors).

- [ ] **Step 7: Commit**

```bash
git add web/ internal/ openapi* cli* # whatever make gen touched, plus the edited web files
git commit -m "feat: rename a system from the detail with an inline name check"
```

---

## Task 5: Components mirror

**Files:**
- Modify: `internal/storage/components.go` (add `ComponentPatch.Name`, validate in create+update, `set name`, `ComponentNameTaken`), `internal/storage/storage.go`, `internal/storage/unimplemented.go`
- Test: `internal/storage/components_rename_test.go` (create)
- Modify: `internal/api/components.go` (`Name` on update input + create pattern, `check-component-name` op, `ErrInvalidName` in `mapComponentErr`)
- Test: `internal/api/components_test.go`
- Modify: `web/src/lib/components.ts`, `web/src/pages/Components.tsx`, `web/src/pages/Components.test.tsx`

Apply the exact same changes as Tasks 2-4, substituting `Component`/`component`/`COMPONENTS_KEY`/`updateComponent`/`checkComponentName`. Preserve the Components page's `EffectiveSecrets`/`EffectiveVariables` panels and its `ID` fact — only the Technical-name field, edit signals, seed effect, and bound save change.

- [ ] **Step 1: Storage rename test (mirror Task 2 Step 1) for components** — write `internal/storage/components_rename_test.go` (rename a component that owns a tag/variable/secret binding; assert the binding's `component_id` UUID FK survives and the old name frees).
- [ ] **Step 2: Run it — FAIL** (`ComponentPatch` has no `Name`). `go test ./internal/storage/ -run TestRenameComponent`
- [ ] **Step 3: Implement `ComponentPatch.Name`, validation in `CreateComponent`+`UpdateComponent`, `set name`, `ComponentNameTaken` + interface + unimplemented** (mirror Task 2 Step 3).
- [ ] **Step 4: Run — PASS.** `go test ./internal/storage/`
- [ ] **Step 5: API test (mirror Task 3 Step 1) for components — FAIL, then implement `Name` on `updateComponentInput`, create pattern, `check-component-name` op, `ErrInvalidName` in `mapComponentErr` — PASS.** `go test ./internal/api/`
- [ ] **Step 6: `make gen`, then `web/src/lib/components.ts` (`UpdateComponent.name?`, `checkComponentName`) and `Components.tsx` (mirror Task 4 Step 5, keeping the Effective panels), and `Components.test.tsx` — PASS + tsc.** `cd web && npx vitest run src/pages/Components.test.tsx && npx tsc --noEmit`
- [ ] **Step 7: Commit**

```bash
git add internal/ web/ openapi* cli*
git commit -m "feat: rename a component from the detail with an inline name check"
```

---

## Task 6: Locations mirror

**Files:**
- Modify: `internal/storage/locations.go` (add `LocationPatch.Name`, validate, `set name`, `LocationNameTaken`), `internal/storage/storage.go`, `internal/storage/unimplemented.go`
- Test: `internal/storage/locations_rename_test.go` (create)
- Modify: `internal/api/locations.go` (`Name` on update input + create pattern, `check-location-name` op, `ErrInvalidName` in `mapLocationErr`)
- Test: `internal/api/locations_test.go`
- Modify: `web/src/lib/locations.ts`, `web/src/pages/Locations.tsx`, `web/src/pages/Locations.test.tsx`

Apply the same changes as Tasks 2-4, substituting `Location`/`location`/`LOCATIONS_KEY`/`updateLocation`/`checkLocationName`. Keep the Locations page's `<select>` for `location_type` and its `leadIcon` + "Contains" fact — only the Technical-name field, edit signals, seed effect, and bound save change.

- [ ] **Step 1: Storage rename test for locations — FAIL** (rename a location that has a system/component placed in it; assert the placement's `location_id` UUID FK survives and the old name frees). `go test ./internal/storage/ -run TestRenameLocation`
- [ ] **Step 2: Implement `LocationPatch.Name`, validation in create+update, `set name`, `LocationNameTaken` + interface + unimplemented — PASS.** `go test ./internal/storage/`
- [ ] **Step 3: API test for locations — FAIL, then implement `Name` on `updateLocationInput`, create pattern, `check-location-name` op, `ErrInvalidName` in `mapLocationErr` — PASS.** `go test ./internal/api/`
- [ ] **Step 4: `make gen`, then `web/src/lib/locations.ts` and `Locations.tsx` (mirror Task 4 Step 5, keeping the `<select>` and the fact) and `Locations.test.tsx` — PASS + tsc.** `cd web && npx vitest run src/pages/Locations.test.tsx && npx tsc --noEmit`
- [ ] **Step 5: Commit**

```bash
git add internal/ web/ openapi* cli*
git commit -m "feat: rename a location from the detail with an inline name check"
```

---

## Task 7: Docs + ship

**Files:**
- Modify: `docs/src/content/docs/architecture/status.mdx` (build-progress entry)
- Modify: `docs/src/content/docs/guides/operator/entities.md` (rename + check paragraph)
- Modify: `docs/src/content/docs/architecture/decisions.md` (only if the build diverged from the spec)

- [ ] **Step 1: status.mdx** — add a build-progress note: technical-name rename landed for components/systems/locations with a collection-level scope-blind `:checkName` and an advisory inline check; the slug rule now gates create too. Advance any affected page's status floor if warranted.
- [ ] **Step 2: entities.md** — document: in edit mode the technical name is editable; the check button reports valid/available; Save renames and the URL follows; the old name stops resolving; the name rule is lowercase letters, digits, hyphens.
- [ ] **Step 3: decision log** — add a row only if the build diverged from `docs/superpowers/specs/2026-07-14-inventory-rename-design.md`. If it matched, no ADR needed (the spec is the record).
- [ ] **Step 4: Full gate.** Run: `make test` (paste the output into the PR). Expected: all Go packages + web green.
- [ ] **Step 5: `make gen` drift check.** Run: `make gen` then `git status` — expected: clean (no drift).
- [ ] **Step 6: `/ship-slice`** — run it. It performs the em-dash + attribution scan, the reviewer pass, docs-with-everything, and requires **live screenshots** of the three edit surfaces (editable name + check button showing Available / Taken / bad-format, and a completed rename landing at the new URL). Its ship-review becomes the PR body.
- [ ] **Step 7: Commit docs + open the PR**

```bash
git add docs/
git commit -m "docs: technical-name rename + inline name check"
git push -u origin feat/inventory-rename
gh pr create --repo hyperscaleav/omniglass --base main \
  --title "feat: rename technical names for inventory entities + inline name check" \
  --body-file <ship-review> # closes #245
```

---

## Self-review

**Spec coverage:** shared validator (Task 1) ✓; `Name` on the three patches + `set name` + no-cascade rename proof (Tasks 2/5/6) ✓; scope-blind `NameTaken` (Tasks 2/5/6) ✓; collection-level `:checkName` gated on `<entity>:update` returning `{valid, available, reason}` (Tasks 3/5/6) ✓; create tightening via shared validator + Huma pattern (Tasks 2/3) ✓; UI unlock + inline check button + status + navigate-on-rename, advisory (Save always enabled, 409 real gate via existing `saveErr`) (Tasks 4/5/6) ✓; CLI via `make gen` (Tasks 4/5/6) ✓; tests at all three tiers (every task) ✓; docs (Task 7) ✓. Out-of-scope items (old-name redirect, create-draft live check, type-id rename) are excluded, matching the spec.

**Placeholder scan:** the illustrative web/API test helper names (`seedSystems`, `renderAt`, `enterEdit`, `mustPOST`, `assertJSON`) are explicitly flagged to match each test file's existing harness rather than invent new helpers; all production code steps carry full code.

**Type consistency:** `ValidateEntityName`/`ErrInvalidName` (Task 1) are consumed unchanged in 2/3/5/6. `SystemNameTaken`/`ComponentNameTaken`/`LocationNameTaken` are declared in storage, added to the `Gateway` interface + `UnimplementedGateway`, and consumed in the API handlers. `checkSystemName`/`checkComponentName`/`checkLocationName` return the same `NameCheck` shape used in the pages. The PATCH body `name?` field name matches across storage patch, API input, generated client, and the `updateSystem` wrapper.
