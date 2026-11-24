---
title: Settings
description: "How settings are built: one typed Go struct declares every setting, layers merge into an effective document with per-key provenance, and only the console's binding is stored."
sidebar:
  order: 62
  badge:
    text: Partial
    variant: note
---

A **setting** configures the install itself (how long telemetry is kept, the console theme, the
identity provider), not the fleet. Fleet values, which resolve down locations, systems and
components, are [variables](/architecture/variables/). A setting resolves the same way a variable
does, most specific wins with provenance, along its own axis
([ADR-0035](/architecture/decisions/#adr-0035-settings-resolve-as-a-cascade-over-principals-with-a-broader-wins-lock)).
To change one, see [the admin Settings page](/admin/settings/).

## Declared once, as a typed struct

Every setting is a tagged field on one Go struct, `Settings` in `internal/settings/schema.go`, one
field per **namespace**
([ADR-0041](/architecture/decisions/#adr-0041-settings-are-a-reflected-typed-struct-with-generated-client-and-server-validation)):

```go
type Settings struct {
	UI        UISettings        `json:"ui" settings:"profile,client"`
	Retention RetentionSettings `json:"retention" settings:"platform,client"`
	SSO       SSOSettings       `json:"sso" settings:"platform"`
	// ...one field per namespace
}

type UISettings struct {
	Theme string `json:"theme" enum:"omniglass-dark,omniglass-light" default:"omniglass-dark" doc:"Console color theme"`
}
```

Reflection over the struct builds everything else, so there is no second place to drift:

- **the declared defaults**, from each field's `default` tag;
- **the namespace registry**, from the top-level fields and their `settings` tag;
- **the OpenAPI schema**, which Huma reflects, and from it the typed console client and the
  console's inline validation (`make gen` writes the field constraints to a committed artifact);
- **the server's write validation**, which reflects the same field: an unknown namespace is a 404,
  an unknown key, a wrong type or a value outside an `enum` or `pattern` a 422 naming the key.

A field with no `default` is optional: absent from the effective document until a level sets it, and
read in Go as "unset". A list field takes its default comma-separated and is replaced whole by a
write; a map field merges by key. A malformed tag is a boot panic, never a runtime branch. Adding a
setting is one field and `make gen`.

The layers merge as generic maps, since a Go struct cannot tell "unset" from a zero value, and the
effective document unmarshals into `Settings` at the edge, so Go code reads `s.Retention.MetricDays`
typed.

## Layers and provenance

An effective value resolves from ordered layers, the more specific winning:

1. **The declared default**: the struct's `default` tag, the fall-through, never a row.
2. **The settings file**: an optional JSON or YAML file at `OMNIGLASS_SETTINGS_FILE`, read at boot.
   The GitOps layer, a Kubernetes ConfigMap for instance.
3. **The environment**: `OMNIGLASS_SETTING__<NAMESPACE>__<KEY>=value`, one key a variable, coerced
   through the same declaration. A `profile` namespace is refused at boot, since per-user values do
   not live in a process's environment.
4. **The platform binding**: what an admin sets in the console or the API, stored in Postgres.

The merge is a deep merge in map space, so a key's **presence** decides: a key set to `false`
shadows, an absent key inherits. Every resolved key reports where it came from: the admin read
returns the effective document beside a `sources` map (each `namespace.key` to the winning layer, or
`default`) and a `locks` map, and the console badges each set key with its layer.

Only the binding is stored
([ADR-0033](/architecture/decisions/#adr-0033-settings-persist-only-the-override-level-base-layers-are-recomputed-in-memory)):
the defaults, the file and the environment are recomputed every boot, so restoring a namespace is
deleting its row and the file never drifts into a second authoritative copy. The one table is
`setting_binding`, unique on `(scope, user_id, namespace)`, with `scope` held by a CHECK to the
levels the resolver reads, today `platform` alone. It is never boot-seeded.

## Two domains: platform and profile

Each namespace's `settings` tag names its **domain** and whether the console may read it:

- **`platform`** namespaces are set only at the platform level and never cascade further:
  `retention`, `collection`, `label`, `notification`, `database`, `agent`, `assistant`, `heartbeat`
  and `sso`.
- **`profile`** namespaces describe how one person's console behaves and are shaped to cascade to
  user groups and users: `ui` and `keybindings`.

**Who writes and who reads are two questions.** The domain says how far a namespace cascades; the
`client` token says whether `GET /settings/me`, which any signed-in user reads, carries it.
`retention` and `label` are platform namespaces any user reads, since a chart says how far back its
history reaches and the console renders a label from the same acronym list the server did. `sso`,
`notification` and `agent` are admin-only to read. A caller learns how the agent policy applies to
them from `GET /auth/me/agent-policy`
([ADR-2063](/architecture/decisions/#adr-2063-an-install-sets-always-ask-or-never-per-class-of-operation-for-every-agent)).

## The API

- **`GET /settings`** (`settings:read`): every namespace, with `sources` and `locks`.
- **`GET /settings/me`** (any signed-in user): the client-visible namespaces, resolved, with no
  provenance. Apart from `/auth/me`, so a settings change never disturbs identity.
- **`PATCH /settings/{namespace}`**: an RFC 7386 JSON Merge Patch onto the namespace's platform
  binding, where `null` on a key restores it.
- **`DELETE /settings/{namespace}`** and **`POST /settings:restoreDefaults`**: drop one binding, or
  all of them.

Every write lands at the platform level by definition, so each takes `settings:update` **and**
`platform:update`, the install-wide authority a platform-tier variable needs
([install-wide authority is not fleet scope](/architecture/identity-access/#install-wide-authority-is-not-fleet-scope)).
A patch that names a secret (the mail relay, the assistant's key, the identity provider's client
secret) also takes `secret:bind` on it. Every write records an audit row in the same transaction.

The settings gateway methods take no fleet scope: settings describe the install, so the permission
alone gates them, a recorded carve-out from the scope invariant, not a missed one
([ADR-0034](/architecture/decisions/#adr-0034-the-settings-gateway-is-unscoped-only-the-permission-gates-it)).

From the CLI the merge patch is the whole body, since a namespace's keys are its own rather than
fields the OpenAPI declares:

```bash
omniglass setting update retention --body '{"metric_days": 30}'
omniglass setting update retention --body '{"metric_days": null}'
omniglass setting list
```

:::design[Per-key CLI flags, tracked in #1914]
`omniglass setting update` takes one flag per key, generated from the settings struct rather than the
OpenAPI document.
:::

:::design[The group and user levels, locks and the read-only file, tracked in #270]
- **Group and user levels.** A `profile` namespace takes a binding per user group and per user, so
  `user > group > platform > environment > file` and a person sets their own preferences on their
  profile, each user editing only their own row.
- **Locks.** An admin locks a key at a level, which pins its value and forbids any more specific level
  from shadowing it; a broader lock wins over a narrower one, so a user may edit a key only where no
  broader level locked it. The resolver already honors a stored lock and reports it; nothing writes one
  yet, and with one level there is nothing beneath it to lock.
- **The file as authority.** A generated JSONSchema validates the settings file at boot, a read-only
  mode makes the file win over the console for GitOps installs, and a signal reloads it without a
  restart.
:::
