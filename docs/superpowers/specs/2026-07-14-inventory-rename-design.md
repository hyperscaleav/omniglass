# Inventory technical-name rename + inline name check

**Status:** design · 2026-07-14

## Problem

Component/system/location technical names (`name`) are unrenameable: the update
patch structs carry no `Name`, so the UI locks the field in edit mode. The data
model always supported it (`id uuid` is the identity; every FK, tag/variable/secret
binding, and placement references the UUID, never the name), so a rename is a
one-column change with no cascade. Expose it, with an advisory inline check so an
operator sees format and availability before saving.

## Decisions

- **Rename is a plain patch field**, not a special action. No cascade: FKs are UUID.
- **Name format comes into scope.** One slug rule, `^[a-z0-9][a-z0-9-]*$`, owned
  server-side and mirrored client-side, applied to both create (tightens it) and
  rename so the two surfaces agree.
- **Check is advisory preflight.** The check button is on-demand; Save stays enabled.
  The unique constraint (→ 409) is the real gate; a skipped-check collision surfaces
  in the existing `saveErr` alert.
- **`checkName` is collection-level and scope-blind.** `POST /<entity>:checkName`.
  Availability must be global to match the global unique constraint (a scope-aware
  check would false-positive on a name taken outside the caller's scope). This is a
  deliberate, minor info-leak (you learn a technical name is taken somewhere), gated
  behind `<entity>:update`.

## Design

### Storage (`internal/storage`)

- `ValidateEntityName(s string) error` (shared): the slug rule. Reused by create and
  by each `Update*`. Returns a sentinel (`ErrInvalidName`) mapped to 422.
- `SystemPatch` / `ComponentPatch` / `LocationPatch` gain `Name *string`. When
  non-nil, `Update*` validates it, then `update ... set name = $n`; the existing
  `name ... unique` constraint maps to the existing `Err*Exists` → 409.
- `NameTaken(ctx, name) (bool, error)` per entity: scope-blind `select exists(...)`.

### API (`internal/api`)

- `Name *string` on the three update inputs, `pattern` tag mirroring the validator
  (Huma rejects bad format at the edge; storage re-validates as the source of truth).
- `POST /<entity>:checkName`, body `{name}`, response `{valid bool, available bool,
  reason string}`, permission `<entity>:update`. `valid` = format; `available` =
  `!NameTaken`; `reason` = human string when either is false.
- `make gen` regenerates the typed client + CLI: `update` gains `--name`; a
  `check-name` command falls out of the custom method.

### UI (`web/src`)

- Edit mode unlocks the Technical-name input (drops the `disabled`).
- Inline **check button** mirrors the copy/reveal markup (`<button type="button">` +
  icon from `./icons` + `aria-label`/`title`) in the field's trailing slot. Click:
  client format check first (instant), then the `checkName` call; render inline status
  (green "Available" / red "Taken" / format hint).
- On a successful PATCH where the name changed, navigate to `/<entity>/<newname>`.
- `saveErr` (already present) catches a skipped-check collision on Save.

## Slice span + tests

API + CLI + UI. Systems is the validated reference; Components + Locations mirror it
(the create-as-route pattern). Tests:

- **Storage:** `ValidateEntityName` table test; rename round-trip (rename, assert the
  UUID and every binding/FK survive, old name now free, dup-name → `Err*Exists`).
- **API/CLI:** update with `--name` renames; `checkName` returns the three states;
  bad format → 422, dup → 409.
- **Web:** the inline check renders available / taken / bad-format; a rename navigates
  to the new slug; view mode stays read-only.

## Out of scope

- Old-name → id redirect/alias (bookmarked-URL soft break stays a 404). File separately
  if wanted.
- Wiring `checkName` into the create draft's inline validation (create keeps its
  current save-time validation; the shared validator makes adopting it later trivial).
- Renaming type-registry ids (`system_type.id` etc.), which are text PKs, not this.
