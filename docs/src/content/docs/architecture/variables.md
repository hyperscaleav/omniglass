---
title: "Config, secrets, and variables"
description: "Three kinds of operator-set value resolved by one cascade: config keyed to a signal, secrets encrypted at rest, and free variables."
sidebar:
  badge:
    text: Partial
    variant: note
---

:::note[Partial: the secret and variable members are built; config is Design]
Two members are built. The **`secret`** member ([ADR-0017](/architecture/decisions/#adr-0017-credential-is-renamed-secret-the-cascade-is-the-reuse-mechanism),
[#155](https://github.com/hyperscaleav/omniglass/issues/155)): the typed encrypted-at-rest cell owned on the
exclusive arc and resolved down the cascade, the `secret_type` shape registry, envelope AES-256-GCM crypto behind a
pluggable KEK provider, the masked-with-audited-decrypt read path, the two-axis visibility (placement scope plus the
per-secret `admin_sensitive` flag, with a scope-filtered directory, [ADR-0025](/architecture/decisions/#adr-0025-secret-is-a-sensitive-resource-a-per-secret-admin_sensitive-flag-flips-a-secret-to-the-admin-tier)),
and the operator surfaces. The **`variable`**
member ([#183](https://github.com/hyperscaleav/omniglass/issues/183)): the typed **plaintext** cell on the same
exclusive arc, resolved down the same cascade, with a Variables directory and a per-component effective-variables
panel. Each member's section below marks what is built versus deferred; the
[build progress](/architecture/status/#build-progress) note carries the shipped shape. A related primitive,
the **`field`** (slice 0), a typed schema declared on a `component_type` and resolved on a component to
set-or-default, also lands `Partial` (its macro interpolation and cross-type cascade are deferred); its
section is below. The **config** member stays `Design`, so this page is `Partial`. (`secret` was renamed
from `credential`; the ADR anchor keeps the old term.)
:::

Everything an operator **sets** resolves the same way: a typed value, owned at a scope, resolved
most-specific-wins down the [cascade](/architecture/cascade/) on every poll and every tick. Three
kinds share that resolution but differ in what they are keyed to and what lifecycle they carry:

- **config** (`Design`): a device setting you declare. Keyed by a **canonical signal** (a
  `datapoint_type`), so it has an observed side and can be reconciled.
- **secret** (**built**): an access secret, encrypted at rest. Its own `secret_type` shape registry,
  envelope crypto behind a pluggable KEK provider, resolved down the cascade and consumed by a `$sec:`
  token.
- **variable** (**built**): a free interpolated value (a macro). Not bound to a signal, just resolved
  down the cascade; the `$var:` splice into functions and interfaces is a later slice.

| | **config** | **secret** | **variable** (macro) |
|---|---|---|---|
| what it is | a declared device setting | an access secret, encrypted at rest | a free interpolated value |
| keyed by | a canonical signal (`datapoint_type`) | its own `secret_type` shape name | an org config key (cascade namespace) |
| has an observed side? | yes, a datapoint via a get function | its **validity**, not the secret value | no |
| lifecycle | drift → reconcile (a set function) | refresh + rotation + expiry (deferred) | none; resolved and interpolated |
| example | `video.input = HDMI1` | an `snmp-community`, a `basic-auth` | `poll_interval = 30s`, a base URL, a label |

The common thread is the cascade and an exclusive-arc scope (exactly one of
`global | template | location | system | component`): the same exclusive-arc ownership as
datapoints, plus a `template`-scoped default the datapoint arc lacks (and unlike datapoints,
config is not `node`-owned). The three are not three subsystems; they are three uses of one
"set a value, resolve it down a scope" idea.

## config: declared device state, keyed to a signal

A **config** item is the **declared side of a canonical signal**. `video.input` is one key with two
sides: the **observed** value the device reports (a `state_datapoint`, provenance=observed) and the
**declared** value you set. They share the **key** but not the **storage**: the declared value lives
in the config table, resolved down the cascade, and is **never a datapoint row**. Same name, opposite
direction, the observed side flowing *up* from the device and the declared side flowing *down* from
the operator. This is not a "declared provenance" (there are no declared rows in the datapoint
tables); it is one signal with two homes, and their gap is **drift**.

Keying config to the signal registry instead of a private name is what removes the import problem: a
component template **brings no keys, it references registered ones**, exactly as it does for the
datapoints it reads. Two display templates that both touch `video.input` are two references to one
governed key, not a collision. Config reuses the `datapoint_type`'s value domain, so a declared value
is validated against the same `{values: […]}` the observed side uses.

**The template is the source of truth for configurability.** A signal becomes settable on a device
class when that class's [component template](/architecture/templates/) binds a **get** function (an
ordinary collection function that emits the observed datapoint) and a **set** function (a
command-triggered function that writes it). The registry may carry a soft `settable` hint, but the
binding is authoritative: no set function, not enforceable here.

Each piece of a config item has one home, joined by the canonical key:

| Piece | What it holds | Lives in |
|---|---|---|
| signal definition | key, kind, value domain, unit | `datapoint_type` (the registry) |
| get / set binding | how this device class reads and writes the signal | the **component_template** version |
| declared value | the intent (`HDMI1`), plus the per-item `reconcile` policy | the **config table** (cascaded) |
| observed value | what the device reports (`HDMI2`) | `state_datapoint` rows (observed) |
| drift | declared ≠ observed | **computed on read**, not stored |

### Drift and reconcile

When a config item has both a declared and an observed value, their gap is **drift**: the same
[`disagree(declared, observed)`](/architecture/datapoints/#disagree-and-divergence) comparison used
everywhere, with the declared side sourced from config. A per-item `reconcile` policy turns drift
into action:

- **`observe`** (default): record the drift, raise **no** alarm. Log that it differs and go get the
  info; drift stays visible through [`disagree`](/architecture/datapoints/#disagree-and-divergence)
  and the config view, silently.
- **`warn`**: raise an alarm for the drift, at **warning** severity. Surface it, change nothing.
- **`enforce`**: declared wins. Call the template's **set** function to push the value back; that
  issues a command, writes an [`intended`](/architecture/datapoints/#intended-the-declared-effect-of-a-command)
  datapoint, and reconciles against the next observation (desired-state convergence, the controller
  half of spec-and-status). If the set **fails**, raise a real alarm (enforcement failure).

Adopting the observed value as the declared one (reality becomes intent) is **not** an ongoing mode;
it is a separate **one-shot import action** an operator runs deliberately.

:::caution[Open question]
The `reconcile: enforce` execution (the set-function push and the enforcement-failure alarm) and the
separate one-shot import action (observed-becomes-declared): the controller shape behind the reserved
seam.
:::

The power here is that **remediation needs no rule**. You do not author an `event_rule` or a flow to
fix a setting; you declare the value, set the policy to `enforce`, and the cascade plus drift plus
the set function close the loop. Reconcile runs **per item**, so one reconciled setting is better than
none; the capability of any item is simply which of its get/set functions the template has bound (get
only gives observe or warn on drift; a set too makes it enforceable). The data-mediated loop (set -> device ->
observe -> drift clears) is the one guarded at action dispatch
([alarms and actions](/architecture/alarms-actions/)), with a per-item backoff so a device that
refuses a write does not hammer.

### Declaring at the system level

Because config rides the standard cascade, you rarely declare on the device. Declare
`video.input = HDMI1` for the **main-display role** on the system template, and the cascade resolves it
onto whichever display fills that role; the display's own template declared nothing. Drift and
reconcile then *just happen*, no per-device authoring.

:::caution[Open question]
Resolving a value scoped to a role slot (the `system_template_member` where `health_role` already
lives) may need a new cascade level between system and component, alongside the per-item get/set
binding shape on the template.
:::

## secret: a typed, encrypted cascaded value

A **secret** is an access secret: a typed value, encrypted at rest, owned on the exclusive arc
(`global | location | system | component`) and resolved most-specific-wins down the cascade like config
and variables, but sealed so its value never sits in the clear. Its first slice is **built**
([ADR-0017](/architecture/decisions/#adr-0017-credential-is-renamed-secret-the-cascade-is-the-reuse-mechanism)):
the typed cell, the crypto, the cascade resolver, and the operator surfaces. A **sensitivity ladder** is
not a flag any value carries; a secret is its own primitive because it is stored encrypted and read back
only through a masked, audited path. (Within the secret primitive, a **binary** `admin_sensitive` flag
does split admin-only platform credentials from operator-visible device secrets, described below; that is
a visibility tier, not an arbitrary sensitivity level bolted onto every value.)

**Shape is a `secret_type` registry.** A secret has a structured **`secret_type`** shape: a per-field
list of `{name, type, secret, origin}`, so one field is secret (an `snmp-community` string, a password)
while another is plaintext (a username), and `origin` marks whether the operator sets a field or the
lifecycle fills it. The type also carries a **`default_admin_sensitive`** boolean that seeds the create
form's `admin_sensitive` default (see the two-axis visibility below): a device type (`snmp-community`,
`basic-auth`) defaults operational, a platform-integration type (`oauth2-client`) defaults
admin-sensitive. The ship-with types are `snmp-community`, `basic-auth`, and `oauth2-client`; an
`official` boolean marks shipped-canonical versus org-local, exactly as the datapoint and role
registries do.

**Envelope-encrypted at rest.** Crypto is **envelope AES-256-GCM** behind a pluggable **KEK provider**:
the key comes from the env (`OMNIGLASS_SECRET_KEY`), a file (`OMNIGLASS_SECRET_KEY_FILE`), or a warned
fallback under `OMNIGLASS_DATA_DIR`, with a KMS or Vault able to drop in behind the same seam and no
model change. Each secret field is sealed under a **per-value DEK wrapped by the KEK**, with `(owner,
name, field)` bound as **AAD**, so a ciphertext cannot be lifted from one row into another. A secret
field is stored as its `{ciphertext, nonce, wrapped_dek, key_id}` envelope; a non-secret field is
stored plaintext.

**Consumed at the site, by token.** A secret is not composed from references; the cascade is the reuse
mechanism (define once high, inherit below). Interpolation lives at the **consumption site**, a
**`$sec:name.path`** token in an interface input or a function arg, never inside a secret's own fields.
(The interpolation consumer that splices a live value into a request is the deferred collection-driver
slice.)

**Masked everywhere except an audited decrypt.** A secret's value is masked (`••••••`) in every read:
the directory, the per-component effective-secrets view, the type list. Reading the plaintext is a
separate, privileged action: `secret:reveal` gates the decrypt (both an on-screen **reveal** and a
clipboard **copy**, recorded under distinct `reveal` and `copy` verbs), and **every decrypt writes an
[audit](/architecture/audit/) row**.

**Two axes decide who reaches a secret** ([ADR-0025](/architecture/decisions/#adr-0025-secret-is-a-sensitive-resource-a-per-secret-admin_sensitive-flag-flips-a-secret-to-the-admin-tier)).
**Placement scope** (where the secret attaches on the exclusive arc) gives locality: a Singapore-scoped
field tech creates and reveals device secrets under Singapore and cannot reach one attached at global.
A per-secret **`admin_sensitive` flag** gives same-scope sensitivity: when set, every action on that
secret is lifted to the **`:admin` tier**, so a platform credential (a Zoom client secret) stays
admin/owner-only even at the same scope as an operational device secret an operator sees. `secret` is
also a **sensitive resource** kept off the bare `*:read` floor, so a `viewer` (only `*:read`) reads no
secrets at all, while an `operator`/`deploy` holds an explicit scoped `secret:read,reveal,create,update`
and works the operational secrets in its subtree. Concretely:

- The `/secrets` **directory is scope-filtered** (no longer all-scope-only) and **hides admin-sensitive
  rows** from a caller without the admin tier, so a platform credential's existence and field names never
  appear to an operator.
- **Reveal, update, and delete** of an admin-sensitive secret without the admin tier is a **non-disclosing
  404** (identical to an out-of-read-scope row), never a 403, so existence does not leak.
- **Creating** a secret marked `admin_sensitive` needs `secret:create:admin`, so an operator can mint only
  operational secrets (nor pick a type that defaults admin-sensitive).
- Sealing and editing an **operational** secret (`secret:create,update`) and revealing it
  (`secret:reveal`) are open to **operators** in scope; **delete** stays admin-and-owner. `admin` holds
  `secret:>` (reaching the `:admin` tier a two-token `secret:*` cannot); `owner`'s `>` covers everything.

**Lifecycle is a later slice.** The first slice is the typed encrypted cell and its cascade; the
lifecycle a plain config never carries (refresh, rotation, expiry) is **deferred**, each behavior a
template-declared use of functions, time, and flows rather than a new subsystem:

- **refresh** (an `oauth2` access token) is **lazy**: refreshed on use when within a skew window of
  expiry, coordinated across replicas by a NATS KV lock (CAS on the secret key). Idle secrets never
  refresh; the refreshed token is a separate encrypted cache, not an operator secret.
- **rotation** (a password on a schedule) is a **flow**: generate → set on the device (a set function)
  → update the store → verify → invalidate the old, driven by the [time](/architecture/time/) primitive.
- **expiry and reminders** are an expiry timestamp plus a **watchdog** that fires an event and an
  **alarm** before the secret lapses.

**Secret health is its validity, not its value.** A secret's observable is whether it still
works: **intrinsic expiry** (an `oauth2` token, a `tls_cert` `notAfter`) warns proactively, and
**observed-use failure** flips it unhealthy after N consecutive auth failures consumers report. Both
surface through the ordinary datapoint-to-alarm pipeline, so a secret gets a health story without
being a device signal.

**Shared versus per-device is just scope.** A fleet-wide SNMP community is a secret set high in the
cascade; a unique-per-device secret is the same shape set at component scope. No shared-versus-unique
split to model; it is the cascade, like everything else.

## variable: free interpolated values (macros)

:::note[Built: slice 1, plaintext cell + cascade]
The first slice ([#183](https://github.com/hyperscaleav/omniglass/issues/183)) is **built**: the typed plaintext
cell (a `value_type` of `string` / `int` / `float` / `bool` / `json`, its value stored as jsonb and validated
against the type in the app), owned on the exclusive arc and resolved down the cascade, with a Variables directory
and the per-component effective-variables panel. It **grants `variable:create,update` to operators** (delete stays
admin and owner), mirroring the secret member. Three parts of the design below are deferred: the **`template`**
owner scope (slice 1 mirrors the secret arc, `global | location | system | component`; template scope and cascade
groups are [#184](https://github.com/hyperscaleav/omniglass/issues/184)); a **`variable_type` registry** (slice 1
types inline with the `value_type` enum, no registry table, matching the "operator-defined, not curated" model); and
the **`$var:` consumer** and the **secret-flagged** variable. These divergences are logged in the
[decision log](/architecture/decisions/).
:::

A **variable** is the leftover, and the most familiar: a value you splice into behavior that is **not**
a device signal and carries no lifecycle, like a poll interval, a base URL, an environment label, or a
tuning constant. These are Zabbix-style **macros**, resolved `global → template → instance` down the
same cascade and interpolated as `$var:<name>` into functions, interface definitions, and rule
scopes.

- **Names are org-specific config keys, not canonical signals** (the one place the
  "operator-defined, not curated" namespace genuinely applies). There is no registry authority and no
  pre-registration; sprawl is controlled by a creation **role-gate** ([IAM](/architecture/identity-access/))
  and by every variable being **surfaced in the tree** as it is added.
- **Global and template-local are the same primitive at different scopes.** A global macro
  (a company-wide NTP server) and a template-local one (a device class's default poll interval) differ
  only by where on the cascade they sit.
- A variable has **no observed side and no reconcile**; nothing on a device mirrors a poll interval.
  That absence is exactly what separates it from config.

Scalar shapes (`string`, `int`, `float`, `bool`, `json`) cover the common case; a variable may be
flagged secret (a free secret like a webhook signing token) without being a full secret cell, since it
has no lifecycle.

## field: an operator-defined typed schema on a type

:::note[Partial: slice 0, schema on a `component_type` plus set-or-default on a component]
The first cut is **built**: an operator declares a typed **`field_definition`** on a `component_type` (a
`name`, a `data_type` of `string` / `int` / `float` / `bool` / `json`, and an optional type-level default
validated against the `data_type`), unique per `(component_type, name)`; every component of that type
carries it. A component sets a **literal** `field_value` for a field defined on its type, and the
component's **effective** value resolves to the set literal or the type default (an `is_set` flag marks the
override). The vertical is whole: storage (transactional, audited), the API (the definition catalog is flat
and `field:<action>`-gated, the value routes are ABAC-scoped to the owning component), the generated CLI and
typed client, and the UI (define on the component-type blade under [Types](/guides/admin/types/), plus an
**Effective fields** panel on the component detail that sets a literal and shows override-versus-default).
The rest of the design below is **deferred**, listed plainly so a built badge never hides drift.
:::

A **field** is an operator-defined **typed attribute declared on a type**, the schema layer over the value
cells above: where a variable or a secret is a single cascaded cell, a field is a named slot every instance
of a type carries, whose value the operator sets per instance. It revives the fixed-inventory idea (Zabbix's
`inventory_1..40` that never fit and were never custom) as a schema an operator defines per type, so "add a
`brightness` field to every `display` component" is one operation, not forty fixed slots.

Slice 0 stores **literals only** and resolves on the **component alone**. What the design intends, and this
slice does **not** yet do:

- **Macro interpolation of a field value** (the consumer of `$var:` / `$sec:` / `$datapoint:`). A field's
  eventual whole job is to be the consumer of the values above: a `field_value` will hold a `$var:` /
  `$sec:` / `$datapoint:` macro string as readily as a literal, resolved through the same interpolation
  engine at read. Slice 0 holds **literals only**; that consumer is deferred.
- **The cross-type cascade** (`product → location → system → component`, deepest-wins). Slice 0 resolves a
  field on the **component alone**, set-or-default with a single owner; there is no cascade across types yet.
- **The `sources` model**: the per-field allowed origins (`literal` / `variable` / `secret` / `datapoint` /
  `file`), the inline pickers they drive, and the override rules.
- **File typing** (a `file` `data_type` with accepted MIME types and formats), which would give an attached
  file a semantic role by the field it fills (a `floor_plan`, a `firmware`).
- **Multi-type definitions.** Slice 0 defines fields on `component_type` only; the `location_type` /
  `system_type` / `vendor` / `product` / `driver` schemas the exclusive-arc owner model allows come later.

One **known limitation** in the shipped UI: the Effective fields panel can set and re-set a value but
**cannot yet clear** it back to the type default. The effective read returns the field's id, not the
`field_value` id the clear needs, so the `DELETE` route exists on the API but is not wired into the panel.

## tag: a normalized label vocabulary

:::note[Built: slice 1, the registry, the bindings, and the cascade. See the [tags](/architecture/tags/) page.]
The tag primitive's first slice is **built** ([ADR-0021](/architecture/decisions/#adr-0021-tag-slice-1-a-governed-key-registry-with-entity-update-gated-bindings)):
the governed `tag` key registry, the per-entity `tag_binding` cell on the exclusive arc, and the union-on-key /
override-on-value cascade resolver. The [tags](/architecture/tags/) page is its home; this section is the conceptual
frame for how tags share the cascade with config, secrets, and variables.
:::

A **tag** is an operator **`key: value`** label attached to an entity to organize, filter, and scope by
dimensions Omniglass does not model natively (`category: audio-dsp`, `environment: prod`,
`cost_center: 4021`). A tag is not a signal and carries no lifecycle; it rides the cascade with a
**union-on-key, override-on-value** combinator, so keys accumulate down the tree while the
most-specific binding wins each value.

**The key is a tenant-wide governed vocabulary.** A tag **key** is a row in the `tag` registry, shared
across the whole tenant (one registry per database, which is the tenant boundary). Minting a new key is
**permissioned**: it takes a `tag:create` grant ([identity and access](/architecture/identity-access/)),
an admin or curator action. *Setting a value* on an existing key is the ordinary entity write
(`component:update` and friends), open to operators. That split is the point: the vocabulary stays
**normalized** (no one inventing `env` beside `environment` beside `Environment`) while binding values
stays routine. The UI **autocompletes keys from the registry** as you type, so you reach for the
existing key instead of coining a near-duplicate.

**Values bind down the cascade.** A `tag_binding` sets a value for a key at any scope
(`global | template | location | system | component`) and through [groups](/architecture/groups/),
exactly like config and variables. Keys **union** (an entity surfaces every tag bound at or above it);
values **override** most-specific-wins. A [template](/architecture/templates/) seeds default tags onto
its component (`category: audio-dsp`). Because resolution is cascaded, you tag a location once and every
system and component beneath it inherits it, which is what makes tags a practical scoping dimension: a
high-weight [group](/architecture/groups/) can key a rule-set off `compliance: pci`, an action can read
a `maintenance_window`.

:::caution[Open question]
Value-domain normalization. Key normalization is settled (the governed registry plus the `tag:create`
gate). The open part is the **value** side: whether a tag key may **constrain** its values (an enum or
`value_type` on the key, so `environment` accepts only its allowed set, validated and autocompleted like
a `datapoint_type` domain), and whether it may **normalize** them on input through an Expr transform
(lowercase, trim whitespace, fold synonyms) so `Prod`, `prod `, and `PROD` resolve to one value.
Free-text values ship either way; the question is how much governance a key places on its values.
:::

## What's shared

- **The cascade.** Config, secrets, and variables resolve most-specific-wins down
  `global → … → component`, with a template-scoped value as a shipped default; **tags** resolve down
  the same cascade with a union-on-key, override-on-value combinator. One resolver
  ([cascade](/architecture/cascade/)).
- **The exclusive-arc scope.** Each value is owned at exactly one scope: the same exclusive-arc
  ownership as datapoints, plus a `template`-scoped default the datapoint arc lacks (and config is
  not `node`-owned).
- **Typing.** A secret takes a structured `secret_type` shape registry (per-field secrecy and origin);
  a variable types inline against its `value_type` (a scalar, validated in the app, no registry); config
  instead borrows the `datapoint_type`'s domain, because its key *is* a signal.
- **Interpolation** renders variables (`$var:`) and secret fields (`$sec:`) into requests (a later
  consumer slice for both); config is read by key like a datapoint. Secrets are **masked** in every
  read and surface in the clear only through the audited reveal; a variable is plaintext.

The observed side of config is maintained by one **event-driven worker** (the one-worker-plus-stages
model): when a `state_datapoint` lands whose `(owner, key)` a config item is keyed to, it refreshes
that item's cached observed value, reverse-indexed so "is this datapoint a config's observed side?" is
a sargable lookup, not a scan. It is the one controlled, one-directional crossing from the timeseries
back into current-value config.

```d2
direction: right
classes: { node: { style.border-radius: 8 }; key: { style: { border-radius: 8; bold: true } } }
operator: operator { class: node }
declared: "config\ndeclared (spec)" { class: key }
device: device { class: node }
state: state_datapoint { class: node }
observed: "config\nobserved (status)" { class: key }
command: "command (intended)" { class: node }
operator -> declared: declares (cascade)
device -> state: observed (get fn)
state -> observed: observed-value worker
declared -- observed: "disagree = drift" { style.stroke-dash: 4 }
declared -> command: reconcile: enforce (set fn)
command -> device
```

## How this changes provenance

Modeling declared state as **config** (and access secrets as **secrets**) keeps **declared** out of the
datapoint provenances. Datapoints carry three ([observed, calculated,
intended](/architecture/datapoints/#provenance-how-we-know-a-value)); declared intent lives in config,
keyed to the same signal but stored down the cascade rather than as a row. The `state` **kind** is
unchanged: an observed `power.state = on` is still a `state_datapoint`, and a config item is keyed to
it. What moved is the *declared* value, out of the datapoint tables and into config resolved by the
cascade. There is no separate property or vault store; config, secrets, and variables are one
resolution model, and the spec-and-status loop gets a real home instead of overloading datapoint
provenance with operator intent.

## Storage

The shape registries, the value cells, and the operator-label tables; the physical layout (the owner
arc, the cascade key) lives on [storage](/architecture/storage/). The **secret** and **variable** cells
below are **built**; the **config** cell is `Design`.

The two shipped members each got their own table rather than a shared discriminated cell: **`secret`**
is its own `secret_type` + `secret` pair (it carries encryption and a masked read path), and
**`variable`** is a single table typed inline. Whether the remaining `config` cell joins `variable` in
one table with a discriminator or stays separate is still open; they share the cascade and the
exclusive-arc scope either way.

| Table | Key columns | Notes |
|---|---|---|
| `secret_type` | id, **official**, schema (per-field `{name, type, secret, origin}`) | **Built.** The secret **shape** registry (`snmp-community`, `basic-auth` seeded); `official` marks shipped-canonical versus org-local, like the datapoint and role registries |
| `secret` | (name, **owner arc**), secret_type, **`admin_sensitive`**, **value** (secret fields as `{ciphertext, nonce, wrapped_dek, key_id}` envelopes, non-secret fields plaintext) | **Built.** The encrypted cell and the `$sec:` cascade key; scope is the exclusive arc (global/location/system/component). Read masked through a **scope-filtered** directory; decrypted only through the audited `:reveal` / `:copy` path. Visibility is placement scope plus the per-secret `admin_sensitive` flag (admin-only when set); `secret` is off the `*:read` floor ([ADR-0025](/architecture/decisions/#adr-0025-secret-is-a-sensitive-resource-a-per-secret-admin_sensitive-flag-flips-a-secret-to-the-admin-tier)) |
| `variable` | (name, **owner arc**), **value_type** (`string`/`int`/`float`/`bool`/`json`), **value** (jsonb) | **Built.** The plaintext variable cell and the `$var:` cascade key; scope is the exclusive arc. Typed inline (no `variable_type` registry: the value is validated against `value_type` in the app), no observed side. The **config** cell (declared/observed/reconcile) is a separate, deferred member |
| `field_definition` | (**component_type**, name), **data_type** (`string`/`int`/`float`/`bool`/`json`), **default_value** (jsonb) | **Built (slice 0).** The typed **schema on a `component_type`**: flat and unscoped like the type registries, unique per `(component_type, name)`, with an optional type-level default validated against `data_type`. Multi-type owners (`location_type`/`system_type`/`vendor`/`product`/`driver`) are deferred |
| `field_value` | (**field**, **component**), **value** (jsonb) | **Built (slice 0).** A component's **literal** for a field on its type; the effective read is **set-or-default** (`is_set` marks the override), ABAC-scoped to the owning component. Macro-string values (`$var:`/`$sec:`/`$datapoint:`), the cross-type cascade, and clear-to-default in the UI are deferred |
| `tag` | name, applies_to, propagates | **Built.** The **tenant-wide governed key vocabulary**; minting a key needs `tag:create` ([identity and access](/architecture/identity-access/)). No `_type`, no namespace; values bind via `tag_binding`. See [tags](/architecture/tags/) |
| `tag_binding` | (tag, **owner arc**), value | **Built.** The `key: value` binding: **union on key, override on value** down the [cascade](/architecture/cascade/), owned on the exclusive arc (`global / location / system / component`); setting a value is the owner's own `update` write. Binding via groups and a `template` default are deferred |

