---
title: "Config, secrets, and variables"
description: "Three kinds of operator-set value resolved by one cascade: config keyed to a signal, secrets encrypted at rest, and free variables."
sidebar:
  badge:
    text: Partial
    variant: note
---

:::note[Partial: the secret member is built; config and variable are Design]
The **`secret`** member is built ([ADR-0017](/architecture/decisions/#adr-0017-credential-is-renamed-secret-the-cascade-is-the-reuse-mechanism),
[#155](https://github.com/hyperscaleav/omniglass/issues/155)): the typed encrypted-at-rest cell owned on the
exclusive arc and resolved down the cascade, the `secret_type` shape registry, envelope AES-256-GCM crypto behind a
pluggable KEK provider, the masked-with-audited-decrypt read path, and the operator surfaces (a Secrets directory
and the per-component effective-secrets cascade panel). Its own section below marks what is built versus deferred;
the [build progress](/architecture/status/#build-progress) note carries the shipped shape. The **config** and
**variable** members stay `Design`, so this page is `Partial`. (`secret` was renamed from `credential`; the ADR
anchor keeps the old term.)
:::

Everything an operator **sets** resolves the same way: a typed value, owned at a scope, resolved
most-specific-wins down the [cascade](/architecture/cascade/) on every poll and every tick. Three
kinds share that resolution but differ in what they are keyed to and what lifecycle they carry:

- **config** (`Design`): a device setting you declare. Keyed by a **canonical signal** (a
  `datapoint_type`), so it has an observed side and can be reconciled.
- **secret** (**built**): an access secret, encrypted at rest. Its own `secret_type` shape registry,
  envelope crypto behind a pluggable KEK provider, resolved down the cascade and consumed by a `$sec:`
  token.
- **variable** (`Design`): a free interpolated value (a macro). Not bound to a signal, just resolved
  and spliced into functions and interfaces.

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
the typed cell, the crypto, the cascade resolver, and the operator surfaces. Sensitivity is not a flag
any value carries; a secret is its own primitive because it is stored encrypted and read back only
through a masked, audited path.

**Shape is a `secret_type` registry.** A secret has a structured **`secret_type`** shape: a per-field
list of `{name, type, secret, origin}`, so one field is secret (an `snmp-community` string, a password)
while another is plaintext (a username), and `origin` marks whether the operator sets a field or the
lifecycle fills it. The ship-with types are `snmp-community` and `basic-auth`; an `official` boolean
marks shipped-canonical versus org-local, exactly as the datapoint and role registries do.

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
clipboard **copy**, recorded under distinct `reveal` and `copy` verbs), it is **not** covered by the
`*:read` floor (so only admin, `secret:*`, and owner may decrypt), and **every decrypt writes an
[audit](/architecture/audit/) row**. Sealing and editing a secret (`secret:create,update`) is open to
**operators**; delete stays admin-and-owner.

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

## tag: a normalized label vocabulary

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
- **Shape registries** back secrets (`secret_type`, structured with per-field secrecy and origin) and
  variables (`variable_type`, scalar); config instead borrows the `datapoint_type`'s domain, because
  its key *is* a signal.
- **Interpolation** renders variables (`$var:`) and secret fields (`$sec:`) into requests; config is
  read by key like a datapoint. Secrets are **masked** in every read and surface in the clear only
  through the audited reveal, never in a log line, error string, or datapoint label.

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
arc, the cascade key) lives on [storage](/architecture/storage/). The **secret** tables below are
**built**; the **config** and **variable** cells are `Design`.

The shipped secret answered the "one table or three" question for its own member: **`secret` is its
own pair of tables** (`secret_type` + `secret`), not a discriminated row in a shared cell, because it
carries encryption and a masked read path a plain value does not. Whether the remaining `config` and
`variable` cells share one table with a discriminator or stay separate is still open; they share the
cascade and the exclusive-arc scope either way.

| Table | Key columns | Notes |
|---|---|---|
| `secret_type` | id, **official**, schema (per-field `{name, type, secret, origin}`) | **Built.** The secret **shape** registry (`snmp-community`, `basic-auth` seeded); `official` marks shipped-canonical versus org-local, like the datapoint and role registries |
| `secret` | (name, **owner arc**), secret_type, **value** (secret fields as `{ciphertext, nonce, wrapped_dek, key_id}` envelopes, non-secret fields plaintext) | **Built.** The encrypted cell and the `$sec:` cascade key; scope is the exclusive arc (global/location/system/component). Read masked; decrypted only through the audited `:reveal` / `:copy` path |
| `variable_type` | name, schema (scalar or fields + **per-field secret**), validation, **official** | `Design`. The variable **shape** registry (a scalar `string`/`int`/`float`/`bool`/`json`); `official` marks shipped-canonical versus org-local |
| `variable` | (name, **owner arc**), type, **declared_value**, **linked_state** (-> state_datapoint, nullable), **observed_value**, reconcile | `Design`. The config/variable cell and the `$var:` cascade key; scope is the exclusive arc. Holds declared intent, optionally mirrors an observed datapoint for drift |
| `tag` | name, applies_to, propagates | the **tenant-wide governed key vocabulary**; minting a key needs `tag:create` ([identity and access](/architecture/identity-access/)). No `_type`, no namespace; values bind via `tag_binding` |
| `tag_binding` | (scope_kind, scope_id, tag), value | the `key: value` binding: **union on key, override on value** down the [cascade](/architecture/cascade/), bindable at any scope and via groups |

