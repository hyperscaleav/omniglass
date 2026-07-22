---
title: Settings
description: "A cascade-resolved, lockable settings engine: ordered layers merged into an effective document, with per-key provenance, top-down locks, and a platform-versus-profile domain split."
sidebar:
  badge:
    text: Partial
    variant: note
---

:::note[Partial]
Slice-0 ships the **platform** rung of the cascade end to end: the pure `settings` merge and resolve primitive, the single unscoped `setting_override` table, the Huma routes, the two `settings:<action>` permissions, the two seeded `profile`-domain namespaces (`ui`, `keybindings`), `ui.theme` wired through to re-theme the SPA, and the Admin settings page (namespace sections, provenance badges, lock chips, restore) ([ADR-0033](/architecture/decisions/#adr-0033-settings-persist-only-the-override-level-base-layers-are-recomputed-in-memory), [ADR-0034](/architecture/decisions/#adr-0034-the-settings-gateway-is-unscoped-only-the-permission-gates-it), [ADR-0035](/architecture/decisions/#adr-0035-settings-resolve-as-a-cascade-over-principals-with-a-broader-wins-lock)). Deferred to the fast-follow: the **group** and **user** override rungs and the Profile preferences tab, the `settings:lock` split for group-admins, `platform`-domain namespaces (`retention`, `integrations`) with their features, a GitOps read-only mode, and live file reload (SIGHUP) instead of restart-to-reload. Slice-1 makes a setting a reflected **typed struct** ([ADR-0041](/architecture/decisions/#adr-0041-settings-are-a-reflected-typed-struct-with-generated-client-and-server-validation)): one canonical `Settings` type is the single source for the default, the OpenAPI schema, the typed client, and validation, and both the console write path and the settings form now validate against that generated schema (the `defaults.yaml` and hand-kept namespace list are retired).
:::

Omniglass resolves a **setting** the same way it resolves a secret or a variable: down a cascade,
most-specific-wins, with provenance. The difference is the axis. The [estate cascade](/architecture/cascade/)
resolves down location to system to component; the settings engine resolves down the **principal** hierarchy,
platform to group to user. It is the same primitive (doctrine 5) pointed at identity instead of the estate, and
its least-specific level carries the same name for the same reason: `platform` is what an admin set for the
**whole install** ([ADR-0057](/architecture/decisions/#adr-0057-the-cascades-least-specific-tier-is-platform-and-a-default-is-not-a-tier)).

This generalizes the narrower "platform settings store" the [scaling](/architecture/scaling/) page sketched
(see [ADR-0033](/architecture/decisions/#adr-0033-settings-persist-only-the-override-level-base-layers-are-recomputed-in-memory)):
platform settings become one **domain** within the engine (set only at the platform level, admin-owned), and user
preferences become the other (settings that cascade to groups and users).

## Layers and levels

An effective value is resolved from ordered contributions, plus one thing that is not a level at all.

**`default` is off the axis.** It is the value reflected from the canonical `Settings` struct's `default:` tags
(see [the single-source struct](#the-single-source-struct)): the setting's own **declaration**, what it *is*
when nobody set it. It is never a row, nobody writes it, and it shadows nothing; every settable key has one, so
the effective document is always complete. It is the **fall-through**, not the bottom rung
([cascade](/architecture/cascade/#bindings-cascade-declarations-do-not)).

**The base layer** is recomputed into memory on every boot and never stored in the override table:

1. **`file`**: an operator settings file (`settings.json` or YAML) at a bootstrap-configured path, optional (a
   laptop run has none). This is the GitOps / Kubernetes ConfigMap layer; a change lands on pod restart.

**Override levels** are rows in Postgres, the identity cascade:

2. **`platform`**: the install-wide admin override. **Slice-0.**
3. **`group`**: per user-group override. **Fast-follow.**
4. **`user`**: per-user override. **Fast-follow.**

### Most-specific wins

Absent any lock, a more-specific level wins: `user > group > platform > file`. Where no level set the key at
all, the value is the setting's `default`, which provenance reports as a **declaration** (the console shows no
origin badge at all and names it "Declared default" in the layer stack) rather than as a level. Merge is a
**deep merge in JSON map-space**, so key **presence** decides an override, not a Go zero-value: a key set to
`false` overrides, a key absent inherits the layer below. A write is an RFC 7386 JSON Merge Patch, so `null` on
a key deletes it from that level's override (restoring it to the layer below, or to the declared default when
no level below set it).

## Locking: enforced from above

An admin **locks** a key at a level. A lock at level L pins L's contributed value and forbids any more-specific
level from overriding it: lock `ui.theme` at `platform` and no group or user can change it.

**Lock conflict: broader wins.** A `platform` lock supersedes a `group` lock; top-down admin authority is absolute.
The editability rule falls out of it: a principal may edit a key at level L if and only if no broader level has
locked it.

## Provenance

Every resolved key reports **where it came from** and its **lock state**. The admin read returns the effective
document plus a sibling `sources` map (`namespace.key` to the winning level, or `default` when no level set it)
and a `locks` map (`namespace.key` to the locking level). This reuses the estate cascade's effective-values
vocabulary (the winning level per key), extended from the estate bands to the principal levels plus a lock chip.
The Admin page badges a key that somebody set (`From settings file` / `Set in console`) and deliberately badges
nothing for a declared default, since nobody set it; a row expands to teach the full layer stack, where the
default reads as "Declared default" below `file` and `platform` (doctrine 4: the page teaches the cascade it
operates).

## Domains: platform versus profile

Each namespace carries a `domain` classifier. The `platform` domain is **named after the level**: a
platform-domain namespace is one only ever set at the `platform` level, never further down the principal
cascade.

- **`profile`**: cascades platform to group to user, **client-visible**, lockable, user-overridable in the
  fast-follow. `ui` and `keybindings` are the two seeded `profile` namespaces (`ui.theme` and `ui.default_landing`;
  the default keymap as data).
- **`platform`**: set only at the platform level, admin-only-read, does not cascade (for example `retention`,
  `integrations`). None is seeded in slice-0; the mechanism exists and is unit-tested, exercised when the first
  platform setting lands with its feature.

## Storage: one override table, unscoped

The declared defaults and the file layer live in memory, so Postgres holds **only the override levels**: a single
`setting_override(scope, principal_id, namespace, doc, locks, ...)` table with a
`unique nulls not distinct (scope, principal_id, namespace)` identity (a surrogate `id` is the primary key because
`principal_id` is nullable, and Postgres forbids NULL in a PK column). `scope` is under a CHECK naming the levels
that are actually persisted, today `platform` alone: the declared defaults and the file layer are recomputed in
memory and can never be rows, so a level the resolver would never read cannot be written, and a future rename of
the tier fails loudly at the database instead of orphaning every override in silence. Restore semantics fall out of the layer
model: **restore a namespace** is a `DELETE` of its row, **restore everything** truncates the scope, and the file
layer plus the declared defaults re-supply the values. The table is **never boot-seeded**: it is operator data,
and the seeding doctrine's "operator rows untouched" rule applies. Persisting only the override (not the file)
is a recorded call
([ADR-0033](/architecture/decisions/#adr-0033-settings-persist-only-the-override-level-base-layers-are-recomputed-in-memory)),
diverging from the scaling page's "materialized in Postgres" sketch.

### The unscoped-Gateway carve-out

The two-layer authorization model (a `<resource>:<action>` permission on every route, ABAC **scope** on every
applicable query) has one deliberate exception here. Settings Gateway methods are **unscoped**: platform and
cascade settings describe the platform and its principals, not the estate, so the ABAC storage-scope invariant is
**not applicable**, the same as the registry-type reads (`GET /types/...`). Only the `settings:<action>`
permission gates them. This is a recorded carve-out
([ADR-0034](/architecture/decisions/#adr-0034-the-settings-gateway-is-unscoped-only-the-permission-gates-it)),
not a missed invariant. The group and user levels will constrain override reads and writes by the acting principal
(a user edits only their own `user` row), a per-principal ownership check that is a different mechanism than
estate ABAC.

Every override write and delete writes an `audit_log` row in the same transaction (the existing `writeAuditRes`
pattern), so every settings edit carries change history.

## The single-source struct

A setting is declared **once**, as a tagged field on a canonical Go struct in
`internal/settings/schema.go`. That one declaration is the whole source of truth: reflection over the struct
builds the `default` layer and the namespace registry, Huma reflects the struct into the OpenAPI schema,
and the schema generates the typed SPA client and the write validator. There is no second place (no hand-kept
`defaults.yaml`, no hand-kept `Namespaces()` slice) to drift.

```go
// Settings is the canonical settings document: one field per namespace.
type Settings struct {
	UI          UISettings  `json:"ui"          settings:"profile,client"`
	Keybindings Keybindings `json:"keybindings" settings:"profile,client"`
}

// UISettings is the ui namespace. Adding a setting is one tagged field.
type UISettings struct {
	Theme          string `json:"theme" enum:"omniglass-dark,omniglass-light" default:"omniglass-dark" doc:"Console color theme"`
	DefaultLanding string `json:"default_landing" default:"/" doc:"Route the console opens to"`
}
```

Each namespace is a struct, a closed set of developer-defined keys. The `settings:"<domain>,<visibility>"` tag
carries the metadata: `domain` is `profile` or `platform`, and `client` marks a client-visible namespace fed to
`/settings/me`. A small reflect pass in the pure `settings` package produces two things from the tags, so the
tags are the only declaration:

- **`Defaults()`** walks each leaf's `default:` tag and coerces it to the field's Go kind (string, int, float,
  bool), building the `default` layer as a generic map. A field with no `default:` tag contributes no default.
  This replaces the retired embedded `defaults.yaml`.
- **`Namespaces()`** reflects the top-level fields: the `json` tag names the namespace, the `settings:` tag
  carries its `domain` and client-visibility. This replaces the hand-kept slice.

Reflection walks a compile-time type, so a malformed tag is a boot panic (a compile-time asset, like the old
embedded YAML), never a runtime branch.

### Typed at the edges, maps in the middle

The cascade merges **partial** layers (the file and the DB override each carry only the keys an operator set),
and a Go struct cannot express "unset" versus a zero value, so the layers stay generic maps and the merge
engine is unchanged. Typing lives only at the edges. The effective (fully-merged) document unmarshals into
`Settings`, so the API `values` field is the typed struct (the generated client reads `values.ui.theme` as the
enum union), and Go code calls `settingsSvc.EffectiveTyped(ctx)` and reads `s.UI.Theme` typed, anywhere in the
codebase. `sources` and `locks` stay flat maps keyed by `namespace.key`, since provenance is inherently
dynamic.

## Adding a setting

Everything about a setting lives on its struct field in `internal/settings/schema.go`. Add the field, run
`make gen`, and it is discovered everywhere. There is no registry to update, no `defaults.yaml`, no second
place.

**Add a key to an existing namespace.** Add one tagged field to the namespace's sub-struct. The tags are the
whole declaration:

```go
type UISettings struct {
	Theme          string `json:"theme" enum:"omniglass-dark,omniglass-light" default:"omniglass-dark" doc:"Console color theme"`
	DefaultLanding string `json:"default_landing" pattern:"^/" default:"/" doc:"Route the console opens to (an absolute path)"`
	// add a field here.
}
```

- `json:"<key>"` (**required**) is the setting's key: its name in the merge-patch, the API, and the client.
  Use snake_case. The key is the `json` tag, not the Go field name.
- `default:"<value>"` is the setting's **declared default**, coerced to the field's Go kind (string, int, float, bool).
  Omit for no default. Do not seed a default anywhere else.
- `enum:"a,b,c"` constrains the value to a set. It renders as a select in the console and is rejected
  (inline, and 422 on the server) otherwise.
- `pattern:"^regex$"` constrains a free-string value. A value that fails it is rejected inline and 422 on the
  server.
- `doc:"..."` is the human description, carried into the schema and the generated client.

**Add a namespace.** A namespace is a struct. Define the sub-struct, then add it as a field on `Settings`:

```go
type Settings struct {
	UI          UISettings         `json:"ui"          settings:"profile,client"`
	Keybindings Keybindings        `json:"keybindings" settings:"profile,client"`
	Retention   RetentionSettings  `json:"retention"   settings:"platform"` // new: platform-level only, admin-read
}
```

The `settings:"<domain>[,client]"` tag carries the namespace metadata:

- `domain` is `profile` (cascades to groups and users, user-overridable) or `platform` (set only at the platform level, admin).
- Add `client` to make the namespace's effective values readable at `/settings/me` (the SPA's boot read);
  omit it for admin-only-read (a `settings:read` gate).

**Then run `make gen`** and commit the drift. That one field now drives, with no further edits: the declared
default (`Defaults()`), the namespace registry (`Namespaces()`), the OpenAPI schema, the typed SPA client
(`values.<namespace>.<key>`), the server write-validator, the inline form validation
(`web/src/api/settings.schema.gen.ts`), and the typed Go accessor `settingsSvc.EffectiveTyped(ctx)`.

**Rules and gotchas.**

- Every namespace is a struct, a closed set of developer-defined keys; there is no operator-open namespace.
- A malformed tag is a boot panic (the struct is a compile-time asset), so a typo surfaces immediately, never
  as a silent runtime branch.
- Prefer `enum` or `pattern` over a bare string whenever the value is constrained: one tag buys the console
  picker, the inline validation, and the server 422 together.
- Never seed a default outside the tag (no `defaults.yaml`, no boot-seed `ON CONFLICT`); the `default:` tag is
  the declaration, and a second source is exactly the drift the single-source struct exists to prevent.

## Generated validation, one rule set from the struct

A write is validated against the **same reflected schema** on both sides, so the client and the server enforce
identical rules from the single Go source, with no hand-authored second copy.

- **Server (the backstop).** `PATCH /settings/{namespace}` validates the merge-patch before storing it. An
  unknown namespace in the path is a **404**; an unknown key, a wrong type, or an `enum` or `pattern` violation
  is a **422** naming the offending `namespace.key`. A `null` value is a delete and is always allowed. The
  validator reflects the namespace's sub-struct into a Huma schema and checks each non-null key against its
  field schema. This closes the slice-0 write-validation thin cut, where the PATCH accepted any namespace, key,
  or value and stored it as-is.
- **Client (caught before submit).** `make gen` gains a step that slices the settings field constraints (the
  per-field `type`, `enum`, `pattern`, `minLength`, and so on) out of the generated `api/openapi.json` into a
  committed artifact, `web/src/api/settings.schema.gen.ts`. It is diff-checked exactly like the other generated
  artifacts, so a struct-tag change reflows to the form with no hand edits. In edit mode each row validates its
  draft against that field's generated constraints and shows an inline error, an `enum` field renders as a
  select of the generated options (retiring the hard-coded theme list), and Save is blocked while a field is
  invalid. The server 422 remains the backstop for anything the client does not catch (a direct API call, a
  stale client) and maps back to the same field.

The generation chain is the Go struct to OpenAPI to `settings.schema.gen.ts` to inline form validation, one
rule set with the server 422 behind it.

## API surface

Two read audiences, two read endpoints, and merge-patch writes:

- **`GET /settings`** (admin, `settings:read`): the full effective document, all namespaces, **with provenance**
  (`sources` and `locks`). Feeds the Admin settings page.
- **`GET /settings/me`** (any authenticated user): the caller's resolved settings, **client-visible namespaces
  only, no provenance**. Feeds the SPA at boot (theme, landing, later keybindings). Parallel to `/auth/me`, and
  correct as the cascade grows (it is the caller's own effective cascade). Dedicated, not folded into `/auth/me`,
  so a settings change invalidates a settings cache without disturbing the identity cache.
- **`PATCH /settings/{namespace}`** (`settings:update` **and** `platform:update`): an RFC 7386 JSON Merge Patch onto
  the namespace's override at the acting scope (`platform` in slice-0); `null` on a key restores it.
- **`DELETE /settings/{namespace}`** (`settings:update` **and** `platform:update`): drop the override, restoring the
  whole namespace to defaults.
- **`POST /settings:restoreDefaults`** (`settings:update` **and** `platform:update`): an AIP custom method, a factory
  reset of the acting scope.

Every settings write lands at the **platform** tier by definition (a setting applies to the whole install, never to
part of the estate), so all three carry `platform:update` on top of `settings:update`, the same install-wide
authority a platform-tier variable, secret, or tag binding needs. The console gates its Edit and Restore controls on
**both**, and a principal holding only `settings:update` reads a note naming the missing capability rather than
meeting a 403 on Save.

Per doctrine 1 the effective document is a Huma struct, so the OpenAPI, the typed SPA client, the CLI command, and
the JSONSchema all generate from it (`make gen`). The `values` field is the typed `Settings` struct: the generated
client reads a known field like `values.ui.theme` as a union (slice-0 exposed `values` as a free-form object).
Because the declared defaults fill every key, the effective document is always fully populated; only the override
**storage** is raw JSONB partials.

The two resource permissions live on the admin role: `settings:read` (admin read with provenance) and
`settings:update` (write, restore, lock and unlock), paired on every write with `platform:update` (also admin and
owner). The store is a singleton, so there is no create or delete-of-resource permission; the client-safe values
reach ordinary users through `/settings/me`, which is authn-only, not `settings:read`.

## The cascade-over-principals model

Reusing the [cascade](/architecture/cascade/) primitive on the principal axis, rather than writing a second
resolver, is the deliberate call
([ADR-0035](/architecture/decisions/#adr-0035-settings-resolve-as-a-cascade-over-principals-with-a-broader-wins-lock)):
resolution, provenance, and the broader-wins lock are one mechanism the estate and the settings engine share. The
engine itself is a **pure `settings` package** (no I/O beyond reading the operator file): the deep merge, the
merge-patch, the cascade resolution, and the lock enforcement are the primary unit-test target, and the DB layer
is supplied by the caller (the Storage Gateway) through a narrow function seam, so the package never imports
storage.

## Slice-0 boundary

**In:** the platform level (file plus DB), the full cascade-shaped payload, the platform lock stored, shown, and
enforced. The pure engine, the override table, the Gateway methods, the API (read with provenance, client-safe
effective read, PATCH / DELETE / `:restoreDefaults`), the two permissions, the two seeded `profile` namespaces,
`ui.theme` wired end to end, and the Admin settings page.

**Fast-follow (not this slice):** the group and user override rungs and the Profile preferences tab (editable,
user-scoped Gateway reads), the `settings:lock` permission split for group-admins, `platform`-domain namespaces
(`retention`, `integrations`) with their features, a GitOps read-only mode (a setting that locks the page to
file-only editing), and live file reload (SIGHUP) instead of restart-to-reload.

## Slice-1 boundary

Slice-1 makes settings a reflected typed struct without touching the merge engine, the cascade precedence, the
permissions, or the routes ([ADR-0041](/architecture/decisions/#adr-0041-settings-are-a-reflected-typed-struct-with-generated-client-and-server-validation)).

**In:** the canonical `Settings` struct as the single source; reflected `Defaults()` and `Namespaces()` (the
embedded `defaults.yaml` and the hand-kept namespace slice retired); the typed effective read (`values` is
`Settings`, plus the `EffectiveTyped` app accessor); server write validation (404 unknown namespace, 422 bad
key / type / enum); and the generated client constraint artifact (`web/src/api/settings.schema.gen.ts`) driving
schema-derived inline form validation with Save blocked on an invalid field.

**Deferred (future slices, tracked on [#270](https://github.com/hyperscaleav/omniglass/issues/270)):** the
declarative operator-file machinery (a generated JSONSchema for the operator `settings.json`, validation of the
**file** layer at boot, and letting the file layer take precedence over the database, the GitOps-wins /
read-only lever); operator-open namespaces (a typed map with a `Default()` method); and the group and user
cascade rungs, all unchanged by slice-1.
