# FieldControl Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One generic `FieldControl` primitive that renders a field's resolved value and its override state (read slim, edit stacked), replacing the ad-hoc field rows in `EffectiveFields`.

**Architecture:** A new SolidJS component `web/src/components/FieldControl.tsx` that composes the read (slim) and edit (stacked two-row) modes. Override is an explicit switch; revert = switch off. Bool renders the resolved word (inherited) or an editable toggle (override). Required marks with `*` and validates on submit. The `$` source picker + symbol-plus-name sourced display are drawn but inert this slice. Storage adds `field_definition.required`.

**Tech Stack:** Go + dbmate + Huma (storage/API), SolidJS + daisyUI + Kobalte (the `$` overlay), testcontainers + vitest.

## Global Constraints

- Design of record: `docs/superpowers/specs/2026-07-19-field-override-rendering-design.md` and the approved "Field rendering: the override model" artifact. Match the artifact's visuals.
- No em dashes; no AI attribution. Head-noun-last naming.
- `make gen` clean; `make test` green before PR. Live screenshots for the UI.
- Cascade rule (this slice, component-only): resolved = this component's set value, else the field's type default (the type default is the floor of the cascade). The multi-scope cascade is deferred (#291).
- Closes #283 (bool). Slice issue #290.

---

### Task 1: Storage + API: `field_definition.required`

**Files:**
- Create: `db/migrations/20260719120000_field_definition_required.sql`
- Modify: `internal/storage/fields.go` (FieldDefinition, FieldDefinitionSpec, cols, scan, create/update), `internal/api/fields.go` (bodies/inputs/handlers), `internal/storage/storage.go` + `unimplemented.go` if the signature changes
- Test: `internal/storage/fields_test.go`, `internal/api/fields_e2e_test.go`

**Interfaces:**
- Produces: `FieldDefinition.Required bool`, `FieldDefinitionSpec.Required bool`; API `required` on the definition body and create/update inputs; effective read carries `required`.

- [ ] **Step 1: Migration** (idempotent, additive)

```sql
-- migrate:up
alter table field_definition add column if not exists required boolean not null default false;

-- migrate:down
alter table field_definition drop column if exists required;
```

- [ ] **Step 2: Failing storage test** — `TestFieldDefinitionRequired`: create a definition with `Required: true`, read it back true; a definition without it reads false; update toggles it. Run: `go test ./internal/storage/ -run TestFieldDefinitionRequired` -> FAIL (field missing).

- [ ] **Step 3: Thread `required`** through `FieldDefinition`/`FieldDefinitionSpec`, `fieldDefinitionCols`, `scanFieldDefinition`, `CreateFieldDefinition`, `UpdateFieldDefinition` (add the column to insert/update + returning). Follow the `display_name` precedent exactly.

- [ ] **Step 4: Storage test passes.** Run: `go test ./internal/storage/ -run TestFieldDefinition -count=1` -> PASS.

- [ ] **Step 5: API + effective read** — add `Required bool` to the definition body, the create/update input bodies, and `effectiveFieldBody` (so the panel knows a field is required). Update `internal/api/fields_e2e_test.go` to assert `required` round-trips.

- [ ] **Step 6: `make gen`** and commit the regen. Run: `make gen && git diff --exit-code` -> clean after commit.

- [ ] **Step 7: Commit.** `feat: add field_definition.required`

---

### Task 2: `FieldControl` primitive, read mode

**Files:**
- Create: `web/src/components/FieldControl.tsx`, `web/src/components/FieldControl.test.tsx`

**Interfaces:**
- Produces the component contract consumed by Task 6:

```tsx
export type FieldSource = { kind: "secret" | "variable" | "file"; name: string };
export default function FieldControl(props: {
  label: string;
  dataType: "string" | "int" | "float" | "bool" | "json";
  // The resolved value display (already stringified) and whether it is an override.
  resolved: string;          // "" when unset with no default
  isSet: boolean;            // true = override, false = inherited/default
  source?: FieldSource;      // when the resolved value comes from a secret/variable/file
  required?: boolean;
  // edit wiring (Task 3+); absent/false => read mode
  editing?: boolean;
  overriding?: boolean;      // the switch state
  draft?: string;            // the override input value
  invalid?: boolean;         // submit-validation failed (Task 4)
  onToggleOverride?: (on: boolean) => void;
  onInput?: (v: string) => void;
  onDrillIn?: () => void;
  first?: boolean;
}): JSX.Element;
```

- [ ] **Step 1: Failing read test** — `FieldControl.test.tsx`: read mode (no `editing`) renders the resolved value; an override (`isSet`) shows an accent dot on the key and the value in the accent color; inherited (`!isSet`) is muted with no dot; unset (`resolved===""`) shows the dash. Run vitest -> FAIL (no component).

- [ ] **Step 2: Read markup** — slim one-line row (mirror the artifact read panel): label left (with the override dot when `isSet`), resolved value right (`.set.fwd` accent when `isSet`, muted otherwise; dash when empty). A `source` renders as symbol + name (secret=key, variable, file), never raw `$sec:`. Chevron/drill-in when `onDrillIn`.

- [ ] **Step 3: Read test passes.**

- [ ] **Step 4: Commit.** `feat: FieldControl read mode`

---

### Task 3: `FieldControl` edit mode (stacked + override switch + per-type input)

**Files:** Modify `FieldControl.tsx`, `FieldControl.test.tsx`

- [ ] **Step 1: Failing edit test** — with `editing`: the stacked layout renders; the key row has an Override switch reflecting `overriding`; switch off shows the resolved value (muted), on shows the type-aware input seeded from `draft`; toggling calls `onToggleOverride`; a bool inherited shows the resolved word, a bool override shows an editable toggle; revert = switch off (no separate button). Run -> FAIL.

- [ ] **Step 2: Edit markup** — key row (label + right-aligned switch), value row swaps resolved-vs-input on `overriding`. Reuse `ValueInput` (Variables.tsx) for string/int/float/json; bool override = a real toggle bound to `draft` (`"true"`/`"false"`). The switch calls `onToggleOverride(!overriding)`.

- [ ] **Step 3: Edit test passes.**

- [ ] **Step 4: Commit.** `feat: FieldControl edit mode with override switch`

---

### Task 4: `FieldControl` required + submit validation

**Files:** Modify `FieldControl.tsx`, `FieldControl.test.tsx`

- [ ] **Step 1: Failing required test** — `required` renders a red `*` by the key always; the input goes red (`.invalid`) and a "This value is required" label shows ONLY when `invalid` is set (the parent sets it on a submit attempt with an empty override); a required field cannot switch override off (the switch is forced on / disabled off). Run -> FAIL.

- [ ] **Step 2: Required markup** — red `*` on the key when `required`; on `invalid`, the input border reds and the `reqsub` label renders. Required forces `overriding` and disables toggling off.

- [ ] **Step 3: Required test passes.**

- [ ] **Step 4: Commit.** `feat: FieldControl required marker and submit validation`

---

### Task 5: `$` source picker chrome + sourced display (drawn, inert)

**Files:** Modify `FieldControl.tsx`, `FieldControl.test.tsx`; reference the `kobalte` skill for the overlay.

- [ ] **Step 1: Failing test** — a source-capable field (a `sourceCapable?` prop, false this slice for real fields) renders a `$` trigger in the input's fixed right slot that opens a Kobalte popover menu listing Secret / Variable / File; the menu is inert (no handler wired); a field with a `source` renders symbol + name, not `$sec:`. Run -> FAIL.

- [ ] **Step 2: Picker markup** — the `$` join-item button + Kobalte `Popover` (portal, so it escapes the blade overflow, per the kobalte skill) with the three items. Inert placeholder handlers. No wrap on the `$`.

- [ ] **Step 3: Test passes.**

- [ ] **Step 4: Commit.** `feat: FieldControl source picker chrome (inert)`

---

### Task 6: Convert `EffectiveFields` to `FieldControl`

**Files:** Modify `web/src/components/EffectiveFields.tsx`, `web/src/components/EffectiveFields.test.tsx`

**Interfaces:** Consumes the Task 2-5 `FieldControl` contract. The batched flush from #282 (drafts store + `edit.onSave`) is preserved; the draft now carries the override state.

- [ ] **Step 1: Failing test** — the panel renders each field through `FieldControl`; toggling override on a row stages it; the blade Save flushes (upsert set, delete cleared) as before; a required field with an empty override blocks Save (sets `invalid` on that row) and does not flush. Run -> FAIL.

- [ ] **Step 2: Rewire** — replace the local presentational row with `FieldControl`. Map the drafts store: `overriding` = the field has a staged draft or `is_set`; `draft` = the staged value; toggling override off = stage a clear (delete on flush). On flush, if a required field's effective value is empty, set its `invalid` and throw to abort (the existing `edit.onSave` abort path). Contribute validity so the blade `valid()` blocks Save.

- [ ] **Step 3: Test passes; the #282 batched-flush tests still pass.**

- [ ] **Step 4: Commit.** `feat: render EffectiveFields through FieldControl`

---

### Task 7: Cascade ADR + docs + status

**Files:** Modify `docs/src/content/docs/architecture/decisions.md` (new ADR), `docs/src/content/docs/architecture/status.mdx` (build-progress entry), `docs/src/content/docs/guides/admin/fields.md` (the override model, required, the picker drawn).

- [ ] **Step 1: ADR** — a decision-log entry: "Field cascade and the type-default floor" stating the type default is the floor of the cascade (deepest-set-wins down product/location/system/component; fall to the type default when nothing is set), refs #291 for the multi-scope build.

- [ ] **Step 2: Guide + status** — update `fields.md` to the override model (switch, revert-off, bool, required, the drawn picker, symbol+name sources); add a `status.mdx` entry. Keep the `Partial` field badge.

- [ ] **Step 3: Commit.** `docs: field override model + cascade ADR`

---

### Task 8: Dev seed a bool and a required field

**Files:** Modify `internal/devseed/fixtures.yaml`, `internal/devseed/devseed.go` if needed

- [ ] **Step 1** — add a `bool` field (e.g. `wall_mounted` default true on the display type) and a `required` field to the seed so the screenshots render both states. Keep the seed idempotent (`make test` devseed green).

- [ ] **Step 2: Commit.** `chore: seed a bool and a required field`

---

### Task 9: Screenshots + ship-slice

- [ ] **Step 1** — `make test` green (Go + web). `make gen` clean.
- [ ] **Step 2** — `make docs-shots`; commit the refreshed field screenshots; embed the edit panel (all types incl. bool), a required-unfilled submit, and read mode with an override in the PR body.
- [ ] **Step 3** — run `/ship-slice`; address findings; open the PR (closes #290, #283).

## Self-Review

- Spec coverage: read/edit modes (T2/T3), override switch + revert-off (T3), bool (T3), required + submit validation (T4), `$` picker + sourced display drawn (T5), `field_definition.required` (T1), cascade-floor ADR (T7), EffectiveFields conversion (T6), seeds + screenshots (T8/T9). All spec sections mapped.
- Deferred and documented: the sources backend (picker inert), the multi-scope cascade (#291), the platform-wide adoption sweep (#292).
