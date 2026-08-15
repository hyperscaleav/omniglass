---
title: Settings
description: "A cascade-resolved, lockable settings engine: ordered layers merged into an effective document, with per-key provenance, top-down locks, and a platform-versus-profile domain split."
sidebar:
  badge:
    text: Partial
    variant: note
---

:::note[Partial]
Slice-0 ships the **platform** rung end to end: the pure `settings` merge and resolve primitive, the single unscoped `setting_override` table, the Huma routes, the two `settings:<action>` permissions, the two seeded `profile`-domain namespaces (`ui`, `keybindings`), `ui.theme` wired through to re-theme the SPA, and the Admin settings page ([ADR-0033](/architecture/decisions/#adr-0033-settings-persist-only-the-override-level-base-layers-are-recomputed-in-memory), [ADR-0034](/architecture/decisions/#adr-0034-the-settings-gateway-is-unscoped-only-the-permission-gates-it), [ADR-0035](/architecture/decisions/#adr-0035-settings-resolve-as-a-cascade-over-principals-with-a-broader-wins-lock)). The deferred fast-follow list is the design fence under [Slice-0 boundary](#slice-0-boundary). Slice-1 makes a setting a reflected **typed struct** ([ADR-0041](/architecture/decisions/#adr-0041-settings-are-a-reflected-typed-struct-with-generated-client-and-server-validation), [below](#slice-1-boundary)), retiring `defaults.yaml` and the hand-kept namespace list.
:::

Omniglass resolves a **setting** the same way it resolves a secret or a variable: down a cascade,
most-specific-wins, with provenance, but on the **principal** axis (platform to group to user)
rather than the [fleet cascade](/architecture/cascade/)'s location to system to component. Same
primitive (doctrine 5) pointed at identity; the least-specific level is `platform`, an admin's
**install-wide** value
([ADR-0057](/architecture/decisions/#adr-0057-the-cascades-least-specific-tier-is-platform-and-a-default-is-not-a-tier)).
It generalizes the "platform settings store" the [scaling](/architecture/scaling/) page sketched
([ADR-0033](/architecture/decisions/#adr-0033-settings-persist-only-the-override-level-base-layers-are-recomputed-in-memory)):
platform settings become one **domain** within the engine, user preferences the other.

## Layers and levels

An effective value resolves from ordered contributions, plus one thing that is not a level.

**`default` is off the axis.** It is the value reflected from the canonical `Settings` struct's
`default:` tags (see [the single-source struct](#the-single-source-struct)): the setting's own
**declaration**, never a row, shadowing nothing; every settable key has one, so the effective
document is always complete. It is the **fall-through**, not the bottom rung
([cascade](/architecture/cascade/#bindings-cascade-declarations-do-not)).

**The base layer** is recomputed into memory on every boot and never stored in the override table:

1. **`file`**: an operator settings file (`settings.json` or YAML) at a bootstrap-configured path,
   optional. The GitOps / Kubernetes ConfigMap layer; a change lands on pod restart.

**Override levels** are rows in Postgres, the identity cascade:

2. **`platform`**: the install-wide admin override. **Slice-0.**

:::design[The group and user override rungs, tracked in #270]
3. **`group`**: per user-group override. **Fast-follow.**
4. **`user`**: per-user override. **Fast-follow.**
:::

### Most-specific wins

Absent any lock, a more-specific level wins: `user > group > platform > file`. Where no level set
the key, the value is the setting's `default`, which provenance reports as a **declaration**, not a
level. Merge is a **deep merge in JSON map-space**, so key **presence** decides an override, not a
Go zero-value: a key set to `false` overrides, an absent key inherits. A write is an RFC 7386 JSON
Merge Patch, so `null` on a key deletes it from that level's override, restoring the layer below or
the declared default.

## Locking: enforced from above

An admin **locks** a key at a level. A lock at level L pins L's contributed value and forbids any
more-specific level from overriding it: lock `ui.theme` at `platform` and no group or user can
change it.

**Lock conflict: broader wins.** A `platform` lock supersedes a `group` lock. The editability rule
falls out: a principal may edit a key at level L if and only if no broader level has locked it.

## Provenance

Every resolved key reports **where it came from** and its **lock state**: the admin read returns the
effective document plus a sibling `sources` map (`namespace.key` to the winning level, or `default`)
and a `locks` map (`namespace.key` to the locking level). The Admin page badges a set key
(`From settings file` / `Set in console`) and badges nothing for a declared default ("Declared
default" in the layer stack); a row expands to teach the full layer stack (doctrine 4).

## Domains: platform versus profile

Each namespace carries a `domain` classifier. The `platform` domain is **named after the level**:
set only at the `platform` level, never further down.

- **`profile`**: cascades platform to group to user, **client-visible**, lockable, user-overridable
  in the fast-follow. `ui` and `keybindings` are the two seeded `profile` namespaces (`ui.theme` and
  `ui.default_landing`; the default keymap as data).
- **`platform`**: platform-level only, does not cascade (for example `retention`, `integrations`).
  `label` is the first, holding the acronym dictionary the [label rule](/architecture/core-entities/)
  engine's `title` consults.

**The domain and client-visibility are two questions, not one.** The domain says how far a setting
cascades and therefore who WRITES it; `client` says who may READ the effective value. A
platform-domain namespace is admin-write by definition, and `label` is `platform,client` because the
console renders a label from the same dictionary the server did, which an ordinary viewer has to be
able to read. Omitting `client` on a platform namespace makes it admin-only-read as well
(`retention`, `integrations`).

## Storage: one override table, unscoped

The declared defaults and the file layer live in memory, so Postgres holds **only the override
levels**: a single `setting_override(scope, principal_id, namespace, doc, locks, ...)` table with a
`unique nulls not distinct (scope, principal_id, namespace)` identity (a surrogate `id` primary
key; `principal_id` is nullable, and Postgres forbids NULL in a PK column). `scope` is under a
CHECK naming the persisted levels, today `platform` alone, so a level the resolver would
never read cannot be written and a future tier rename fails loudly. Restore falls out of the layer
model: **restore a namespace** is a `DELETE` of its row, **restore everything** truncates the
scope, and the file layer plus declared defaults re-supply the values. The table is **never
boot-seeded** (operator data). Persisting only the override is a recorded call
([ADR-0033](/architecture/decisions/#adr-0033-settings-persist-only-the-override-level-base-layers-are-recomputed-in-memory)),
diverging from the scaling page's "materialized in Postgres" sketch.

### The unscoped-Gateway carve-out

The two-layer authorization model has one exception here: Settings Gateway methods are
**unscoped**. Settings describe the platform and its principals, not the fleet, so the ABAC
storage-scope invariant is **not applicable** (as with the registry-type reads, `GET /property-types` and its catalog siblings);
only the `settings:<action>` permission gates them, a recorded carve-out
([ADR-0034](/architecture/decisions/#adr-0034-the-settings-gateway-is-unscoped-only-the-permission-gates-it)),
not a missed invariant. The group and user levels will constrain override reads and writes by the
acting principal (a user edits only their own `user` row), a per-principal ownership check distinct
from fleet ABAC. Every override write and delete writes an `audit_log` row in the same transaction
(the existing `writeAuditRes` pattern).

## The single-source struct

A setting is declared **once**, as a tagged field on a canonical Go struct in
`internal/settings/schema.go`. Reflection over the struct builds the `default` layer and the
namespace registry, Huma reflects it into the OpenAPI schema, and the schema generates the typed SPA
client and the write validator. There is no second place to drift.

```go
// Settings is the canonical settings document: one field per namespace.
type Settings struct {
	UI          UISettings    `json:"ui"          settings:"profile,client"`
	Keybindings Keybindings   `json:"keybindings" settings:"profile,client"`
	Label       LabelSettings `json:"label"       settings:"platform,client"`
}

// UISettings is the ui namespace. Adding a setting is one tagged field.
type UISettings struct {
	Theme          string `json:"theme" enum:"omniglass-dark,omniglass-light" default:"omniglass-dark" doc:"Console color theme"`
	DefaultLanding string `json:"default_landing" default:"/" doc:"Route the console opens to"`
}
```

Each namespace is a struct, a closed set of developer-defined keys. The
`settings:"<domain>,<visibility>"` tag carries the metadata: `domain` is `profile` or `platform`
([above](#domains-platform-versus-profile)), and `client` marks a namespace fed to `/settings/me`
(omit for admin-only-read). A reflect pass in the pure `settings`
package produces both artifacts from the tags: **`Defaults()`** walks each leaf's `default:` tag,
coerced to the field's Go type (replacing the retired embedded `defaults.yaml`; no tag, no default),
and **`Namespaces()`** reflects the top-level fields (replacing the hand-kept slice). Reflection
walks a compile-time type, so a malformed tag is a boot panic, never a runtime branch.

### Typed at the edges, maps in the middle

The cascade merges **partial** layers, and a Go struct cannot express "unset" versus a zero value,
so the layers stay generic maps and the merge engine is unchanged; typing lives at the edges. The
effective (fully-merged) document unmarshals into `Settings`, so the API `values` field is the
typed struct (the generated client reads `values.ui.theme` as the enum union) and Go code calls
`settingsSvc.EffectiveTyped(ctx)` and reads `s.UI.Theme` typed; `sources` and `locks` stay flat
maps keyed by `namespace.key`, since provenance is dynamic.

## Adding a setting

Everything about a setting lives on its struct field in `internal/settings/schema.go`: add the
field, run `make gen`, and it is discovered everywhere.

**Add a key to an existing namespace.** Add one tagged field to the namespace's sub-struct:

```go
type UISettings struct {
	Theme          string `json:"theme" enum:"omniglass-dark,omniglass-light" default:"omniglass-dark" doc:"Console color theme"`
	DefaultLanding string `json:"default_landing" pattern:"^/" default:"/" doc:"Route the console opens to (an absolute path)"`
	// add a field here.
}
```

- `json:"<key>"` (**required**): the setting's key in the merge-patch, the API, and the client
  (snake_case); the key is the `json` tag, not the Go field name.
- `default:"<value>"`: the **declared default**, coerced to the field's Go kind; omit for none.
- `enum:"a,b,c"`: constrains the value to a set (a console select, rejected inline and 422 on the
  server otherwise); `pattern:"^regex$"` likewise for a free string.
- `doc:"..."`: the human description, carried into the schema and the client.

**A list-valued setting** is a `[]string` (or any slice) field, and its `default` tag is the
**comma-separated** form, `default:"AV,DSP,HDMI"`. That spelling is not a local convention: Huma
reflects the same tag into the JSON Schema and splits a string array on commas, so following it
exactly is what keeps one tag from producing two disagreeing defaults. The JSON-array spelling Huma
also accepts (`default:"[\"AV\"]"`) is refused here rather than parsed a second way, and a bad tag is
a boot panic like any other. Three consequences worth knowing before declaring one:

- **A write replaces the whole list**, since merge is a deep merge of maps and a non-map value
  overrides wholesale. There is no "add one entry" patch, which is also the only way an entry the
  platform ships can be removed.
- **The console edits it as one comma-separated line** and sends an array, translating both ways from
  the generated `"type": "array"` constraint; a blank line is an empty list and a doubled comma is an
  inline error, since a blank entry would be stored and match nothing.
- **Declare it `nullable:"false"`.** Huma makes arrays nullable by default, and `null` is the merge
  patch's delete sentinel, so a nullable list invites a write that means something else entirely.

**Add a namespace.** Define the sub-struct, then add it as a field on `Settings`:

```go
type Settings struct {
	UI          UISettings         `json:"ui"          settings:"profile,client"`
	Keybindings Keybindings        `json:"keybindings" settings:"profile,client"`
	Retention   RetentionSettings  `json:"retention"   settings:"platform"` // new: platform-level only, admin-read
}
```

**Then run `make gen`** and commit the drift: that one field now drives every artifact above, plus
the inline form validation and the typed Go accessor (`values.<namespace>.<key>`), with no further
edits.

**Rules and gotchas.** No operator-open namespace; prefer `enum` or `pattern` over a bare string,
since one tag buys the console picker, the inline validation, and the server 422 together; and never
seed a default outside the tag (no `defaults.yaml`, no boot-seed `ON CONFLICT`), a second source
being exactly the drift the single-source struct prevents.

## Generated validation, one rule set from the struct

A write is validated against the **same reflected schema** on both sides, no hand-authored second
copy.

- **Server (the backstop).** `PATCH /settings/{namespace}` validates the merge-patch before storing:
  an unknown namespace in the path is a **404**; an unknown key, wrong type, or `enum` / `pattern`
  violation is a **422** naming the offending `namespace.key`; a `null` value is a delete, always
  allowed. The validator reflects the namespace's sub-struct into a Huma schema (closing the slice-0
  thin cut).
- **Client (caught before submit).** `make gen` slices the settings field constraints out of the
  generated `api/openapi.json` into a committed artifact, `web/src/api/settings.schema.gen.ts`,
  diff-checked like the other generated artifacts. Each row validates its draft inline, an `enum`
  field renders as a select of the generated options (retiring the hard-coded theme list), and Save
  is blocked while a field is invalid; the server 422 remains the backstop and maps back to the same
  field.

## API surface

Two read audiences, two read endpoints, and merge-patch writes:

- **`GET /settings`** (admin, `settings:read`): the full effective document, all namespaces, **with
  provenance** (`sources` and `locks`). Feeds the Admin settings page.
- **`GET /settings/me`** (any authenticated user): the caller's resolved settings, **client-visible
  namespaces only, no provenance**. Feeds the SPA at boot; dedicated (not folded into `/auth/me`) so
  a settings change never disturbs the identity cache, and correct as the cascade grows.
- **`PATCH /settings/{namespace}`** (`settings:update` **and** `platform:update`): an RFC 7386 JSON
  Merge Patch onto the namespace's override at the acting scope (`platform` in slice-0); `null`
  restores a key.
- **`DELETE /settings/{namespace}`** (same gates): drop the override, restoring the namespace to
  defaults.
- **`POST /settings:restoreDefaults`** (same gates): an AIP custom method, a factory reset of the
  acting scope.

Every settings write lands at the **platform** tier by definition, so all three writes carry
`platform:update` on top of `settings:update`, the same install-wide authority a platform-tier
variable, secret, or tag binding needs. The console gates Edit and Restore on **both**; a principal
holding only `settings:update` reads a note naming the missing capability rather than a 403 on
Save.

Per doctrine 1 the effective document is a Huma struct, so the OpenAPI, the typed SPA client, the
CLI command, and the JSONSchema all generate from it (`make gen`); only the override **storage** is
raw JSONB partials.

`settings:read` and `settings:update` (write, restore, lock and unlock) live on the admin role. The
store is a singleton, so there is no create or delete-of-resource permission.

## The cascade-over-principals model

Reusing the [cascade](/architecture/cascade/) primitive on the principal axis is the deliberate call
([ADR-0035](/architecture/decisions/#adr-0035-settings-resolve-as-a-cascade-over-principals-with-a-broader-wins-lock)):
resolution, provenance, and the broader-wins lock are one shared mechanism. The engine is a **pure
`settings` package** (no I/O beyond reading the operator file): the deep merge, merge-patch,
resolution, and lock enforcement are the primary unit-test target, the DB layer supplied by the
caller (the Storage Gateway) through a narrow function seam.

## Slice-0 boundary

**In:** the platform level (file plus DB), the full cascade-shaped payload, and the platform lock
stored, shown, and enforced, end to end through the pure engine, the override table, the Gateway,
the API, the two permissions, the two seeded `profile` namespaces, and the Admin settings page.

:::design[The fast-follow rungs and platform-domain namespaces, tracked in #270]
**Fast-follow (not this slice):** the group and user override rungs and the Profile preferences tab
(editable, user-scoped Gateway reads), the `settings:lock` permission split for group-admins, the
remaining `platform`-domain namespaces (`retention`, `integrations`) with their features (`label`
has since landed with the acronym dictionary), a GitOps read-only mode, and live file reload (SIGHUP)
instead of restart-to-reload.
:::

## Slice-1 boundary

Slice-1 makes settings a reflected typed struct without touching the merge engine, the cascade
precedence, the permissions, or the routes
([ADR-0041](/architecture/decisions/#adr-0041-settings-are-a-reflected-typed-struct-with-generated-client-and-server-validation)):
the single-source struct, the typed effective read, and the generated validation described above.

:::design[The declarative operator-file machinery and the cascade rungs, tracked in #270]
**Deferred (future slices, tracked on [#270](https://github.com/hyperscaleav/omniglass/issues/270)):**
the declarative operator-file machinery (a generated JSONSchema for the operator `settings.json`,
boot validation of the **file** layer, and the file-wins / read-only GitOps lever); operator-open
namespaces (a typed map with a `Default()` method); and the group and user cascade rungs, all
unchanged by slice-1.
:::
