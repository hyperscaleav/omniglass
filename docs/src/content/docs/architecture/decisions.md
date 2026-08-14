---
title: Decision log
description: "The dated history of architectural calls: reversals, settled open questions, and where the build currently diverges from the present-tense design."
---

The architecture pages are written in the present tense as the **target design**, and each carries a
status badge that says how much of it is built ([implementation status](/architecture/status/)). Neither
axis carries **history**: why a call was made, when it was reversed, or why the shipped code differs from
the page that describes it. That is what this log is for.

A page tells you what the design **is** and how much is built. This log tells you how it **got there**:
the decisions that bind the design, the ones that were reversed in the open, and the points where the
implementation has deliberately (or accidentally) drifted from the prose. It is the project's
architecture decision record (ADR), kept lightweight and append-only.

## How it works

- **One entry per decision** that reverses a prior call, settles an [open question](/architecture/status/),
  or records a point where the build diverges from a page's present-tense design.
- Each entry carries a **date**, a **status** (`Proposed`, `Accepted`, `Resolved`, or `Superseded`), the **decision**
  in one line, the **context** that forced it, and the **page(s)** it touches.
- A **divergence** entry is the partner of a page's inline note: the page says what is true *now*, this
  log says *why* and *when* it diverged, and which issue tracks closing the gap.
- Entries are **never edited away**. A reversed decision is marked `Superseded` and points at the entry
  that replaced it, so the trail of reasoning survives. Nothing in this log is deleted when a page
  changes.
- New reversals and divergences are added **per slice**, as part of the
  [ship gate](/contributing/slice-workflow/): if a slice changes a settled call or ships something that
  differs from its architecture page, the entry lands in the same PR.

This log was seeded on 2026-06-30 from the first architecture-drift review, which backfilled the entries
below from the project's history. From here it grows one slice at a time.

## Index

| ID | Date | Status | Decision |
|---|---|---|---|
| [ADR-0001](#adr-0001-ai-acts-as-a-user-the-agent-principal-is-deferred) | 2026-06-27 | Accepted | AI acts as a `human` / `service` principal; a first-class `agent` principal is deferred |
| [ADR-0002](#adr-0002-roles-carry-requirements-not-an-allow-list) | 2026-06-27 | Accepted | Authorization is role + scope grants, not a per-principal allow-list |
| [ADR-0003](#adr-0003-health-reads-ok-not-up) | 2026-06-27 | Superseded by [ADR-0050](#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain) | The healthy state is named `ok`, not `up` |
| [ADR-0004](#adr-0004-credentials-ship-bearer-only) | 2026-06-27 | Resolved | Bearer shipped first; `password` credentials (argon2id) landed in identity slices 1-2. OIDC / NATS still deferred |
| [ADR-0005](#adr-0005-the-first-owner-is-omniglass-bootstrap) | 2026-06-27 | Resolved | `omniglass bootstrap <username> [--password]`; the password-on-create path shipped, the `iam` namespace is deferred |
| [ADR-0006](#adr-0006-the-owner-invariant-is-enforced-by-bootstrap-for-now) | 2026-06-27 | Resolved | The single-owner invariant is now a DEFERRABLE constraint trigger, landed with grant revocation |
| [ADR-0007](#adr-0007-principals-are-gated-at-all-scope-not-scope-tree) | 2026-07-01 | Accepted | A principal is not a scope-tree entity; the `principal` capability confers access only at all-scope |
| [ADR-0008](#adr-0008-disable-is-hard-revocation-no-token-version-column) | 2026-07-06 | Accepted | Disable revokes live sessions via the per-request `active` re-read; no token-version column (nothing consumes it) |
| [ADR-0009](#adr-0009-root-exclusion-lives-on-the-grant-not-a-new-scope-kind) | 2026-07-06 | Superseded by [ADR-0011](#adr-0011-grant-scope-is-an-operator-not-a-boolean-modifier) | The deploy "act on the subtree but not the root" capability is an `exclude_root` grant modifier, not a new scope kind |
| [ADR-0010](#adr-0010-impersonation-is-a-session-not-a-credential-guarded-by-capability-cover) | 2026-07-06 | Accepted | Impersonation ships view-as + act-as as an `impersonation_session` (not a credential), guarded by capability-cover, with a real-actor audit column |
| [ADR-0011](#adr-0011-grant-scope-is-an-operator-not-a-boolean-modifier) | 2026-07-06 | Accepted | Generalize the `exclude_root` boolean into a `scope_op` operator (`subtree` / `subtree_excl_root` / `self`), a flat enum, not a predicate-expression tree |
| [ADR-0012](#adr-0012-owner-accounts-are-un-impersonatable-impersonation-stays-capability-gated-not-scope-intersected) | 2026-07-07 | Accepted | Owner accounts are un-impersonatable by anyone; impersonate stays swept by `principal:*`; drop act-as scope intersection (#101) |
| [ADR-0013](#adr-0013-a-grant-cannot-confer-capabilities-the-granter-lacks) | 2026-07-07 | Accepted | Grant creation is refused when the granted role's capabilities exceed the granter's all-scope capabilities (admin cannot self-promote to owner) |
| [ADR-0014](#adr-0014-the-audit-trail-is-a-sensitive-read-not-reached-by-a-partial-global-wildcard) | 2026-07-07 | Superseded by [ADR-0015](#adr-0015-permissions-are-topic-patterns-single-token-and-tail-wildcards) | The audit trail is admin/owner-only: `audit` is a sensitive resource that `*:read` does not confer, only an explicit `audit:read` or `*:*` |
| [ADR-0015](#adr-0015-permissions-are-topic-patterns-single-token-and-tail-wildcards) | 2026-07-07 | Accepted | Permissions match like NATS subjects (`*` one token, `>` tail); admin-sensitivity is a deeper `:admin` token no partial wildcard reaches; owner is `>` |
| [ADR-0016](#adr-0016-a-principal-can-be-purged-and-the-audit-trail-is-denormalized-to-survive-it) | 2026-07-09 | Accepted | A principal can be hard-deleted (purge, gated on archival); the audit trail survives via a denormalized actor label and `ON DELETE SET NULL`, retiring the "never hard-deleted" rule (soft-delete verb: archive) |
| [ADR-0017](#adr-0017-credential-is-renamed-secret-the-cascade-is-the-reuse-mechanism) | 2026-07-09 | Accepted | The access-secret member of the config / credential / variable trio is renamed credential to secret: an encrypted-at-rest typed value resolved most-specific-wins down the cascade |
| [ADR-0018](#adr-0018-the-avatar-read-endpoint-is-json-not-raw-image-bytes) | 2026-07-10 | Accepted | A profile picture is read through a JSON `image_base64` endpoint the console renders as a data URL, not a raw `image/jpeg` handler, so every route stays under the Huma authz middleware |
| [ADR-0019](#adr-0019-every-credential-is-time-bounded-token-purpose-not-expiry-shape) | 2026-07-11 | Accepted | Every credential is time-bounded (reverses tokens-never-expire): session 12h, token / bootstrap 90d default with a `--ttl` capped at 365d; a `credential.purpose` column, not the expiry shape, tells session from token |
| [ADR-0020](#adr-0020-variable-slice-1-types-inline-and-mirrors-the-secret-arc) | 2026-07-11 | Accepted | The variable member ships plaintext, typed inline against a `value_type` enum (no `variable_type` registry), on the secret owner arc; template scope, groups, the `$var:` consumer deferred |
| [ADR-0021](#adr-0021-tag-slice-1-a-governed-key-registry-with-entity-update-gated-bindings) | 2026-07-12 | Accepted | The tag primitive ships its first slice (governed key registry, per-entity bindings, cascade); minting a key is admin `tag:create`, setting a value is the entity's own `update` |
| [ADR-0022](#adr-0022-effective-tags-resolve-onto-systems-and-locations-a-placed-system-inherits-its-location) | 2026-07-13 | Accepted | Directory rows carry batch-resolved effective tags; effective resolution extends to systems and locations, and a placed system inherits its location's tags |
| [ADR-0023](#adr-0023-the-iam-directory-reads-principal-role-principal_group-are-admin-tier) | 2026-07-13 | Accepted | The IAM directory reads (principal, role, principal_group) move to the admin tier (`<resource>:read:admin`), so viewer's `*:read` floor no longer reaches Users, Roles, and Groups |
| [ADR-0024](#adr-0024-a-tag-key-may-constrain-its-values-to-an-enum) | 2026-07-13 | Accepted | A tag key may declare an `allowed_values` enum (empty = free text), enforced on the binding write; a free key autocompletes its distinct in-use values |
| [ADR-0025](#adr-0025-secret-is-a-sensitive-resource-a-per-secret-admin_sensitive-flag-flips-a-secret-to-the-admin-tier) | 2026-07-13 | Accepted | `secret` leaves the bare `*` wildcard's reach (direct match and read floor); a per-secret `admin_sensitive` flag flips a secret to the `:admin` tier, so operators read operational device secrets in scope while platform credentials stay admin/owner-only at the same scope |
| [ADR-0026](#adr-0026-console-nav-ia-estate-values-get-their-own-top-level-group-the-settings-group-becomes-admin) | 2026-07-13 | Accepted | Console nav IA: Variables, Secrets, and Config get their own top-level Values group; Inventory holds the estate entities including Nodes; Interfaces and Tasks become facet panels; the Settings group is renamed Admin |
| [ADR-0027](#adr-0027-create-is-a-route-inventory-create-and-edit-unify-on-the-detail-accordion) | 2026-07-14 | Accepted | Inventory create/edit unify on the detail accordion: `New` routes to `/<entity>/create` (a draft) and Save hands off to `/<entity>/<id>` in edit; view is read-only, edit is the sole writer; the create/edit Drawer is retired |
| [ADR-0028](#adr-0028-rank-retired-from-the-type-registries-sort-is-alphabetical) | 2026-07-14 | Accepted | `rank` is dropped from `location_type`, `system_type`, and `component_type`; the three list operations sort by `display_name, id` instead |
| [ADR-0029](#adr-0029-files-slice-1-a-content-addressed-blob-store-and-a-tenant-wide-file-handle) | 2026-07-14 | Accepted | Files slice 1: a content-addressed `blob` store primitive (pgblobs) and a tenant-wide `file` handle; no placement arc (a file is 1:many, its locality is a future attachment), a binary `sensitive` flag reusing the secret `:admin` tier (defaults off), a delete frees its unreferenced blob synchronously (async mark-sweep GC deferred), base64-in-JSON on the wire |
| [ADR-0030](#adr-0030-allowed_parent_types-constrains-where-a-location-may-be-placed) | 2026-07-14 | Accepted | `allowed_parent_types` constrains where a location may be placed |
| [ADR-0031](#adr-0031-component_make-registry-slice-1-an-official-boolean-a-deferred-referential-guard-and-website-scheme-validation) | 2026-07-14 | Accepted | `component_make` slice 1: an `official` boolean (not an `origin` enum) for consistency with the type registries; the in-use referential delete guard deferred to the `component_model` slice (nothing references a make yet); `website` scheme-validated to `http`/`https`, client and server, against stored XSS |
| [ADR-0032](#adr-0032-the-required-permission-is-published-per-route-and-the-permission-universe-is-route-derived) | 2026-07-17 | Accepted | Every gated route stamps its required permission into the OpenAPI (`x-omniglass-permission`), so the permission universe is derived from the routes rather than a hand-kept catalog |
| [ADR-0033](#adr-0033-settings-persist-only-the-override-level-base-layers-are-recomputed-in-memory) | 2026-07-17 | Accepted | The settings engine persists only the override level; the `code` and `file` base layers are recomputed in memory each boot, so restore is a delete (diverges from scaling.md's "materialized in Postgres") |
| [ADR-0034](#adr-0034-the-settings-gateway-is-unscoped-only-the-permission-gates-it) | 2026-07-17 | Accepted | Settings Gateway methods are unscoped: ABAC storage-scope is not applicable to platform / principal config; only the `settings:<action>` permission gates them |
| [ADR-0035](#adr-0035-settings-resolve-as-a-cascade-over-principals-with-a-broader-wins-lock) | 2026-07-17 | Accepted | Settings resolve down the principal hierarchy reusing the cascade primitive, with per-key provenance and a top-down broader-wins lock |
| [ADR-0036](#adr-0036-a-node-is-a-kindnode-principal-with-an-interim-bearer-credential-and-static-per-connection-nats-subject-permissions) | 2026-07-07 | Accepted | A node is a `principal` of `kind=node` with a 1:1 detail table and a bearer `credential` row (interim shared secret), and per-node NATS isolation is static per-connection subject permissions via an in-process auth callback; nkey/JWT deferred |
| [ADR-0037](#adr-0037-telemetry-is-a-protobuf-event-over-jetstream-with-an-inline-owner-confining-consumer) | 2026-07-07 | Accepted | Telemetry is a protobuf `Event` over a JetStream durable consumer; the consumer binds the owner from the task's interface and confines a node to its own tasks inline (no separate raw-telemetry table or Postgres queue); raw persistence + replay and label-based multi-owner routing deferred |
| [ADR-0038](#adr-0038-the-reachability-verdict-is-a-built-in-state) | 2026-07-07 | Accepted | The per-interface reachability verdict `interface.reachable` is a built-in **state** (not a metric); availability is `time_in_state` over it; readiness is interface-type-defaulted and interface-overridable, node-executed, not a `calc_rule` |
| [ADR-0039](#adr-0039-an-interface-is-a-device-api-the-interface-type-is-its-transport-not-its-driver) | 2026-07-08 | Accepted | An interface is a device **API** named by its protocol (not a NIC); `interface_type` = its **transport** (the reach gate), a **driver** = the collect layer (protocol handler + transports + normalized menu, what a device CAN do), a template **curates** (SHOULD), the instance holds what **IS** there; OIDs/commands live in the driver, not the template |
| [ADR-0040](#adr-0040-the-task-is-derived-read-only-plumbing-projected-from-its-interface) | 2026-07-14 | Accepted | The `task` is **derived** read-only plumbing: creating an `interface` derives its one poll task, so task create/update/delete routes and the `task:create` / `:update` grants are dropped; `task.node_name` is removed and **projected** from `interface.node_name` (the worklist and telemetry owner-confinement join the interface), and a node purge cascades its interfaces and their tasks. Reverses the checkpoint-5d task-CRUD build; refines ADR-0039 |
| [ADR-0041](#adr-0041-settings-are-a-reflected-typed-struct-with-generated-client-and-server-validation) | 2026-07-19 | Accepted | A setting is declared **once**, as a tagged field on a canonical `Settings` Go struct; reflection produces the `code` defaults layer and the namespace registry, Huma reflects the struct into the OpenAPI schema so the typed client `values` is a `Settings` struct, and both the server PATCH and the generated client validate against that **same reflected schema**. Closes the slice-0 write-validation thin cut; retires the hand-kept `defaults.yaml` and `Namespaces()` slice |
| [ADR-0042](#adr-0042-field-cascade-and-the-type-default-floor) | 2026-07-19 | Accepted | A field's resolved value is deepest-set-wins down `product -> location -> system -> component`, falling to the field's **type default** when nothing is set at any scope; the type default is the **floor** of the cascade, not a competitor to it. This slice is component-only (resolved = the component's set value, else the type default); the multi-scope cascade is tracked by #291 |
| [ADR-0043](#adr-0043-the-property-catalog) | 2026-07-19 | Accepted | The `datapoint_type` catalog is generalized into a primitive-agnostic **`property`** catalog (the typed set of signals a datapoint observes and a field declares): the unused scope ladder becomes an `official` boolean, `value_type` becomes `data_type` (text to string, add bool), `kind` is nullable (a declared-only property has none), and `validation` is a **JSON Schema** validated by Huma's own validator (zero new dependencies). Value/source tables key by **name** (no FK), so the rename is behavior-preserving; the type-schema (`field_definition.key`) is the only binding and lands in PR-B |
| [ADR-0044](#adr-0044-the-component-classification-catalogs) | 2026-07-20 | Accepted | The `component_make` catalog is generalized into **`vendor`** (a `kind` of manufacturer / integrator / developer), and two new leaf catalogs join it, **`driver`** (id, display_name, version) and **`capability`** (id, display_name), as the component-classification reference data: each a gated CRUD Catalog console page with read-only official seeded rows. `product` + `product_capability` + `component.product` are the next slice. This is PR2 of the estate-model shift toward property / event / command + vendor / product / driver / capability / standard / role / health |
| [ADR-0045](#adr-0045-the-product-catalog) | 2026-07-20 | Accepted | **`product`** lands as a first-class catalog entity, the concrete SKU that binds a **`vendor`**, a **`driver`**, a **`kind`** (`device` / `app` / `service` / `vm`), and a capability set via the **`product_capability`** join; **`parent_product_id`** models variants, and **`component.product_id`** (`on delete restrict`) points a component at the SKU it is, making the product the source of a component's shape and retiring the `component_type`-as-shape notion. PR3 of the estate-model shift; consumes the vendor / driver / capability catalogs from ADR-0044 |
| [ADR-0046](#adr-0046-the-event-log-kind-sink) | 2026-07-20 | Accepted; superseded in part by [ADR-0066](#adr-0066-logs-are-a-raw-ingest-lane-not-events) | A **log**-kind observation is no longer dropped at ingest: it lands in a new **`event`** table, the log-kind sink (a past occurrence) beside `metric_datapoint` / `state_datapoint` (a sampled present value). `event` carries the same datapoint owner exclusive-arc and provenance, plus `message` + `attributes`, and the reserved `event_id` FK stubs on the two datapoint tables are closed (`on delete set null`). Scope excludes the `datapoint`->`sample` rename (a later cleanup) and `property_value` / the current-value store (the fold-fields slice). P1 follow-up of the estate-model roadmap |
| [ADR-0047](#adr-0047-the-fields-fold-product_property-and-property_value) | 2026-07-21 | Accepted; superseded in part by [ADR-0085](#adr-0085-the-component_type-registry-returns-as-the-device-class-genus) (the `component_type` registry returns, above the product) | The standalone **fields** feature retires and folds into the estate model: a field was only ever a **property with `declared` provenance**, never a primitive of its own. **`product_property`** is the product's declared-property **contract** (`product_id`, `property_name`, `default_value`, `required`), replacing `field_definition`; **`property_value`** is the value store, carrying the **same owner exclusive-arc** as `metric_datapoint` / `event` plus `instance` and `provenance`, replacing `field_value`. `EffectiveProperties` unions the contract arm (`coalesce(set value, contract default)`) with the off-contract arm, so a productless component still resolves. `field_definition`, `field_value`, `component.component_type`, and the whole `component_type` registry retire. PR5 of the estate-model shift |
| [ADR-0048](#adr-0048-the-standard-blueprint-and-the-template-fork-seed-model) | 2026-07-21 | Accepted | `system_type` is promoted to **`standard`**, the blueprint a system conforms to and the system-side counterpart of `product`: it gains `parent_standard_id` (variants), a declared-property contract, and its own `standard:*` Catalog resource, and `system.standard_id` becomes **optional**. `standard_property` and `location_type_property` join `product_property`, and one **owner-generic** `EffectiveProperties(ownerKind, ownerID)` resolves component, system, location, and node off a single parameterized template. A standard and a location type are created by **forking an in-code template** (one-time, no inheritance), so a shipped row is **operator-owned** (`official: false`, seeded **if absent**), while a system **conforms** to its standard with **live** inheritance; only the canonical catalogs keep the authoritative upsert. PR6 of the estate-model shift |
| [ADR-0049](#adr-0049-the-system-role-capability-gated-staffing-and-the-resolved-capability-set) | 2026-07-21 | Superseded by [ADR-0087](#adr-0087-capability-gated-staffing-retires-an-alarm-impairs-its-component-not-a-named-capability) | A **`system_role`** is a slot a system needs filled (a table microphone, a main display), declared on a **standard** (inherited live by every conforming system) or on one **system** (ad-hoc) over the same exclusive arc `property_value` uses, requiring a **conjunctive** `role_capability` set and carrying a **`quorum`**. A component's capabilities become a **resolved set** (`EffectiveCapabilities` = its product's, plus its own `component_capability` `present=true` rows, minus its `present=false` ones), because `product` is optional and a strict guard over a product-only fact would lock a productless component out of every role. `AssignRole` **refuses (422) and names the missing capabilities**, joining the location placement constraint as a refusal on modeled grounds that names the parties. **Quorum** ships here (staffing is visible without health); **impact** and the SLI rollup land in PR8. Supersedes the `system_template_member` role-requirement design. PR7 of the estate-model shift |
| [ADR-0050](#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain) | 2026-07-21 | Accepted | Health is **recorded as a transition** and **recomputed at the write**, never on read. An **`alarm`** is component-local and names the **capabilities** it degrades; a component satisfies a role only when it provides every required capability and none of them is degraded; a role below its **quorum** is impaired and contributes its **`impact`** (`outage` / `degraded` / `none`); a system takes the worst of its roles, a location the worst of its systems. The verdict domain is **`healthy` / `degraded` / `outage`** and the judgement is a **pure package** (`internal/health`), unit-tested with no database. The recorded carrier is **`state_datapoint`**, already transition-only, so the history is edges and only edges; **compute-on-read** (no history) and **write-through-on-read** (the edge timestamped when somebody looked) are both rejected. A **read never writes**, and it computes the verdict it serves from the same rows it shows, so a report cannot contradict its own evidence. PR8 of the estate-model shift, closing epic [#266](https://github.com/hyperscaleav/omniglass/issues/266) |
| [ADR-0051](#adr-0051-membership-is-the-attachment-and-a-role-is-what-it-does) | 2026-07-21 | Accepted | Membership is the attachment, and a role is what it does |
| [ADR-0052](#adr-0052-the-cascade-resolves-through-membership-and-secrets-carry-no-system-band) | 2026-07-21 | Accepted | The cascade resolves through membership, and secrets carry no system band |
| [ADR-0053](#adr-0053-a-name-is-the-address-a-uuid-is-identity) | 2026-07-21 | Superseded in part by [ADR-0056](#adr-0056-every-foreign-key-stores-a-primary-key) | A name is the address, a uuid is identity |
| [ADR-0054](#adr-0054-the-shell-owns-a-panels-action-rail-the-body-registers-and-never-draws) | 2026-07-21 | Accepted | A panel's action bar is **declared, not laid out**: a blade body binds through `lib/blades`, a Drawer form body through `lib/formactions`, and `BladeStack` / `Drawer` draw the result through the one `PanelFooter` rail. The opt-in `DrawerFooter` helper is deleted. A convention a body must remember can be forgotten, and was, by two forms for months while it was copied into six new pages around them |
| [ADR-0055](#adr-0055-the-tag-variable-and-secret-owner-arcs-key-by-name) | 2026-07-21 | Superseded by [ADR-0056](#adr-0056-every-foreign-key-stores-a-primary-key) | The tag, variable, and secret owner arcs key by name |
| [ADR-0056](#adr-0056-every-foreign-key-stores-a-primary-key) | 2026-07-22 | Accepted; the slug-keyed carve-out is retired by [ADR-0062](#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle), the health carve-out amended (#717) | Every foreign key stores a primary key. **Amended (#717):** the health carve-out is gone: the advisory lock hashes `health/<kind>/<id>` and has since the identity epic (#627) landed its addressing slice, because a name-keyed lock partitions the estate only while names do and that epic scoped a location's name uniqueness to its placement |
| [ADR-0057](#adr-0057-the-cascades-least-specific-tier-is-platform-and-a-default-is-not-a-tier) | 2026-07-21 | Accepted | The cascade's least-specific **binding** tier is renamed `global` to **`platform`** on both axes (same rung, no precedence change); a **`default`** is off the axis entirely, a column on a type declaration rather than a tier; there is **no root location**; a write at the tier needs its own **`platform:<action>`** permission. **Breaking:** a secret sealed at the old tier can no longer be decrypted (the AEAD binds the owner kind) |
| [ADR-0058](#adr-0058-a-run-mode-is-a-verb-under-its-noun-and-no-command-may-be-shadowed) | 2026-07-22 | Accepted | A run mode is a verb under its noun, and no command may be shadowed |
| [ADR-0059](#adr-0059-every-collection-segment-is-a-command-level) | 2026-07-22 | Accepted | Every collection segment is a command level |
| [ADR-0060](#adr-0060-a-resource-is-one-kebab-case-noun-nesting-means-ownership) | 2026-07-22 | Accepted | A resource is one kebab-case noun; nesting means ownership |
| [ADR-0061](#adr-0061-a-calculated-series-is-current-at-its-highest-id-not-its-newest-timestamp) | 2026-07-22 | Accepted | A calculated series is current at its highest id, not its newest timestamp |
| [ADR-0062](#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle) | 2026-07-22 | Accepted | A registry takes a uuid primary key and a renameable handle |
| [ADR-0063](#adr-0063-the-telemetry-model-is-typed-registries-over-bare-noun-data-tables) | 2026-07-23 | Accepted; superseded in part by [ADR-0079](#adr-0079-five-telemetry-lanes-and-property-stops-being-the-genus) | The telemetry model is typed registries over bare-noun data tables |
| [ADR-0064](#adr-0064-placement-and-classification-are-mutable-after-create) | 2026-07-23 | Accepted | Placement and classification are mutable after create |
| [ADR-0065](#adr-0065-property-sample-and-current-value-replace-the-datapoint) | 2026-07-28 | Accepted; superseded in part by [ADR-0079](#adr-0079-five-telemetry-lanes-and-property-stops-being-the-genus) | Property, sample, and current value replace the datapoint |
| [ADR-0066](#adr-0066-logs-are-a-raw-ingest-lane-not-events) | 2026-07-28 | Accepted | Logs are a raw ingest lane, not events |
| [ADR-0067](#adr-0067-bookings-are-exclusive-arc-owned-schedules-reconciled-against-observed-usage) | 2026-07-28 | Accepted | Bookings are exclusive-arc-owned schedules, reconciled against observed usage |
| [ADR-0068](#adr-0068-the-api-error-model-is-the-stock-rfc-9457-shape) | 2026-07-30 | Accepted | The API error model is Huma's stock RFC 9457 problem+json (`ErrorModel` with `ErrorDetail` `{location, message, value}`); the custom `code` plus `violations` envelope sketched on the API page is retired |
| [ADR-0069](#adr-0069-cycle-safety-is-provenance-based-not-topology-based) | 2026-07-30 | Accepted | Cycle safety is provenance-based: consequence writes carry `provenance='calculated'` with a `source_rule` naming the producer, and rules never route on their own consequences; supersedes the "alarms are terminal upstream and never write samples" premise |
| [ADR-0070](#adr-0070-retire-the-standalone-effective-secrets-and-effective-variables-per-component-panels-fields-become-the-component-value-surface) | 2026-07-16 | Accepted | retire the standalone effective-secrets and effective-variables per-component panels; fields become the component value surface |
| [ADR-0071](#adr-0071-a-template-is-a-clonable-example-not-a-versioned-shape-an-instance-pins) | 2026-07-31 | Accepted | A template is an example configuration an operator **clones**: creating from one is a one-time fork with no inheritance and no back-pointer, so templates stay **upgrade-safe because nothing remains connected to them**. The versioned-shape model retires (`component_template`, `system_template`, `*_version`, channels, the frozen BOM, instance pinning); a component's shape is its `product`, a system's is its `standard`, and a system **conforms** to that standard with live inheritance. Reverses ADR-0045's deferral and ADR-0049's "templates stay `Design`" |
| [ADR-0072](#adr-0072-an-envelope-is-not-named-after-its-passengers-and-an-insert-struct-takes-the-write-suffix) | 2026-07-31 | Accepted | Two naming rules: a carrier is named for what it carries, never a passenger (the telemetry wire message is `TelemetryBatch`, since it carries samples, log lines, and later events), and a storage insert struct takes the **`Write`** suffix paired with the bare read struct (`MetricSampleWrite` / `MetricSample`), the pattern `LogLineWrite` set. Retires `MetricSampleEvent`, `StateSampleEvent`, `EventOccurrence`, and the proto `Event` |
| [ADR-0073](#adr-0073-a-driver-consumes-transports-a-transport-is-code-not-a-row) | 2026-07-31 | Accepted | a driver consumes transports; a transport is code, not a row |
| [ADR-0074](#adr-0074-an-approved-definition-rolls-up-to-one-pr-slices-cascade-on-an-integration-branch) | 2026-08-01 | Accepted | loop-executed work rolls up to one PR per approved definition; slices cascade through per-slice gates on an integration branch |
| [ADR-0075](#adr-0075-an-alarms-condition-identity-is-a-raiser-supplied-dedup-key) | 2026-08-01 | Accepted | alarm gains dedup_key and the one-open-per-condition partial unique index; RaiseAlarm becomes a guarded conditional insert |
| [ADR-0076](#adr-0076-a-renameable-human-typed-identifier-stays-in-the-url-and-the-write-returns-the-uuid) | 2026-08-04 | Accepted | the name stays renameable and addressable; rename is a custom method, every write returns the uuid, and one validator applies one of two rules |
| [ADR-0077](#adr-0077-a-group-name-obeys-the-entity-name-rule-tightening-a-pattern-the-code-had-excused) | 2026-08-04 | Accepted | principal_group.name moves to the entity name rule, retiring the looser API-layer pattern |
| [ADR-0078](#adr-0078-a-read-only-field-renders-as-a-fact-not-as-a-box-that-refuses-typing) | 2026-08-04 | Accepted | a blade the operator cannot edit contains nothing shaped like a control; BladeField owns the read-or-edit switch |
| [ADR-0079](#adr-0079-five-telemetry-lanes-and-property-stops-being-the-genus) | 2026-08-05 | Accepted; the collective noun revised to signal lanes by [ADR-0084](#adr-0084-the-catalog-shell-and-five-signal-lanes) | five telemetry lanes with five names: the catalog splits on data_type, state renames to property, the value store folds into the series (tombstone unset, current values derived), the wire goes per-lane, logs split by origin, and a command records its status; reverses the property-as-genus half of ADR-0063/0065 |
| [ADR-0080](#adr-0080-retention-is-provenance-aware-never-declared-never-the-latest-row-per-series) | 2026-08-05 | Accepted | retention is provenance-aware: a prune never deletes a declared row and never the latest row of any series, shipped as the PruneSamples primitive before any retention feature exists |
| [ADR-0081](#adr-0081-the-control-plane-wire-is-one-subject-grammar-node-anchored-and-batch-granular) | 2026-08-06 | Accepted | the control-plane wire is one subject grammar, og.v1.verb.node with the node name exactly one token: the api.telemetry lane sits in its own segment, per-record subjects are rejected, and the core-NATS consumers (worklist, heartbeat) are singletons by construction with their HA fork named and deferred |
| [ADR-0082](#adr-0082-the-type-resource-renames-to-location_type) | 2026-08-06 | Accepted | the permission resource type renames to location_type on every surface (route stamps, roles seed, console gates, guard fixtures); the generic word retires from the permission vocabulary |
| [ADR-0083](#adr-0083-the-catalog-rail-is-sectioned-by-the-estate-noun-each-registry-serves) | 2026-08-06 | Superseded by [ADR-0084](#adr-0084-the-catalog-shell-and-five-signal-lanes) | the Catalog rail is sectioned by the estate noun each registry serves, entries keep the registry's own word (Types where that is all there is), Telemetry holds what gets recorded and Action what the platform does, and the /catalog hub teaches the map with live counts |
| [ADR-0084](#adr-0084-the-catalog-shell-and-five-signal-lanes) | 2026-08-07 | Accepted | Catalog is one rail entry opening a shell: a grouped subrail (Telemetry, Actions, Components, Systems, Locations, Metadata) navigating to the per-registry pages at canonical URLs, with an Overview landing; the organizing axis is direction (Telemetry is what you receive, Actions what you send or run), the lane collective noun becomes five signal lanes, and secret types loses its nav slot |
| [ADR-0085](#adr-0085-the-component_type-registry-returns-as-the-device-class-genus) | 2026-08-07 | Accepted; partially reverses [ADR-0047](#adr-0047-the-fields-fold-product_property-and-property_value) | The `component_type` registry returns as a nested taxonomy classifying the product, not the component: it carries the identity facts that span products (naming stem, display name, icon, abbrev, default tags), inheriting down the tree with override at any node |
| [ADR-0086](#adr-0086-the-product-classification-floor-and-the-kind-split) | 2026-08-07 | Accepted | Every component is required to name a product (the three seeded generics cover anything unmodeled); product.kind narrows to device / app / service, no default, required at create; vm retires, folded into app |
| [ADR-0087](#adr-0087-capability-gated-staffing-retires-an-alarm-impairs-its-component-not-a-named-capability) | 2026-08-07 | Accepted | The alarm-capability-role chain retires: an alarm impairs its component's own verdict wholesale, an occupant satisfies its role whenever its own verdict is not outage, and the typed-slot guard is the only assignment-time check; records the 409-vs-422 refusal line and the choice/alternate boot-seed reconciliation carve-out. Supersedes ADR-0049, amends ADR-0050 |
| [ADR-0088](#adr-0088-a-placement-change-is-an-authorization-act-so-a-move-is-its-own-verb) | 2026-08-08 | Accepted | Placement (parent, location) leaves the component/system/location PATCH body and becomes its own `:move` custom method under its own `<resource>:move` permission, closing the gap where clearing parent_id to root via PATCH needed no scope check while creating the same root already required an all-scoped grant; MoveLocation deliberately gains no clear-to-root capability |
| [ADR-0089](#adr-0089-a-uuid-is-the-address-a-dotted-path-is-a-positional-lookup) | 2026-08-08 | Accepted | A uuid is the address the platform generates; a dotted path (location segments, a `$comp`/`$sys`/`$role` accessor, plane-local segments) is a human-typed positional lookup, resolved by an allowlist name rule that renders as a CLI argument, a REST path, or a NATS subject with no escaping; dash and bare renders are display-only and never accepted back. Extends ADR-0062, amends ADR-0076 in justification |
| [ADR-0090](#adr-0090-a-derived-value-is-a-default-that-tracks-until-touched) | 2026-08-08 | Accepted | A derivable value fills at create, tracks live while the platform holds the pen, freezes on the operator's first edit, and resumes tracking only on an explicit reset; `component.name_generated` ships `DEFAULT false`, not the epic's `DEFAULT true`, so no pre-existing operator-typed name is silently claimed by the platform |
| [ADR-0091](#adr-0091-an-update_mask-says-which-fields-a-patch-writes) | 2026-08-09 | Accepted | A `PATCH` body may carry an optional `update_mask` with AIP-134 semantics exactly (absent is the implied mask of populated fields, present writes exactly what it names so a zero value clears, `["*"]` is full replacement, an unknown field is a 422 naming it); it rides in the body, the three-state string sentinel stays, the other 108 `PATCH` routes are not retrofitted, and the role declarations convert from `PUT` to `PATCH` as its first consumer |
| [ADR-0092](#adr-0092-a-location-move-recomputes-both-ancestor-chains) | 2026-08-09 | Accepted | `MoveLocation` recomputes health over both ancestor chains (joined and left) inside its own transaction, the second and last member of the exception class ADR-0088 carved out for a system's relocate; one named row per side seeds the recursive ancestry walk the query already performs, and a no-op move recomputes nothing |
| [ADR-0093](#adr-0093-the-tag-cascade-follows-the-component-it-resolves-for) | 2026-08-09 | Accepted | Effective tags are scoped by the component the cascade resolves FOR, not per band: a caller who can read the component sees every value that cascades onto it, including from systems and locations it could not list directly; the `?system=` seed is a filter over that answer, never a widening of it |
| [ADR-0094](#adr-0094-benchmarks-are-the-second-performance-instrument-and-they-gate-nothing) | 2026-08-09 | Accepted | Performance has two instruments: round-trip counting gates in `make test`, wall-clock benchmarks (`make bench`, two estate sizes, fixtures outside the timed section) are diagnostic and gate nothing; no CI perf job, no stored baseline, no `EXPLAIN` assertions (deferred), no timing assertion anywhere, and one candidate benchmark was measured as three-quarters transport and dropped rather than shipped. **Amended (#725):** the `EXPLAIN` deferral holds for the plan a planner PREFERS and is lifted for the access path a query can REACH; planned under `enable_seqscan = off` that answer does not move with the fixture's size, so an access-path assertion on one relation (index name AND index condition, never plan shape, never a duration) becomes a third instrument that gates |
| [ADR-0095](#adr-0095-an-operator-forks-a-shipped-registry-row-instead-of-the-platform-writing-it) | 2026-08-09 | Accepted | An operator's edit of a shipped (`official: true`) registry row does not write that row: it forks it into `registry_shadow`, one registry-agnostic table keyed `(registry, row_id)` on the shipped row's OWN uuid, and reads resolve the shadow over the official row; restore is deleting the shadow. One uuid and one name per logical row either way, so no foreign key, walk, audit row or URL learns about the fork. A fork captures the whole mutable row and never the structure, which makes inheritance on a nested registry resolvable per node. `component_type` is the first adopter. **Amended (#709):** a fourth fact adopters must keep, that the registry row's lock is taken in a statement of its OWN before the read that resolves the shadow, because `for update` on the resolving read serialises the transactions without refreshing what the waiter reads |

| [ADR-0096](#adr-0096-the-system_type-name-returns-as-the-coarse-space-taxonomy) | 2026-08-09 | Accepted | A nested, universally seeded **`system_type`** registry lands beside `standard` (not inside it): the coarse taxonomy of what kind of space a system is (`av / room / {board, class, ...}`, `av / sign / {...}`), with `stem`, `abbrev`, and `icon` inherited down `parent_id` and overridable at any node, and `system.system_type_id` nullable for now. The identifier is reused deliberately (it was ADR-0048's retired column name for `standard_id`), so its docs-lint denylist entry is removed on ADR-0085's precedent |
| [ADR-0097](#adr-0097-allocation-tests-the-name-it-would-mint-rather-than-reading-the-ordinal-it-stored) | 2026-08-10 | Accepted | The generated ordinal becomes a stored nullable `component.ordinal`, but sibling allocation does **not** read it: it reads sibling NAMES and returns the lowest ordinal whose MINTED name is free. An operator can hold a generated-shaped name with no ordinal, and the unique index is on the name, so an ordinal-only allocator would remint a taken name as a `23505`. Minting candidates instead of parsing siblings is what makes a stem-less name (a floor called `1`) possible, not the column; the column is what every reader DOWNSTREAM of allocation consumes. No scoped-unique index on the ordinal |
| [ADR-0098](#adr-0098-a-label-rule-reads-what-an-entity-is-never-where-it-sits) | 2026-08-10 | Accepted; the placement exclusion reversed by [ADR-0100](#adr-0100-a-label-cascades-where-the-blast-radius-is-a-placement-and-waits-for-the-verb-where-it-is-the-estate), amended (#729) | A label rule is Go `text/template` over a **closed `map[string]string`** AND a **closed grammar**: the sandbox is the data map (a secret is absent rather than filtered) plus an allowlist over the parsed tree (a closed set of node types and function names, so `printf` and friends are refused at rule-edit time; a length cap bounds output, which is not the same as bounding work). The map carries the entity's own columns and its resolved classification facts, and deliberately **no placement**: every input then changes on exactly five of the entity's own acts (create, rename, move, reclassify, reset), which is what makes the stored label's recompute-and-compare invariant provable. Exposing a location's name would put a location rename in that set and stale every label under it. The global tier is one row per entity kind with TWO columns, `default_template` (boot-seed space, authoritative) and `template` (operator space, nullable). **Amended (#729):** the key table in the entry is the map as of this decision; each kind's keys are now declared once in `internal/storage/label_keys.go` beside the accessor that produces each value, the map is built by ranging that declaration, and the docs render it from `docs/src/generated/labeldata.json`, so the taught set and the reachable set cannot disagree |
| [ADR-0099](#adr-0099-the-acronym-list-is-one-replaceable-setting-not-a-shipped-list-plus-operator-additions) | 2026-08-10 | Accepted | The acronym dictionary `title` consults is ONE key, `label.acronyms`, in a new `platform,client` settings namespace; an operator's list REPLACES the shipped one and provenance tells them apart, rather than a union of shipped plus additions (which would give one key a merge rule no other setting has, make the wire value a fragment rather than the effective dictionary, and make a shipped entry unremovable). The engine resolves the dictionary at render time and caches the compiled engine against the dictionary ITSELF, so a change builds a replacement rather than mutating one and no writer has a generation counter to forget; validation uses a dictionary-less engine, since whether a rule parses is a fact about the rule alone |
| [ADR-0100](#adr-0100-a-label-cascades-where-the-blast-radius-is-a-placement-and-waits-for-the-verb-where-it-is-the-estate) | 2026-08-10 | Accepted | A label rule reads its entity's PLACEMENT (a component's location label and its primary system's type label, a system's location label), reversing ADR-0098's exclusion, and the write paths that keep those honest are derived from the map rather than enumerated, a derivation the epic's review pass then caught missing the one the DATABASE performs (an `ON DELETE CASCADE` is a write path with no Go on it, so a system's delete now releases its memberships explicitly). The line is BLAST RADIUS, not ownership: bounded by a placement (the rows at one location, one system's members, one component's membership) it cascades inside the act's own transaction; bounded only by the estate (a rule at any tier, a classification row's display_name, the acronym list) it waits for the preview-then-apply verb. A preview is an apply that rolls back, so it lists exactly what the apply changes including the second hop. One audit row per operation, keyed on the rule, never one per changed entity |
| [ADR-0101](#adr-0101-the-first-of-its-stem-in-a-bucket-carries-no-ordinal-and-the-mint-that-says-so-is-the-one-allocation-tests) | 2026-08-10 | Accepted | A generated **system** name suppresses the ordinal on the first of its stem in a placement bucket (`boardroom`, then `boardroom-2`), and the order dependence is accepted: deleting the bare one while the second survives frees the bare name again, and `boardroom-1` never exists. Suppression is a field on the MINT rather than a change to the shape (a component still reads `display-1`), and the ALLOCATOR takes that same mint, so a suppressing mint and a non-suppressing allocator cannot disagree on ordinal 1 and turn the second create into a `23505`. A placement bucket becomes a value per entity kind, so a location's two buckets cannot be written as a system's three, and the allocation lock loses the stem from its key, since two stems can now mint one name. The pen and both verbs spread to system and location; only a system generates, and a location's `:resetName` refuses with the missing fact named. No backfill: the default false is the right value for a row an operator already named. **Amended (#696, #691):** the component tier's two guards close too, its `:move` with the identical bucket comparison and its reclassify on the RESOLVED STEM rather than the classification id, because a component reaches its stem through a product and two products of one `component_type` mint the same name; the system tier's matching residual is #706 |
| [ADR-0102](#adr-0102-a-name-rule-is-a-declaration-a-type-opts-in-with-and-a-rule-change-renames-nothing) | 2026-08-10 | Accepted | A **`location_type.name_rule`** (nullable jsonb) is a type's opt-in to naming the locations it classifies, and it is a **declaration** (a stem, possibly empty, plus the first-ordinal suppression flag) rather than the `label_rule` template beside it. A name has to satisfy `validateEntityName`, it lands in a scoped-unique index, and other things reference it, so an unrenderable rule has no safe degradation the way a label's does; a declaration IS a `nameMint`, so a rule is refused at RULE-EDIT time by minting from it (ordinal 1 and a nine-digit ceiling bound the whole output space), which a template's output could only be sampled. Null is the opt-out and there is no boolean beside it. A rule change **renames nothing**: there is no name-side recompute verb, deliberately, because relabelling in bulk is recoverable and renaming is not. A positional type permitted at root allocates `1`, `2` across the estate and that is legal, since the bucket is the placement and two positional types under one parent already share an ordinal space. **Amended (#657):** the entry's "only floor is genuinely auto-nameable" is now false in both halves, since ADR-0103 was reversed for `floor` and no shipped type carries a rule; the composed limit is that a withdrawn shipped rule cannot be un-shipped, because insert-when-absent leaves the row alone and the wire cannot spell "clear" |
| [ADR-0103](#adr-0103-a-positional-name-is-allocation-order-and-the-real-world-designation-is-a-label) | 2026-08-11 | Accepted | A positional name is **allocation order**, never a claim about the world, and the entry first kept the dev estate's divergence (a floor named `1` labelled Level 2) on the argument that a name is an address and a label is what a human reads. **Amended (#657) and REVERSED for `floor`:** a floor's designation is not an integer at all (B2, LG, G, M, 12A), so an ordinal is the wrong KIND of value for it rather than an imprecise one, and the basement objection dissolves with it, since nobody signs a floor `-1`, they sign it `B1`, already a legal name. `floor` becomes nominal, the dev estate's floors are named `level-2` and `level-1` for their real designations so ADR-0105's rule renders those labels and the two pins are released, and the cost is stated rather than hidden: NO shipped location type carries a rule, so location name generation ships **dormant**, kept covered by a positional type the tests create rather than by a fifth seeded type invented to keep the demo alive. Removing a shipped rule reaches new estates only (insert-when-absent), and no `PATCH` can clear one, so an estate that already seeded it keeps it. What survives: a stem-less positional name is right where the position is an arbitrary disambiguator (a parking deck, a rack row) and wrong where the number is a real-world fact |

| [ADR-0104](#adr-0104-a-create-form-shows-the-name-it-can-know-and-never-mints-one-to-preview-it) | 2026-08-11 | Accepted | A create form shows the **stem** a generated name will carry, resolved in the browser from the classification the operator just chose, and writes the ordinal as the token `n`, because the ordinal is allocated against live siblings inside the create's own transaction and does not exist until the row does. A **draft-preview verb that mints and rolls back** is refused: its answer is provisional (another create can take the ordinal between the preview and the commit), and the rolled-back mint takes the same advisory lock real creates need, so a form that previewed on every keystroke would serialise the estate's creates behind a UI affordance. **Re-rendering the label rule in TypeScript** is refused outright, as the second implementation of an engine slice 3 swept 42 copies of. The label is therefore not previewed at all: its data map carries `Name` and `Ordinal`, so it is unknowable for the same reason. The placement bucket is shown beside the field as a PATH and never as a prefix inside it, since names became scoped to placement and a name no longer contains its ancestry. **Amended (#699):** a RENDER is not a mint, and both refusals were about allocating, so `:renderLabel` resolves the rule through the same tiers with the same one engine, writes the token where the ordinal goes, and takes no lock; the form now shows both values in LOCKED fields, gated by the entity's `:create` with the placement resolved in the caller's read scope. **Amended again (#657):** the lock is an inline square icon action in the field's join, matching Settings' own Restore to default, and a locked field is `readonly` rather than `disabled`, because a disabled input fires no click and leaves the value out of the tab order; focus does not claim the pen, since a locked field is a tab stop and tabbing past would otherwise blank both fields. **Amended again (#702):** READING the lowest free ordinal is not minting one either, so the form shows `display-3` rather than `display-n` and the token retires; the answer is provisional, so the form posts it back as the create's `expected_ordinal` and a create that would land a different number is a 409 naming the one that moved, located on `body.expected_name` so the form can tell it from a name collision. The NAME's shape stops being client-side, which closes the naming half of #695. **Amended again (#695):** the ICON half closes the same way, the listing serving `resolved_icon` beside the raw `icon`, so `lib/typechain.ts` is deleted and no type-chain walk runs in the browser; it costs no read, since the list already loads the whole registry in one query. **Amended again (#702 review):** the precondition binds the drafted NAME rather than the ordinal, because a name carries the stem and the suppression rule as well as the number and an ordinal claim was met by a create that landed `monitor-1` where the form showed `display-1`; and the draft now REFUSES the parentless bucket its create refuses, reversing this entry's own "the draft does not rehearse the all-scope gate", since the previewed ordinal reports which of that bucket's names are taken and the stem asked about is the caller's to choose **Amended again (#693):** the lock is the console's ONE vocabulary for the pen, so it reaches the EDIT BLADE and the list's full-text `Generated` chip retires from both list renderers: an ownership fact belongs beside the field an operator can change it on, not in a cell charging the Name column the width of the word on every platform-labelled row. The NAME's own chip stays, on the blade beside the name, which is the same rule rather than an exception. The blade gains a state the create form has no equivalent for, a locked field showing a value about to change (the hand-back), because `:renderLabel` previews a row that does not exist and would answer an existing row with the NEXT sibling's ordinal; the hint carries it instead. It closes a silent pen theft: every blade posted `display() \|\| undefined` seeded from the stored label, so saving any unrelated field posted the platform's own rendering back as an override and took the pen **Amended again (#713):** "the placement resolved in the caller's read scope" no longer holds for one reference, the component's `system`, which the create binds as a membership under `system:update` (ADR-0107); the draft resolves it in that same set and carries the same conditional permission, so a preview is never served for a create the platform would refuse |
| [ADR-0105](#adr-0105-a-rule-reads-a-name-as-words-and-the-location-tier-ships-the-restatement-it-once-refused) | 2026-08-11 | Accepted | `words` joins the closed FuncMap (a run of `-` or `_` becomes one space, an edge run is dropped, everything else untouched), which is what finally lets a rule turn a kebab NAME into words: `title` alone leaves the separator standing, so the acronym dictionary of ADR-0099 could not be reached from a name by any spelling. Adding a function is a THREE-place act (FuncMap, AST allowlist, `FuncNames`) and the published set is now walked by a test rather than described. **Amended (#701):** it is ONE place; all three derive from a single declaration and the docs table is a fourth reader of it, so a function in the FuncMap the grammar refuses is unwritable rather than tested for. The global LOCATION rule ships as `{{title (words .Name)}}`, reversing the seed's own argument on its restatement half only: a restatement that RE-CASES and runs the operator's dictionary produces a string the read ladder's fallback cannot, where an echo could not, and the constant half ("Room" for every room) is still refused. The ladder's last rung stays verbatim, since this renders and STORES a label rather than prettifying on read; the estate keeps only the pins that say something a name cannot, nine at the time and **seven** after ADR-0103's reversal named the two floors for their designations |

| [ADR-0106](#adr-0106-a-location-type-is-platform-owned-and-a-nullable-object-clears-under-the-mask) | 2026-08-12 | Accepted | `location_type` adopts the **registry fork** (ADR-0095) rather than growing a third ownership model: the shipped rows seed `official: true`, the boot seed writes them authoritatively, and an operator's edit forks into `registry_shadow` with `:restore` discarding it. That is what makes a shipped value **withdrawable**, which insert-if-absent could never be, since it can add a default to every estate and remove one from none. The one-time backfill moves the edits estates already hold ON those rows into shadows first, telling an edit from a shipped value by the **audit trail** rather than by comparing columns against what this release ships, because a row holding a WITHDRAWN shipped value is indistinguishable from an edit by inspection and preserving it would defeat the withdrawal. A location type's property and metric **contracts stay writable** on a shipped row: a contract line is a row in its own table, nothing seeds one, and the official guard was dormant on this registry until the flip would have activated it. On the wire, a nullable OBJECT field clears by being **named in `update_mask`** with no value, since an object has no empty value to overload and an explicit null is indistinguishable from an omitted key after decoding; `name_rule` is the first and the convention is now the API's, not that field's. **Amended (#703):** the discriminator is REMOVED and the backfill is only the `official` flip, because with no release cut and no operator data there is no edit to preserve; the argument for it stands unchanged for the first release that has estates, which owes them a new migration rather than this one |
| [ADR-0107](#adr-0107-a-create-that-writes-a-membership-costs-what-the-membership-route-costs) | 2026-08-12 | Accepted | `POST /components` accepts a `system` and INSERTS that system's primary membership from it, the same row `PUT /systems/{name}/members/{component}` writes under `system:update`, while the create asked for no system permission at all: the create was the cheap way around the membership route's gate. The create now requires `system:update` when the reference is present and resolves it in that scope, so **two paths writing one row cost one permission**. The accepted consequence is a live narrowing: `operator` holds `component:create` and no `system:*`, so an operator can no longer create a component INTO a system, which reads as the role line rather than collateral damage (an operator maintains components, a deploy tech builds out systems and their membership). Granting `operator` the permission was refused as a much larger grant than "may bind a membership while creating". A second permission conditional on the REQUEST is published like the platform tier's, as `x-omniglass-conditional-permission`, and enforced in the handler because middleware cannot see the body; the console hides the picker from a principal that cannot use it and the API's refusal names the permission, so the narrowing is met before the form is filled in. **Amended (#707 review):** the console gate read the PERMISSION only, so a principal holding `system:update` over an empty scope (a location-scoped `deploy` grant, since the cross-tier expansion is unbuilt, #10) was offered the picker and refused on submit, and the API answered "system not found" for a system that caller could `GET`; the gate now also requires a system carrying the scope-aware `update` action, and the bind takes `system:read` beside `system:update` so a readable row is refused by AUTHORITY (403) rather than by absence. **Amended (#713):** the residual this entry accepted is closed, the `:renderLabel` draft resolving the same reference through the create's own resolver and carrying its conditional permission, because a preview that resolved it in `system:read` alone both previewed a refusal and handed back the system's type label; the LOCATION reference stays `location:read` on both routes, read and rendered rather than written |
| [ADR-0108](#adr-0108-settlement-reads-one-clock-and-a-zero-window-is-a-statement-of-intent) | 2026-08-12 | Accepted | Settlement's two timestamps come from **one clock, the database's**: a sample's `ts` is `default now()`, so the `now` a settle-check judges against is read with `select now()` inside the same transaction rather than from `time.Now()` in the server process. Two clocks on one comparison made the verdict a function of skew, and at `settle_window_seconds: 0` there is no margin to absorb it, so a command that genuinely failed could be reported `pending` and never settle on any deployment whose database is on another host. Separately, a **zero window is terminal by construction**, checked before any arithmetic: it is the documented way to say "settle immediately", a claim about intent rather than elapsed time, so no timestamp may make it pending. Stamping samples from Go was refused as the larger ripple (every telemetry `ts` defaults to `now()` and other readers rely on database ordering), and a tolerance was refused outright as the move that stops a test failing without stopping the behavior depending on skew. `Settle` stays pure and still takes `now`; what changed is who supplies it, at the cost of one round trip on each of the two settle paths. **Amended (#718):** "a check in a later transaction reads a strictly later timestamp" over-claims, since READ COMMITTED admits a concurrent issue committing after a settle-check began, so the delta can be negative; the verdict is unaffected (a negative delta is `pending`, and the zero case reads no timestamp), and the claim rather than the behaviour is what was corrected. **Extended (#719):** the same principle reaches the six history reads by a different mechanism and no round trip, since a read needs a BOUND rather than a value: the window travels as a duration and the query filters `ts >= now() - make_interval(...)`, so the instant is never named in Go |
| [ADR-0109](#adr-0109-an-alarm-carries-an-acknowledgement-and-not-a-snooze-or-a-resolve) | 2026-08-13 | Accepted | An alarm's raised state belongs to its **condition** (ADR-0075) and an acknowledgement is a fact about a **person**, so the acknowledgement is two nullable columns orthogonal to `cleared_at`, never a `status` enum, and `AcknowledgeAlarm` is the one alarm write that does **not** recompute health: acknowledging is not fixing. **Snooze** and **resolve** were refused rather than deferred: snooze suppresses notification and the notification registry is unbuilt (#618), so it would be a column that lies, and resolve is either the existing clear under a second name or an unspecified concept. The permission is **`alarm:acknowledge`**, spelled out like every other seeded verb; the `alarm:ack,snooze,resolve` string that appeared in test fixtures and in the identity-access page was never a design and nothing ever seeded or enforced it. Its scope resolves on the component tier from `alarm:acknowledge` itself rather than from `component:update`, and it is granted to `operator` and **not** to `deploy`, because a location-scoped `deploy` grant reaches no component tier at all (#714) and would hold a capability that acknowledges nothing. A second acknowledgement is **idempotent**, keeping the first person and the first time and writing no second audit row |
| [ADR-0110](#adr-0110-a-principals-identifier-is-the-gateways-answer-not-a-stored-functions) | 2026-08-13 | Accepted | `principal_label(uuid)` is dropped. What names a principal (a human's username, else a service account's own identifying column) is declared once in the gateway (`internal/storage/principal_ident.go`) and rendered into the statements it binds. A READ projects both sources and folds them in Go; the audit insert binds the fold as one expression, because it runs inside the CALLER's transaction on every operator write and a Go resolution there would cost a second round trip the alarm write path pins as an exact equation. Two shapes of one policy are held together by an invariant test over every principal kind, not by care. A `node` stays out of the resolution, exactly as the dropped function had it |
| [ADR-0111](#adr-0111-a-service-accounts-identifier-is-a-name-and-it-is-unique) | 2026-08-13 | Accepted | `service.label` becomes **`service.name`**: it is the username analogue for `kind=service`, the only handle the row has, so under the identity triad it is a name and it was the one place in the schema where `label` meant an identifier. The uniqueness question is answered rather than inherited: **unique**, matching `human_username_key` and `node_name_key`, because the string is denormalized as bare text into `audit_log.actor_username` and into an alarm's acknowledgement, where a duplicate is unresolvable after the fact. The table's declared identity shape moves from `ShapeIDOnly` to `ShapeHumanNotAKey`, and a new guard refuses any `ShapeIDOnly` table that carries a `name`. **Breaking wire change:** `svcBody.label` becomes `name`, and the group roster's mixed `coalesce(h.display_name, s.label, '')` splits into `name` and `display_name`, two fields each meaning one thing |
| [ADR-0112](#adr-0112-a-generated-flag-carries-the-schemas-type-and-a-structured-field-carries-json) | 2026-08-13 | Accepted | `cmd/cligen` derives each body flag's TYPE from the OpenAPI property: an `integer` field is an `int` flag, a `boolean` a `bool` flag, a `number` a `float64` flag, so a value the schema refuses is refused at the shell rather than by the server's 422. Every other shape keeps ONE string flag parsed as JSON (an object, an array, an untyped `any`, and a nullable number or boolean), because a nested value has no shell-native flag type and `null` has to stay sendable: it is what clears a field named in `update_mask` (ADR-0106). A nullable STRING is the exception and stays a plain string flag, since this API clears a string with the empty string. `--propagates=false` becomes the spelling for a bool flag, and the docs flag check fails on a bool flag handed a space-separated value |
| [ADR-0113](#adr-0113-a-validation-rule-is-typescript-and-a-native-constraint-attribute-is-not-one) | 2026-08-13 | Accepted | A console control carries **no** `required`, `min`, `max`, `pattern` or `step`: a rule is a pure function over the typed value, the surface renders its message inline beside the field, and the binding's `disabled` / `valid` refuses the submit. The audit decided it: 21 attributes on 24 rendered controls and **zero could ever fire**, because a Drawer's rail is portaled outside the `<form>` (ADR-0054), a blade has no form at all, and the four on genuine form paths sit in forms whose submit is disabled in exactly the states native validation would refuse. Wiring `form.requestSubmit()` instead would have covered the Drawers only, left every blade needing this decision anyway, and meant undoing the disabled gate so an unstyled browser bubble could refuse in place of an inline message. `aria-required` stays as the honest spelling, `type="email"` and `type="number"` stay as input types, and a guard test scans every `.tsx` |

## Entries

### ADR-0001: AI acts as a user; the `agent` principal is deferred

- **Date:** 2026-06-27 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/), [AI](/architecture/ai/)
- **Decision:** An AI tool authenticates over OAuth as an ordinary `human` or `service` principal and acts
  with exactly that principal's grants. A dedicated first-class `agent` principal kind is **not** in the
  initial architecture.
- **Context:** A separate `agent` identity would need its own authN, its own grant semantics, and its own
  audit treatment before any AI surface exists to use it. Treating AI as a scoped, audited user reuses the
  whole identity machinery and keeps the audit trail honest (the acting principal is the human or service).
- **Note:** The schema's `principal.kind` CHECK already **reserves** the `agent` value so a later slice
  adds the kind without editing the applied auth migration; no `agent` identity is issued today. If and
  when a first-class agent identity is built, that is a new entry that supersedes this one.

### ADR-0002: Roles carry requirements, not an allow-list

- **Date:** 2026-06-27 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** Authorization is built from additive `(role x scope)` grants, where a role is a capability
  set of `<resource>:<action>` permissions. An earlier sketch attached a per-principal **allow-list** of
  permitted actions directly.
- **Context:** A per-principal allow-list does not compose: the same operator role at two scopes, or a role
  inherited and extended, would be re-listed by hand per principal. Roles plus scope make the common case
  (the same role at different places) a single reused definition, and keep permissions additive and
  positive (no negative entries). It is also what makes the per-grant binding (an action and its scope bind
  in the *same* grant) expressible.

### ADR-0003: Health reads `ok`, not `up`

- **Date:** 2026-06-27 | **Status:** Accepted | **Pages:** [health](/architecture/health/)
- **Decision:** The healthy state of a component or system is named **`ok`**. An earlier draft used `up`.
- **Context:** `up` reads as reachability (the device answers), which is only one input to health. Health is
  a rollup verdict ("is this system working?") that can be unhealthy while every device is reachable, or
  healthy while a redundant member is down. `ok` names the verdict rather than the ping, so the word does
  not promise something narrower than the model delivers.
- **Superseded by** [ADR-0050](#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain)
  on the **word only**. The reasoning holds and the built domain still names the verdict rather than the ping;
  it spells the members **`healthy` / `degraded` / `outage`**, because `outage` says what a broken system means
  to the people in the room where `down` says what a device is doing.

### ADR-0004: Credentials ship bearer-only

- **Date:** 2026-06-27 | **Status:** Resolved (identity slices 1-2) | **Pages:** [identity and access](/architecture/identity-access/)
- **Resolved:** Password credentials shipped in identity slice 1 ([#35](https://github.com/hyperscaleav/omniglass/pull/35)) and slice 2 ([#70](https://github.com/hyperscaleav/omniglass/pull/70)): `credential.kind` now allows `bearer` or `password` (argon2id, PHC-encoded, one password per principal), and a human signs in with a username and password behind an httpOnly session cookie. The `oidc` / `nats` methods and the full `(method, identifier)` lookup key remain deferred (future slices).
- **Decision (divergence):** The shipped `credential` table carries `kind = 'bearer'` only, stored as the
  token's sha256 with a non-secret `ogp_` locator prefix. The design's fuller model (the `password`,
  `oidc`, and `nats` methods, and the `(method, identifier)` lookup key) is **deferred**, not yet built.
- **Context:** The auth foundation slice needed exactly one working authN method to prove the capability and
  scope seams end to end. Bearer tokens are the thinnest honest cut: a service credential the bootstrap and
  the CLI can both carry. Password login is the first slice of the [identity tier epic (#27)](https://github.com/hyperscaleav/omniglass/issues/27)
  ([slice #28](https://github.com/hyperscaleav/omniglass/issues/28)), which adds `password` to the
  `credential.kind` CHECK in a new migration (never editing the applied one). OIDC and the NATS node
  credential follow with their own surfaces.
- **Closes the gap:** epic [#27](https://github.com/hyperscaleav/omniglass/issues/27).

### ADR-0005: The first owner is `omniglass bootstrap`

- **Date:** 2026-06-27 | **Status:** Resolved (identity slice 1) | **Pages:** [identity and access](/architecture/identity-access/)
- **Resolved:** `omniglass bootstrap <username> --password <pw>` shipped in identity slice 1 ([#35](https://github.com/hyperscaleav/omniglass/pull/35)): bootstrap now installs a password credential on create (plus `--email` / `--display-name`), so the owner can sign in to the console without a separate step. The `og iam` admin command namespace is still deferred (it lands with the admin user surface, slice 3).
- **Decision (divergence):** The first owner is created by `omniglass bootstrap <username>`, which mints an
  `owner@all` grant plus a **bearer** credential in one transaction. The design page describes the eventual
  `og iam create-owner --username ... --email ...` password path under an `iam` command namespace; that
  namespace and the password credential are **deferred**.
- **Context:** Bootstrap has to work before any login surface exists, so it pairs with the bearer-only
  credential decision (ADR-0004): one trusted, idempotent command that produces a token the operator pastes
  into the console or CLI. The `iam` command family (and the password-on-create path) lands with the
  identity-tier admin surfaces.
- **Closes the gap:** epic [#27](https://github.com/hyperscaleav/omniglass/issues/27).

### ADR-0006: The owner invariant is enforced by bootstrap for now

- **Date:** 2026-06-27 | **Status:** Resolved (identity slice 3c) | **Pages:** [identity and access](/architecture/identity-access/)
- **Resolved:** The `DEFERRABLE INITIALLY DEFERRED` constraint trigger (`principal_grant_owner_guard`) shipped with grant revocation ([issue #82](https://github.com/hyperscaleav/omniglass/issues/82)): it refuses to leave zero `owner @ all` grants at `COMMIT`, so revoking the last owner is a clean 409 while a swap (grant a new owner + revoke the old in one transaction) still passes. The gateway maps its custom SQLSTATE `OG001` to `ErrLastOwner`.
- **Decision (divergence):** "At least one active `owner@all` grant exists at all times" is upheld today by
  the bootstrap path (it always creates one) and the absence of any grant-revocation surface. The design's
  **deferrable Postgres constraint trigger** that enforces it at `COMMIT` (so the swap-owners-in-one-txn
  pattern works) is **not yet built**.
- **Context:** With no API to revoke a grant or delete a principal, the last-owner removal the trigger
  guards against is not yet reachable, so the trigger is not load-bearing until grant CRUD ships. It is
  required before the admin user-management slice exposes grant revocation
  ([epic #27](https://github.com/hyperscaleav/omniglass/issues/27), slice 3).
- **Closes the gap:** epic [#27](https://github.com/hyperscaleav/omniglass/issues/27).

### ADR-0007: Principals are gated at all-scope, not scope-tree

- **Date:** 2026-07-01 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** A `principal` is not a scope-tree entity: it is not "under" a location, system, or component,
  so the `principal:<action>` capability confers access **only at all-scope**. A grant scoped to a location
  or system carries no principal access, and the Storage Gateway refuses a non-all scope on the principal
  directory with a 403 (`ErrPrincipalForbidden`) rather than silently returning an empty list. This falls out
  of the scope resolver: `applicableKinds("principal")` is empty, so only an `all` grant resolves to a
  non-empty set.
- **Context:** The admin principal directory (slice 3a, [issue #77](https://github.com/hyperscaleav/omniglass/issues/77))
  is the first surface to gate on `principal:*`. Modelling users as scope-tree entities would be wrong (there
  is no "users under HQ"), and returning an empty list to a mis-scoped admin would hide a misconfiguration, so
  making all-scope explicit keeps the capability honest and surfaces the error. The same rule governs the later
  principal-mutation and grant surfaces.
- **Closes the gap:** n/a (a design decision, not a divergence).

### ADR-0008: Disable is hard revocation; no token-version column

- **Date:** 2026-07-06 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** Disabling a principal revokes its live sessions immediately, achieved by the authn path
  re-reading `principal.active` on **every** request, not by a session-version / epoch column.
  `AuthenticateBearer` and `AuthenticatePassword` both filter `and pr.active` in the credential lookup on
  every call, with no caching anywhere in the authn path, so the very next request on an already-issued
  bearer or session cookie after a disable gets zero rows and a 401. `SetPrincipalActive` flips the flag in
  one statement: disable **is** revocation, atomically. No `token_version` column is added.
- **Context:** Issue [#94](https://github.com/hyperscaleav/omniglass/issues/94) asked for "hard session
  revocation on disable", assuming disable was soft (a propagation delay). It is not: the per-request active
  check already is the hard-revocation mechanism, proven end to end by `TestDisableRevokesLiveSessionAPI` (a
  live token is 401 on its next request the moment it is disabled) and `TestDisablePrincipal`. A
  `token_version` column would matter only as an invalidation signal for an authn-result cache, which does
  not exist; adding it now would be a dead column with no reader, against the primitive-first and
  meaningful-migration disciplines. Revisit if any cache/memoization is introduced in the authn path (an
  epoch bump would then be its invalidation signal).
- **Closes the gap:** issue [#94](https://github.com/hyperscaleav/omniglass/issues/94), closed as already satisfied.

### ADR-0009: Root exclusion lives on the grant, not a new scope kind

- **Date:** 2026-07-06 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** The "act on the subtree but not the root" capability (the deploy / integrator case, issue
  [#87](https://github.com/hyperscaleav/omniglass/issues/87)) is a boolean `exclude_root` modifier on
  `principal_grant`, not a new `scope_kind` (e.g. `location_descendants`) and not a role-level flag. It narrows
  only the **modify** actions (update, delete) to the root's descendants; read and create-placement keep the
  root. An inclusive grant on the same root wins over an excluding one.
- **Context:** A new scope_kind would fork the kind handling three ways (location / system / component) and
  grow the scope vocabulary; a role-level flag could not vary per grant (the same deploy role granted
  root-inclusive in one place and root-excluded in another). The grant modifier composes with the
  additive-grant model and confines the change to one predicate (`inScopeTree`) shared by all three tree
  entities. Keeping read and create-placement inclusive means a `PATCH` on the root is the existing
  readable-but-out-of-write-scope 403, so `exclude_root` reuses the three-way status split rather than adding a
  fourth case. Shipped with a new `deploy` official role (create + update on the three tree tiers, read via the
  viewer floor). The grant-builder toggle to set it from the console is a fast-follow ([#99](https://github.com/hyperscaleav/omniglass/issues/99)).
- **Closes the gap:** issue [#87](https://github.com/hyperscaleav/omniglass/issues/87).

### ADR-0010: Impersonation is a session, not a credential; guarded by capability cover

- **Date:** 2026-07-06 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** Admin/owner impersonation ships with **both** modes (view-as read-only, act-as full) in one
  slice. An impersonation token is an `impersonation_session` row (its own table: target, real actor, mode,
  expiry, revoke), **not** a `credential` (which authenticates a principal as itself). Authorization to
  impersonate is the escalation guard `actor.Covers(target)` (the caller's capabilities must cover the
  target's) plus the `principal:impersonate` capability at all-scope. Capability cover applies to both modes;
  **scope** is where the modes diverge: **view-as** is cross-scope (read-only grants no write authority, and
  seeing another scope is the troubleshooting case), but **act-as** additionally requires the caller's
  **all-scope grants alone** to cover the target: a capability held only through a narrower grant does not
  count. Without that, act-as would let a split-grant admin (all-scope user management, but infra scoped to
  campus X) impersonate a campus-Y admin and gain write in Y, since an impersonated request resolves its ABAC
  scope from the target: a scope escalation. Because the rule is capability-cover against the caller's
  all-scope grants (not a hardcoded list of scoped resources), it closes non-tree escalation too: a user-admin
  who holds grant authority only through a scoped grant (empty effective scope, cannot create a grant directly)
  cannot launder all-scope grant authority by acting-as a grant admin. Accountability
  is a nullable `audit_log.real_actor_principal_id` written on the row directly, not reconstructed from a
  time-window join (clock skew and concurrent sessions make that unreliable for an accountability record), and
  the self-service mutations (`/auth/me` profile and password) audit too so an act-as edit is never untracked.
- **Context:** view-as is enforced by refusing every non-read action when the request carries a view-as
  claim; act-as threads the real actor through the audit writer via a request-scoped context value
  (`storage.WithRealActor`), so no mutating gateway signature changes. `authn` tries the impersonation session
  on a bearer-hash miss, so the same `Authorization: Bearer` path serves both. Disabling either party kills
  the session via the per-request `active` re-read ([ADR-0008](#adr-0008-disable-is-hard-revocation-no-token-version-column)).
  The console ships an Impersonate action (view-as / act-as) and an acting-as banner. Deferred: re-checking
  the escalation guard on every request (bounded instead by a short TTL plus revoke), and act-as within a
  scoped admin's own scope by intersecting the target's scope with the caller's ([#101](https://github.com/hyperscaleav/omniglass/issues/101)),
  rather than the current all-scope-only act-as rule.
- **Closes the gap:** issue [#85](https://github.com/hyperscaleav/omniglass/issues/85).

### ADR-0011: Grant scope is an operator, not a boolean modifier

- **Date:** 2026-07-06 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** Generalize the `exclude_root` boolean ([ADR-0009](#adr-0009-root-exclusion-lives-on-the-grant-not-a-new-scope-kind))
  into a `scope_op` operator on `principal_grant` (issue [#102](https://github.com/hyperscaleav/omniglass/issues/102)):
  `subtree` (root + descendants, the default, == old `exclude_root=false`), `subtree_excl_root` (descendants
  only for update/delete, root kept for read/create, == old `exclude_root=true`), and `self` (the root row
  only for read/update/delete, no descendants and no create-placement, a leaf-lock, net-new). The operator is a **flat enum column**, not a full predicate-expression
  tree or a per-grant tuple list. It is part of a grant's identity: the dedup index includes `scope_op`, so the
  same role at the same root with a different operator is a distinct grant.
- **Context:** Grant scope wants one composable axis, not a growing pile of booleans; the grant builder is
  already a filter-bar-style operator UI, so the operator vocabulary is the natural fit. The flat enum was
  chosen over a predicate-expression scope and a per-grant tuple list (negation, multi-root `in`): those buy
  expressiveness the boolean's two states never needed, at the cost of a much larger blast radius on the two
  authorization invariants (permission-on-every-route, scope-on-every-query). `self` is the cheap third value
  (a scalar `= any()` arm, no new recursive CTE) that turns a boolean rename into a real operator, and grant on
  exactly one node is a frequently-wanted capability the boolean could never express. The pure `scope.Resolve`
  gains a `SelfIDs` set; the three gateway walks (`inScopeTree`, `InScopeIDs`, `scopedListSQL`) gain a self arm.
  The migration also recreates the dedup index to include `scope_op`, fixing a latent collision, and threads
  `scope_op` through `RevokeGrant`'s audit SELECT (previously dropped). The operator model does **not** subsume
  the act-as scope intersection ([#101](https://github.com/hyperscaleav/omniglass/issues/101)): that blocker is
  plumbing (carry the real actor's grants and intersect two Sets per row), unchanged by how a Set is expressed.
  A future tuple model (negation, multi-root) stays a documented path if a real carve-out requirement lands.
  The console grant builder gains an operator stage (role -> kind -> entity -> operator), so [#99](https://github.com/hyperscaleav/omniglass/issues/99)
  (setting the modifier from the console) ships here too.
- **Supersedes:** [ADR-0009](#adr-0009-root-exclusion-lives-on-the-grant-not-a-new-scope-kind) (the boolean is retired for the operator).
- **Closes the gap:** issue [#102](https://github.com/hyperscaleav/omniglass/issues/102).

### ADR-0012: Owner accounts are un-impersonatable; impersonation stays capability-gated, not scope-intersected

- **Date:** 2026-07-07 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** Harden the impersonation authorization model on tiers, not scope. (1) A principal holding
  `owner @ all` cannot be impersonated by **anyone**, including another owner, in either mode (issue
  [#106](https://github.com/hyperscaleav/omniglass/issues/106)): a target-side check in the `:impersonate`
  handler, before the mode branch. (2) The `principal:impersonate` capability stays **swept by the
  `principal:*` wildcard** (admin) and `*:*` (owner); it is not carved out as a sensitive action, because
  holding `principal:*` already lets a caller create and use its own principals, so impersonate confers no new
  reach there. (3) **Drop** act-as scope intersection ([#101](https://github.com/hyperscaleav/omniglass/issues/101)):
  act-as stays all-scope-only.
- **Context:** The escalation guard (`Covers`) already blocks a lesser admin from impersonating an owner, but
  `owner.Covers(owner)` is true, so owner-impersonates-owner was possible. An owner is the highest-trust
  account and impersonating one is a full-takeover vector, so the explicit owner-protection rule removes it
  entirely and reads more clearly than relying on cover arithmetic. Owner detection reuses the same
  `role='owner' and scope_kind='all'` lane as the [owner invariant](#the-owner-invariant), so it is not new
  role-name branching. Scope intersection (a scoped admin acting-as within its own subtree by intersecting two
  scope Sets per row) was dropped as complexity for a narrow case; the tier model plus all-scope-only act-as is
  simpler and safe. The impersonated-vs-direct distinction an operator needs in the audit trail is already
  recorded by `audit_log.real_actor_principal_id` ([ADR-0010](#adr-0010-impersonation-is-a-session-not-a-credential-guarded-by-capability-cover));
  surfacing it is a later auth-event audit slice.
- **Refines:** [ADR-0010](#adr-0010-impersonation-is-a-session-not-a-credential-guarded-by-capability-cover).
- **Closes the gap:** issue [#106](https://github.com/hyperscaleav/omniglass/issues/106); closes [#101](https://github.com/hyperscaleav/omniglass/issues/101) as dropped.

### ADR-0013: A grant cannot confer capabilities the granter lacks

- **Date:** 2026-07-07 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** Grant creation is refused (403) when the granted role's capabilities are not covered by the
  granter's **all-scope** capabilities (`rbac.Set.Covers`, the same primitive as the impersonation escalation
  guard). So no caller can promote anyone, including itself, to a tier above its own: an **admin cannot grant
  `owner`** (`*:*`), because admin is an enumerated role that does not cover the global wildcard. Issue
  [#109](https://github.com/hyperscaleav/omniglass/issues/109).
- **Context:** `CreateGrant` previously checked only that the granter held all-scope `principal_grant:create`
  (`action.All`), not that the granter covered the granted role, so an admin could grant itself `owner@all` and
  log in as a superuser, leaving the admin/owner distinction unenforced. The check lives in the `create-grant`
  handler (capability is a route/handler concern; ABAC scope stays the gateway's), mirroring the impersonation
  guard. Only the caller's **all-scope** grants count, so a capability held through a narrower grant cannot be
  conferred estate-wide (the same reason act-as requires all-scope cover). The consequence is a deliberate
  stance: **admin is bounded on purpose**, the top management role, never the superuser, and does not auto-gain
  future resources; `owner` (`*:*`) is the break-glass superuser and the [owner-invariant](#the-owner-invariant)
  anchor. The same cover rule must extend to role editing when it lands (you cannot edit a role above your own
  tier); tracked with that slice.
- **Refines:** [ADR-0010](#adr-0010-impersonation-is-a-session-not-a-credential-guarded-by-capability-cover) (reuses its capability-cover primitive on the grant path).
- **Closes the gap:** issue [#109](https://github.com/hyperscaleav/omniglass/issues/109).

### ADR-0014: The audit trail is a sensitive read, not reached by a partial global wildcard

- **Date:** 2026-07-07 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** Reading the audit trail requires the `audit:read` capability, and `audit` is a **sensitive
  resource**: a partial global wildcard (`*:<action>`, e.g. the `viewer` role's `*:read`) does **not** confer
  it. Only an explicit grant on the resource (`audit:read`, held by `admin`) or the full `*:*` superuser
  wildcard (held by `owner`) reaches it. So the audit trail is admin/owner-only; a read-only user does not see
  logins, impersonations, and access changes (issue [#116](https://github.com/hyperscaleav/omniglass/issues/116)).
- **Context:** The `:read` floor and the `*:read` viewer role mean "read everything," which is right for the
  estate but wrong for the security audit trail: exposing who impersonated whom and every access change to any
  read-only operator leaks security posture. Rather than gate the route with a non-read action (a hack), `rbac`
  gains a small **sensitive-resource** set: in `Set.Allows`, a `*` resource entry that is not `allActions` skips
  a sensitive resource, so `*:read` no longer matches it while `*:*` still does and an explicit `audit:read`
  still does. This is the narrow, honest version of the "sensitive permission" idea (distinct from the
  impersonate call in [ADR-0012](#adr-0012-owner-accounts-are-un-impersonatable-impersonation-stays-capability-gated-not-scope-intersected),
  where the `principal:*` **resource** wildcard legitimately confers `principal:impersonate`; here it is the
  **global** `*:read` wildcard over a sensitive **read**). The set is extensible if other sensitive reads
  appear (it holds only `audit` today).
- **Closes the gap:** issue [#116](https://github.com/hyperscaleav/omniglass/issues/116).
- **Superseded by** [ADR-0015](#adr-0015-permissions-are-topic-patterns-single-token-and-tail-wildcards): the
  carve-out is replaced by consistent topic-pattern matching, where `:admin` is a deeper token no partial
  wildcard reaches.

### ADR-0015: Permissions are topic patterns (single-token and tail wildcards)

- **Date:** 2026-07-07 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** Permissions match like **NATS subjects** (which the node path already uses, so the stack shares
  one wildcard convention): a colon-delimited token path where a literal matches itself, **`*` matches exactly
  one token**, and **`>` matches one or more tokens and must be last**. A normal permission is
  `resource:action`; an admin-sensitive one is `resource:action:admin`. Because `*` is a single token, a
  two-token pattern (`*:read`, `*:*`, `principal:*`) structurally cannot match a three-token `:admin`
  permission: admin-sensitivity is a **deeper token**, not a special case. The whole-estate superuser is `>`
  (issue [#118](https://github.com/hyperscaleav/omniglass/issues/118)).
- **Context:** The prior ad-hoc wildcard let a two-token `*:*` match a three-token `x:y:z`, an inconsistency:
  the second `*` was silently absorbing a tail. Making matching a real topic match removes every special case,
  the [ADR-0014](#adr-0014-the-audit-trail-is-a-sensitive-read-not-reached-by-a-partial-global-wildcard)
  `sensitiveResources` set is **deleted**. `viewer`'s `*:read` misses `audit:read:admin` because two tokens
  cannot match three; `owner` reaches it via `>`; `admin` carries `audit:read:admin` explicitly. It also fixes,
  for free, a boundary wart from the [grant guard](#adr-0013-a-grant-cannot-confer-capabilities-the-granter-lacks):
  `principal:*` is now `principal:<one token>`, so it does **not** sweep an admin-tier `principal:<action>:admin`,
  those stay owner-only unless granted explicitly. `Set.Allows` matches by token; `Set.Covers` (the impersonation
  and grant-escalation guard) becomes pattern subsumption plus the `:read` floor, staying conservative (a reach
  covered only by the union of several patterns returns false, deny). The only seed change is `owner`'s `*:*`
  becoming `>`; every other permission keeps its meaning because `*` already meant a single token. A closed
  grammar also makes "what does this pattern set grant" exactly enumerable against a permission **catalog** (the
  set of all `resource:action[:admin]` the routes declare), the basis for a future custom-role preview.
- **Supersedes:** [ADR-0014](#adr-0014-the-audit-trail-is-a-sensitive-read-not-reached-by-a-partial-global-wildcard).
- **Closes the gap:** issue [#118](https://github.com/hyperscaleav/omniglass/issues/118).

### ADR-0016: A principal can be purged, and the audit trail is denormalized to survive it

- **Date:** 2026-07-09 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** A principal gains a full **lifecycle**: **disable** (reversible, the `active` flag),
  **archive** (a soft delete, `archived_at`, hidden from the directory and unable to authenticate,
  reversible), and **purge** (an irreversible hard delete of the row). Purge is **gated on prior archival**
  (archive-before-delete) and on the admin-sensitive `principal:purge:admin`, so `admin` (which carries it
  explicitly) and `owner` (`>`) can purge but a two-token `principal:*` cannot reach it
  ([ADR-0015](#adr-0015-permissions-are-topic-patterns-single-token-and-tail-wildcards)). To keep the audit
  trail through a hard delete, the actor's human-readable label is **denormalized** into every `audit_log` row
  at write time, and the audit foreign keys become `ON DELETE SET NULL`: a purge nulls the id link but the text
  survives, so "who did X" outlives the principal. The read side coalesces the live join to the snapshot.
- **Context:** [ADR-0006](#adr-0006-the-owner-invariant-is-enforced-by-bootstrap-for-now)'s single-owner invariant
  meant accounts were **disabled, never hard-deleted**, since audit rows referenced them (`RESTRICT`). But
  operators need to remove accounts created by mistake, a common task, without erasing history or orphaning the
  trail. Denormalizing the actor label decouples the audit record from the principal row, so the row can be
  purged while the history stays legible; the archive gate prevents an accidental one-click hard delete, and
  the last-active-owner guard (extended to archive) means a purgeable account is never the last owner. This
  retires the "never hard-deleted" statement in the identity-access page.
- **Naming:** the soft-delete verb was renamed **deactivate to archive** (and reactivate to **restore**) when
  the console UI landed ([#146](https://github.com/hyperscaleav/omniglass/issues/146)): "disable" and
  "deactivate" read as synonyms, blurring two distinct operations. The ladder is now a *suspend* (**disable**,
  reversible, still listed) then an *offboard* (**archive**, soft delete, hidden, recoverable) then a *destroy*
  (**purge**), so the labels read pause to remove to destroy, matching the industry suspend-vs-delete pair. The
  column, endpoints (`:archive` / `:restore`), capability (`principal:archive`), and list param
  (`include_archived`) all follow the verb.
- **Closes:** issue [#143](https://github.com/hyperscaleav/omniglass/issues/143) (backend),
  [#146](https://github.com/hyperscaleav/omniglass/issues/146) (console + rename).

### ADR-0017: `credential` is renamed `secret`; the cascade is the reuse mechanism

- **Date:** 2026-07-09 | **Status:** Accepted | **Pages:** [config, credentials, and variables](/architecture/variables/)
- **Decision:** The access-secret member of the [config / credential / variable](/architecture/variables/) trio
  is renamed **credential to secret**, and its first slice is built: a typed, encrypted-at-rest value owned on the
  exclusive arc (`global | location | system | component`) and resolved most-specific-wins down the
  [cascade](/architecture/cascade/). A secret is an **encapsulated typed cell** (a `secret_type` shape with
  per-field secrecy and origin), not a bag of references: the reuse a tool like Windmill gets from variable
  references, **the cascade already provides here** (define once at a broad scope, inherit it below), so
  composition solves a non-problem. Interpolation references live at the **consumption site** (`$sec:name.path`
  in an interface input or a function arg), never inside a secret's own fields. Crypto is **envelope AES-256-GCM**
  behind a pluggable KEK provider (env / file / fallback), the value sealed under a per-value DEK wrapped by the
  KEK, with `(owner, name, field)` bound as AAD; the provider seam lets a KMS or Vault drop in without a model
  change. "credential" is retained for the **authentication** credential (a principal's bearer or password), a
  distinct resource; only the collection-side access secret is renamed.
- **Context:** The written [variables](/architecture/variables/) page named this member `credential` and left it
  `Design`. Building it surfaced two calls. First, **naming**: "credential" collided with the identity
  credential and undersold the general case (an `snmp_community`, an API key, an `oauth2` blob are all just
  sensitive cascaded values); "secret" is the Cloudflare-style vars-and-secrets pair and reads correctly. Second,
  **shape**: Windmill's resource-references-variables split was considered and rejected, because our cascade is
  the sharing mechanism and an atomic one-form typed cell (doctrine 4) suits an operator better than composing
  references. Reveal (plaintext decrypt) ships as an audited, `secret:reveal`-gated endpoint that the `*:read`
  floor does not reach, so only admin and owner may decrypt; the interpolation consumer (splicing a value into a
  live request) is deferred to the collection-driver slice that first needs it. This reverses the `credential`
  naming and any "references inside the value" reading on the page; the `variable` and `config` members stay
  `Design`.
- **Closes:** issue [#155](https://github.com/hyperscaleav/omniglass/issues/155) (secret slice 1).

### ADR-0018: The avatar read endpoint is JSON, not raw image bytes

- **Date:** 2026-07-10 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** A human principal's profile picture is read through a **JSON** endpoint
  (`GET /principals/{id}/avatar` gated `principal:read`, `GET /auth/me/avatar` on the self lane) that returns
  `{ image_base64 }`, which the console decodes into a `data:` URL for the `<img>`. The write lanes take base64
  JSON in (`POST /principals/{id}:setAvatar` and the `/auth/me` self lane), and the server-normalized 256x256
  JPEG is stored base64 on the `human` row; the principal read models carry only a `has_avatar` bool, so no
  image payload rides a list or the `loadPrincipal` hot path.
- **Context:** The slice design spec proposed a **raw `image/jpeg`** read endpoint (with `ETag` /
  `Cache-Control` / `304`) so a browser `<img src>` could load it directly. But a raw-bytes handler would be a
  chi-native route sitting **outside** the Huma authz middleware, breaking the two-layer invariant that a
  `<resource>:<action>` capability is checked on **every** route, and a bare `<img src>` cannot send a bearer
  header, so a token-only (non-cookie) session could not authenticate the image. Keeping the read as a Huma
  JSON route puts it under the same `authn` + `require("principal","read")` (admin) or authn-only (self) path
  as every other route, and the typed client (session cookie or bearer, both work) fetches the JSON and builds
  the data URL. The one normalized size is small (roughly 30 to 50 KB base64), so per-request payload is not a
  concern, and HTTP caching over `avatar_updated_at` is a later refinement if it is ever needed. This
  supersedes the spec's raw-bytes read decision; the write transport (base64 JSON) is unchanged.

### ADR-0019: Every credential is time-bounded; token `purpose`, not expiry shape

- **Date:** 2026-07-11 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** All credentials are time-bounded (reverses the earlier tokens-never-expire choice). A
  web-login session keeps a 12h absolute lifetime; CLI/API tokens and the bootstrap token get a 90-day
  default expiry with a `--ttl` override capped at 365 days; nothing is issued without an expiry. Sessions
  and API tokens are distinguished by a `credential.purpose` column, not by whether `expires_at` is set.
  Expiry is enforced lazily at authentication; there is no background sweep, and session/token lists show
  only live credentials. Deferred: a sliding idle timeout, a housekeeping sweep of long-expired rows, and
  nearing-expiry notifications.
- **Context:** The credential-expiry slice ([#157](https://github.com/hyperscaleav/omniglass/issues/157))
  bounded only the web-login session and left the CLI/API token unbounded (`expires_at` null), overloading
  "has an expiry" to mean "is a session". That left an eternal secret in the field, against the every-secret-
  rotates principle, and coupled the session-vs-token distinction to a nullable column that both kinds now
  populate. A dedicated `purpose` column names the concept directly, so the list and the console read the
  discriminator rather than inferring it, and the default 90-day / 365-day-cap window keeps a minted token
  usable for real automation without becoming permanent. `AuthenticateBearer` already refused a passed
  expiry, so enforcement needed no change: giving tokens a future expiry is enough, and the list reuses the
  same `expires_at is null or expires_at > now()` filter so a dead row is never shown.
- **Reverses:** the tokens-never-expire behavior introduced with
  [#157](https://github.com/hyperscaleav/omniglass/issues/157).
- **Closes:** issue [#172](https://github.com/hyperscaleav/omniglass/issues/172) (self-service sessions and
  the every-credential-expires model).

### ADR-0020: `variable` slice 1 types inline and mirrors the secret arc

- **Date:** 2026-07-11 | **Status:** Accepted | **Pages:** [config, secrets, and variables](/architecture/variables/)
- **Decision:** The **variable** member of the trio ships its first slice: a typed, cascade-resolved **plaintext**
  value owned on the exclusive arc and resolved most-specific-wins down the [cascade](/architecture/cascade/), with a
  Variables directory and a per-component effective-variables panel, mirroring the [secret](#adr-0017-credential-is-renamed-secret-the-cascade-is-the-reuse-mechanism)
  member minus crypto, masking, and the reveal. `variable:create,update` is granted to **operators** (delete stays
  admin and owner), the same split secret got. Three parts of the written design are deferred to keep the slice one
  vertical cut. First, **typing is inline**: a `value_type` enum (`string | int | float | bool | json`) on the row
  plus a jsonb `value` validated against it in a pure `internal/variable` package, **not** a `variable_type` shape
  registry. A scalar needs no governed vocabulary, and the page itself calls variables the "operator-defined, not
  curated" member, so a registry would contradict the model. Second, the **`template` owner scope** (the design's
  `global -> template -> instance`) is out: slice 1 mirrors the secret arc (`global | location | system | component`),
  and template scope plus cascade groups land together in [#184](https://github.com/hyperscaleav/omniglass/issues/184),
  because they touch the shared resolver once for both members. Third, the **`$var:` consumer** and the
  **secret-flagged** variable are deferred (the consumer has no live interpolation site yet, as with `$sec:`).
- **Context:** The written [variables](/architecture/variables/) page sketched a `variable_type` registry and a
  shared config/variable cell carrying `observed_value` and `reconcile`. Building the member showed those belong to
  **config** (the declared-vs-observed member), not the free macro: a variable has no observed side. So `variable`
  shipped as its own single table, typed inline, and the page's Storage section is corrected to match. This diverges
  from the page's `variable_type`-registry and shared-cell sketch; the `config` member stays `Design`.
- **Closes:** issue [#183](https://github.com/hyperscaleav/omniglass/issues/183) (variable slice 1).

### ADR-0021: `tag` slice 1, a governed key registry with entity-update-gated bindings

- **Date:** 2026-07-12 | **Status:** Accepted | **Pages:** [tags](/architecture/tags/), [config, secrets, and variables](/architecture/variables/)
- **Decision:** The **tag** primitive ships its first slice on its own [tags](/architecture/tags/) page: the governed
  **`tag`** key vocabulary, the per-entity **`tag_binding`** value cell owned on the exclusive arc
  (`global | location | system | component`), and a resolver that unions keys and overrides values most-specific-wins
  down the [cascade](/architecture/cascade/). Two permissions, not one: **minting a key** is a tenant-wide governance
  action gated by an all-scope **`tag:create`** (broadened to `tag:*` for admin, covering update and delete of keys),
  while **setting a value** is the owner's ordinary write (`component:update` and friends), so an operator who may edit
  an entity may tag it with no new grant; a global binding, having no owning entity, is gated by `tag:update`. A key
  carries **`applies_to`** (an entity-kind allow-list, empty = universal, checked on bind) and **`propagates`** (a flag
  that toggles cascade inheritance versus a flat per-entity set, the shape a [file](/architecture/files/) will reuse).
  Key names are validated as lowercase identifiers in a pure `internal/tag` package, keeping the vocabulary normalized.
  Four parts of the written design are deferred to keep the slice one vertical cut. First, the **operator console
  surface** (a Tags directory and a per-entity tag editor) is out; the slice ships over the API and the generated CLI,
  matching the files-first ordering the estate chose. Second, binding through **[groups](/architecture/groups/)** and a
  **`template`**-scoped default are out, landing with the shared-resolver work in
  [#184](https://github.com/hyperscaleav/omniglass/issues/184) that the variable member also waits on. Third,
  **value-domain governance** (a key constraining or normalizing its values) stays the page's open question; slice 1
  ships free-text values. Fourth, binding a tag onto a **[file](/architecture/files/)** waits on the files primitive.
- **Context:** The tag design lived inside the [config, secrets, and variables](/architecture/variables/) page as the
  fourth cascade user. Building it earned tags a page of its own, because its **governance model is distinct**: unlike a
  variable (one free value, one `variable:*` permission), a tag splits a curated key vocabulary (admin-minted) from
  routine value binding (operator-open via the entity's own write), and it resolves with a **union-on-key** combinator
  rather than a single value. The exclusive-arc scope and the cascade walk are shared with the variable and secret
  resolvers; the combinator and the two-permission split are what make it its own primitive. This diverges from the
  variables page's single-table sketch (the binding is its own `tag_binding` cell) and its "bindable via groups"
  note (deferred); the variables page's tag section now frames the shared cascade and points at the tags page.
- **Closes:** issue [#188](https://github.com/hyperscaleav/omniglass/issues/188) (tag slice 1). The deferrals are
  filed: the console surface [#189](https://github.com/hyperscaleav/omniglass/issues/189), value-domain governance
  [#190](https://github.com/hyperscaleav/omniglass/issues/190), and binding onto a file
  [#191](https://github.com/hyperscaleav/omniglass/issues/191); groups and template scope ride
  [#184](https://github.com/hyperscaleav/omniglass/issues/184).

### ADR-0022: effective tags resolve onto systems and locations; a placed system inherits its location

- **Date:** 2026-07-13 | **Status:** Accepted | **Pages:** [tags](/architecture/tags/)
- **Decision:** The directory **Tags column** shows a row's **effective** (resolved-cascade) tags, not its direct
  bindings, so the list routes (`GET /components`, `/systems`, `/locations`) carry an **`effective_tags`** map (key to
  winning value, winners only) per row, resolved for the whole page in **one batched query per kind**
  (`Gateway.EffectiveTags(kind, ownerIDs)`, three per-kind recursive-CTE resolvers that thread a target id through the
  ancestor chains and rank per `(target, key)`). This required **defining effective tags for systems and locations**,
  which previously only components resolved: a **location** resolves `global` plus its own location tree; a **system**
  resolves `global`, its own system tree, **and the location it is placed at** (its `location_id` tree). A placed
  system therefore inherits its location's tags (a system in a PCI building surfaces `compliance: pci`), consistent
  with how a component picks up its own `location_id`. A component is unchanged (the full four-band arc). The resolver
  is **scopeless by contract**: the list query has already filtered the ids to the caller's read scope, so the batch
  adds no per-id check, matching the existing `rowActions` batch. Winners only in the column; provenance (which scope a
  value came from) stays in the per-entity effective-tags detail view.
- **Context:** The tag-apply UI needs each directory row to show what tags actually apply to it. The cheaper option was
  to embed a row's **direct** bindings (a flat, non-recursive `where owner_id = any($1)` lookup); the architect chose
  **effective** so the column reflects inherited values, not just locally-set ones. That choice moved real work to the
  backend (a batched recursive cascade versus a flat index scan) and forced the systems-and-locations effective
  definition, whose one genuine call was whether a **system inherits its location**: yes, because a system carries a
  `location_id` exactly as a component does, so treating it as placement-that-inherits is the consistent reading. The
  added cost is a small bounded per-row recursion over the shallow estate trees, one round-trip, and is capped by the
  directory page size. This is the first (backend) slice of the tag-apply UI; the Tags column, the type-to-add editor,
  and tag search consume it in later slices.
- **Closes:** issue [#201](https://github.com/hyperscaleav/omniglass/issues/201) (batch effective-tags resolver);
  part of [#189](https://github.com/hyperscaleav/omniglass/issues/189).

### ADR-0023: the IAM directory reads (principal, role, principal_group) are admin-tier

- **Date:** 2026-07-13 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** The **read** (list and get) of `principal`, `role`, and `principal_group` moves from a two-token
  `<resource>:read` to the admin-sensitive **`<resource>:read:admin`**, so the `viewer` read floor (`*:read`) no
  longer reaches the Users, Roles, and Groups directories. `admin` carries an explicit `principal:read:admin`,
  `role:read:admin`, and `principal_group:read:admin` alongside its `<resource>:*` wildcards, the same shape as the
  existing `principal:purge:admin`; `owner`'s `>` is unaffected. Create, update, and the lifecycle verbs stay
  two-token: they were never reachable by a non-admin, so only the directory read needed promoting. The console
  gates the three Settings tabs on the same three-token permission and the route guard reads it from the shared nav
  map, so the sidebar and the server never diverge.
- **Context:** `deploy` (an integrator or field tech) inherits `viewer`, whose `*:read` is a single-token resource
  wildcard. Because `*` matches exactly one token, `*:read` matched `principal:read`/`role:read`/`principal_group:read`,
  and the read floor shares that reach, so a field tech could enumerate every user, role, and group over the API (a
  real 200, not just a visible menu). Promoting the directory reads reuses
  [ADR-0015](/architecture/decisions/#adr-0015-permissions-are-topic-patterns-single-token-and-tail-wildcards)'s
  deeper-token rule rather than adding a matcher special case: admin-sensitivity is a third token `*` cannot reach.
  Secrets are a separate concern (an operator legitimately reads device secrets in scope), handled by a forthcoming
  slice that combines placement scope with a per-secret admin-sensitive flag; this ADR is the IAM directories only.
- **Closes:** issue [#197](https://github.com/hyperscaleav/omniglass/issues/197).

### ADR-0024: a tag key may constrain its values to an enum

- **Date:** 2026-07-13 | **Status:** Accepted | **Pages:** [tags](/architecture/tags/)
- **Decision:** A tag key gains an **`allowed_values`** set (a new `text[]` column, empty by default). Empty leaves
  the key **free-text**, unchanged; a non-empty set is the **enum** a bound value must belong to, so `environment`
  can be declared as one of `prod`, `staging`, `dev`. The **binding write enforces it**: `SetTagBinding` rejects a
  value outside a key's non-empty allowed set with a dedicated 422 (`ErrTagValueNotAllowed`), so the constraint is a
  real server gate, not a UI hint. The Tags directory create and edit forms carry a value-domain control (a checkbox
  that turns the key into an enum plus a value-list editor), and the TagAdder value stage renders a **strict dropdown**
  for an enum key. A **free** key instead offers **value autocomplete from the distinct values already bound** for it,
  through a new `GET /tags/{name}:values` read (a `select distinct value`), so an operator reaches for an existing
  value without the key having to declare a set up front. Only the enum (a string set) ships; a typed `value_type`
  (int, bool, date) and input normalization (lowercase, trim, fold) stay the page's open question.
- **Context:** The [tags](/architecture/tags/) page left value-domain governance an open question, with the enum, a
  typed value_type, and normalization all on the table. Operators asked first for the plain case, a key like
  `environment` that should only ever be one of a short list, so that shipped: a string enum on the key, enforced on
  write, with a strict picker. The distinct-in-use autocomplete is the free-key counterpart, cheap (one `select
  distinct`) and immediately useful, so the two ship together. This resolves the enum half of the page's open
  question; the value_type and normalization halves remain deferred.
- **Closes:** issue [#190](https://github.com/hyperscaleav/omniglass/issues/190) (tag value-domain governance, enum).

### ADR-0025: `secret` is a sensitive resource; a per-secret `admin_sensitive` flag flips a secret to the `:admin` tier

- **Date:** 2026-07-13 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/), [variables](/architecture/variables/)
- **Decision:** Two orthogonal axes now decide who reaches a secret. **Placement scope** (the `global`/`location`/
  `system`/`component` entity a secret attaches to on the exclusive arc) gives locality, unchanged. A new per-secret
  **`admin_sensitive` flag** gives same-scope sensitivity: when set, every action on that secret is lifted to the
  **`:admin` tier**, so a scoped two-token grant (`secret:reveal`) cannot reach it and only `admin` (`secret:>`) or
  `owner` (`>`) may see, reveal, update, delete, or create it. The flag defaults from the secret's `secret_type`
  (`secret_type.default_admin_sensitive`: an SNMP community defaults operational, an OAuth2 client secret defaults
  admin-sensitive) and the row's own value is authoritative; the column default is `true` (a secret is admin-only
  until marked operational). Enforcement is a capability flag computed at the API (`canAdmin` = the caller holds
  `secret:<action>:admin`) and passed to the Storage Gateway alongside scope: the gateway hides admin-sensitive rows
  from a lister/resolver without it, and returns a **non-disclosing 404** (not a 403) to a revealer/updater/deleter
  without it, so a platform credential's existence and field names are not disclosed through the read, reveal, list,
  or cascade paths. (One residual: because a secret name is unique per owner, an operator with create scope at the
  same owner can distinguish a create-collision 409 from a 201, a narrow existence-and-name oracle, no field values.
  It predates this slice, since operators already held `secret:create` without `secret:read`; closing it needs a
  namespace or create-path change and is a tracked follow-up, not a value-disclosure path.) Separately, `secret` joins a
  **sensitive-resource set** that a bare single-token `*` does not reach, in both places `*` grants read (the direct
  topic match and the read floor); `>` (owner), a literal `secret:read`, and a `secret:*` still name it. So
  `viewer` (only `*:read`) reads no secrets at all (not the directory, not the per-component effective-secrets
  cascade), `operator`/`deploy` gain a scoped `secret:read,reveal,create,update` and see and reveal the operational
  secrets in their subtree, and `admin`'s `secret:*` becomes `secret:>` so it reaches the admin tier. The
  `/secrets` directory, previously all-scope-only, is now scope-filtered. The client `can()` mirrors both the
  sensitive-set and the `:read` floor so the console hides exactly what the server denies.
- **Context:** A field tech setting up a site must create and read back that site's **device** secrets (an SNMP
  community, a device login), but the **platform integration** credentials (a Zoom or Microsoft client secret the
  collection engine consumes) must never be revealed below admin. A device secret and a platform credential can sit
  at the **same** scope (both global), so placement alone cannot separate them, and a low/medium/high sensitivity
  ladder was rejected as arbitrary and hard-fixed to three tiers. A per-secret binary flag reusing
  [ADR-0015](/architecture/decisions/#adr-0015-permissions-are-topic-patterns-single-token-and-tail-wildcards)'s
  third-token `:admin` rule expresses the real distinction without a new matcher concept. Taking `secret` off the
  bare `*` wildcard (rather than promoting `secret:read` wholesale to `:admin`, which would deny operators their
  device secrets) is the one lever that keeps the two-token `secret:read` operators legitimately hold while stopping
  `viewer`'s `*:read` from reaching it. Negative grants (deny-after-allow) were rejected as a footgun the `:admin`
  tier and the sensitive-set already cover. This is Slice B of the same visibility rework as
  [ADR-0023](/architecture/decisions/#adr-0023-the-iam-directory-reads-principal-role-principal_group-are-admin-tier);
  the IAM directories use the `:admin` tier (no legitimate sub-admin reader) and are not in the sensitive-set,
  `variable` stays viewer-visible by decision and is not in the set. The move of Secrets, Variables, and Config out
  of Settings into Catalog is a separate branch, not this slice.
- **Closes:** issue [#210](https://github.com/hyperscaleav/omniglass/issues/210).

### ADR-0026: Console nav IA: estate values get their own top-level group; the Settings group becomes Admin

- **Date:** 2026-07-13 | **Status:** Accepted | **Pages:** [ui](/architecture/ui/)
- **Decision:** The operator console left nav is reorganized around five genera: Catalog (the reusable,
  estate-agnostic model), Inventory (the estate instances: locations, systems, components, and nodes), Values
  (the operator-set values resolved down the scope cascade: variables, secrets, config), the observed surfaces
  (Explore, Alarms, Dashboards, Learn), and platform Admin. Secrets, Variables, and Config are values operators
  set on estate entities, so they move from the Settings menu into a **Values** group of their own, standing
  beside Inventory rather than nested inside it as a band. Config's meaning is fixed as the **CI store**:
  operator-set desired component and system configuration, optionally observed back from the device to detect
  drift and reconcile, distinct from platform Settings and from Variables. Inventory gains **Nodes** (the
  collection daemons, a monitored, scope-controlled entity, ungated "soon" until `node:read` lands) alongside
  Locations, Systems, and Components; Interfaces and Tasks are dropped from the nav entirely, since an interface
  is a facet of a component and a task a facet of a node, not a directory of their own. The Settings group is
  renamed **Admin** (Users, Roles, Groups, Audit) and gains an ungated "soon" Settings leaf that reserves the
  platform-settings-table page.
- **Context:** Settings had become a junk drawer mixing platform governance, platform config, and estate-attached
  values. Those three values attach to a single estate entity on the scope cascade (the same genus as a tag
  assignment) but are not estate entities themselves, so they earned a home of their own, not Settings, not
  Catalog, and not a band folded inside Inventory. This **supersedes** the "into Catalog" line of ADR-0025 above:
  the earlier same-day plan named Catalog, and the decision is a dedicated Values group. Interfaces and Nodes were
  first sketched as Inventory children alongside the estate entities; Nodes stayed (a node is monitored and
  scope-controlled exactly like a location, system, or component, so it belongs with them, not under Admin), but
  Interfaces and the Tasks a node runs were cut from the nav once it was clear each is a facet of one owning
  entity's detail page (a component's device endpoints, a node's collection assignments), not a set an operator
  browses on its own. The relaxed whole-group-drop (an ungated Settings "soon" stub keeps the Admin group visible
  to a viewer, showing only that greyed placeholder while every data-bearing child stays admin-gated and hidden)
  is deliberate until the platform-settings backend ships and the leaf is gated on `setting:read:admin`. Design:
  `docs/superpowers/specs/2026-07-13-operator-console-nav-ia-design.md`.
- **Closes:** issue [#222](https://github.com/hyperscaleav/omniglass/issues/222).
- **Update (2026-07-14):** **Files** joins the **Values** group. The files slice ([ADR-0029](#adr-0029-files-slice-1-a-content-addressed-blob-store-and-a-tenant-wide-file-handle)) first shipped the Files directory under Inventory, but a file is not part of the monitored estate (no health, not polled): it is operator-uploaded **content**. So the Values group broadens from "operator-set values resolved down the cascade" to **operator-set values and content**, with the (deliberately non-cascading, flat) file as its content member alongside the cascaded variables, secrets, and config ([#249](https://github.com/hyperscaleav/omniglass/issues/249)).

### ADR-0027: create is a route; inventory create and edit unify on the detail accordion

- **Date:** 2026-07-14 | **Status:** Accepted | **Pages:** [design system](/contributing/design-system/), [core entities](/architecture/core-entities/)
- **Decision:** The inventory entities (component, system, location) drop the create/edit **Drawer**. Creating one
  is now a **route**: `New` navigates to `/<entity>/create`, a **draft accordion** where Identity and Placement are
  writable and the binding sections (Tags, and later Secrets/Variables) are shown locked until the entity exists;
  **Save** commits the row and hands off to `/<entity>/<id>` in **edit mode** (a one-shot pending-edit flag consumed
  when the detail resolves, the Users `openPrincipalInEdit` pattern). The detail is one accordion, **read-only in
  view and the sole writer in edit**: no in-body field or binding mutation control renders while not editing (the
  footer's Edit / Delete chrome and the read-only effective-secrets/variables panels are exempt). This is the Users
  inline-blade-edit model generalised to inventory, and it holds on **both** the docked blade and the addressable
  full page. No new routes: the static `/create` outranks `/:name` in the router, so `create` is a reserved segment.
  The shared `TreeList` primitive gains a per-surface **edit slot on `ListCtx`** (the full page makes its own slot,
  since the shared `renderDetail` must not call `useBladeEdit` outside a blade provider), plus `renderCreate` /
  `onNew` / `onEdit` hooks and an optional `FormBody`, so a page opts into the model without breaking the others.
- **Context:** Creating an inventory entity returned you to the list, so setting a tag meant find, reopen, edit; and
  `TagAdder` rendered a write control in view. A drawer that opened in edit after create would need a fragile
  cross-surface hand-off (the code-grounded review of the drawer design surfaced a full-page `useBladeEdit` crash, a
  `FormBody` footer collision, and a pending-edit gap). Framing create as its own URL dissolved the "create is
  blade-only" false dilemma: a draft with an address is deep-linkable full-page and dockable as a blade, and Save is
  a route hand-off, not a surface hop. Own-field edits commit on Save (Cancel reverts them); tag bindings keep their
  immediate per-binding write, so Cancel does not roll a tag back, and the tag control sits apart from the Save/Cancel
  form. Slice 2 (a shared cross-page form shell) and slice 3 (moving Users onto it) are deferred.
- **Closes:** issue [#231](https://github.com/hyperscaleav/omniglass/issues/231).

### ADR-0028: `rank` retired from the type registries; sort is alphabetical

- **Date:** 2026-07-14 | **Status:** Accepted | **Pages:** [core-entities](/architecture/core-entities/), [Types guide](/guides/admin/types/)
- **Decision:** `rank` is dropped from `location_type`, `system_type`, and `component_type`: the
  column (a new idempotent migration), the three API bodies and create/update inputs, the
  boot-seed YAMLs, the generated client and CLI, and the Types catalog page (no Rank column, no
  Rank field on create or edit). `ListLocationTypes`, `ListSystemTypes`, and `ListComponentTypes`
  now order by `display_name, id` instead.
- **Context:** `rank` was sort-only from the start (the location_type seed comment already said
  so: "rank does NOT constrain nesting"), never an enforcement mechanism. The upcoming
  `allowed_parent_types` placement constraint on `location_type` needed a clean field to
  introduce without a stale, unused ordering column sitting beside it, so retiring `rank` is the
  mechanical precursor to that slice rather than part of it: this PR only removes the field and
  switches the sort, `allowed_parent_types` is a separate slice. Alphabetical is the obvious
  default with no enforcement semantics to preserve; an operator who wants a specific browse
  order can still rely on the id or display name they chose.
- **Closes:** part of issue [#239](https://github.com/hyperscaleav/omniglass/issues/239) (the
  `allowed_parent_types` half continues in a follow-up PR against the same issue). Design:
  `docs/superpowers/specs/2026-07-14-type-placement-constraints-design.md`.

### ADR-0029: files slice 1, a content-addressed blob store and a tenant-wide file handle

- **Date:** 2026-07-14 | **Status:** Accepted | **Pages:** [files and blobs](/architecture/files/), [storage](/architecture/storage/), [identity and access](/architecture/identity-access/)
- **Decision:** The files subsystem ships its first slice: a content-addressed **`blob`** store as a Storage
  Gateway primitive (a `blob.Store` seam, default **pgblobs** backend holding bytes inline, keyed by the sha256
  of the bytes, dedup via `on conflict do nothing`, integrity-verified on read), and a **`file`** handle,
  searchable metadata (name, content_type, size, sha256, sensitive) that points at a blob by hash, with CRUD
  over the API, the generated CLI, and the typed client, plus the Files directory (under Values; see the
  [ADR-0026 update](#adr-0026-console-nav-ia-estate-values-get-their-own-top-level-group-the-settings-group-becomes-admin)). Four calls
  shape it. **(1) No placement arc on a file.** A file is tenant-wide, not on the exclusive arc a secret sits on,
  because a file relates **1:many** (to entities and types) rather than 1:1; that locality is a future
  many-to-many **attachment**, not an owner column, so the gateway injects no ABAC tree scope on a file query.
  (This reverses an in-design proposal to give `file` a secret-style owner scope.) **(2) Sensitivity reuses the
  secret mechanism, binary, defaulting off.** A per-file `sensitive` flag reuses
  [ADR-0025](#adr-0025-secret-is-a-sensitive-resource-a-per-secret-admin_sensitive-flag-flips-a-secret-to-the-admin-tier)'s
  `:admin`-tier rule (hidden from a lister without the tier, a non-disclosing 404 to a reader without it,
  admin-only to create), but defaults **false** (a file is shared unless marked, where a secret defaults sensitive
  because it is a credential), and `file` is **not** added to the sensitive-resource set, so the viewer floor
  (`*:read`) reads ordinary files. **(3) A delete frees its blob synchronously; async GC deferred.** `DeleteFile`
  drops the handle and, in the same transaction, frees the blob **when no other handle references it** (a dedup-aware
  refcount: a deleted file reclaims its bytes rather than leaking storage, but a blob shared by another handle is
  kept). The general async mark-sweep GC (for blobs referenced by other things, an aged large log body, a
  `collection.failed` raw, an attach event, none of which exist yet) stays a later slice; today a `file` is the only
  referencer, so the synchronous check is complete. **(4) One backend, base64-in-JSON on the wire.** Only the pgblobs backend ships (S3 and disk
  behind the same seam later); upload and download carry the bytes **base64 in JSON**, reusing the avatar precedent
  ([ADR-0018](#adr-0018-the-avatar-read-endpoint-is-json-not-raw-image-bytes)) so the whole surface stays under the
  Huma authz middleware and generates a uniform client and CLI. content_type lives on the **file**, not the blob:
  content-addressing is about the bytes, so identical bytes are one blob regardless of declared type.
- **Context:** [files.md](/architecture/files/) specified the two-layer model (handle plus content-addressed blob)
  and an index-probe GC; its open questions (inline-versus-blob threshold, chunking, the grace floor) are untouched
  here. The **1:many** insight is what separated a file's *locality* (attachment, deferred) from its *access*
  (permission plus sensitivity), and is why the file does not copy the secret owner arc. A full
  **classification + clearance** lattice (an ordered ladder on the resource, a clearance on the principal, an
  external-principal class) was considered for the sensitive axis and split into
  [its own epic (#243)](https://github.com/hyperscaleav/omniglass/issues/243) rather than inflating this slice; the
  binary flag is a 2-rung subset it will subsume. Multipart streaming for very large blobs is deferred with the
  S3/chunking slice.
- **Divergences logged:** files.md moved `content_type` from the blob to the file; the in-design file owner/scope
  arc was dropped (a file is off the placement arc). Both are reflected in the page.
- **Lands:** [epic #242](https://github.com/hyperscaleav/omniglass/issues/242), [#244](https://github.com/hyperscaleav/omniglass/issues/244).

### ADR-0030: `allowed_parent_types` constrains where a location may be placed

- **Date:** 2026-07-14 | **Status:** Accepted | **Pages:** [core-entities](/architecture/core-entities/), [Types guide](/guides/admin/types/), [Work with an entity](/guides/operator/entities/)
- **Decision:** `location_type` gains `allowed_parent_types` (`text[]`, default `{}`): a set whose
  members are `location_type` ids and/or the reserved `root` sentinel (a placement at the top,
  no parent). An empty set is unconstrained (the default, and every existing custom type until an
  operator opts in); a non-empty set is enforced: a placement is valid iff the parent is null and
  the set contains `root`, or the parent location's type is in the set. `root` cannot collide with
  a real type id: `CreateLocationType` refuses it. Enforcement is forward-only, on `CreateLocation`
  and the location move path (`UpdateLocation`'s new `ParentName` patch field, added this slice so
  the "grandfathered until moved" guarantee is real and testable, not merely a claim); an existing
  placement a type's set no longer allows is untouched until something tries to move it. The four
  seeded types get their sets: `campus={root}`, `building={root,campus}`,
  `floor={building,campus}`, `room={floor,building,campus}`. Re-parent ships operator-usable this
  slice: the location edit form's Placement section makes Parent editable, a picker built on
  #240's inventory edit model (the same `Show when={editing()}` field/fact split every other
  editable field on the accordion uses), narrowed to the set and excluding the location's own
  subtree; moving back to root is not offered (the move primitive does not support it this slice).
- **Context:** `rank` ([ADR-0028](/architecture/decisions/#adr-0028-rank-retired-from-the-type-registries-sort-is-alphabetical))
  was sort-only and never expressed the estate's real hierarchy rule (a floor does not belong
  above a room). A `child.level > parent.level` rule was rejected: it does not generalize past
  locations (systems and components have no total order), while a type-level allowed-parent set
  expresses both the general "may skip a level" case and the specific "may never be root" or
  "may never nest under this particular type" cases with one field. A separate `root_placeable`
  boolean was rejected in favor of folding root into the set as a sentinel, keeping one field and
  one validation path. Enforcing retroactively was rejected: seeding a type's set must never
  invalidate an existing estate. Locations had no move or re-parent capability at all before this
  slice (create-time placement only); the storage/API primitive was originally scoped without a UI
  trigger (the console's placement fields render read-only in every edit context today, on all
  three inventory pages), but the decision changed once #240's create-as-route edit model landed
  as the concrete field pattern to hang a reparent picker off: one PR ships the enforcement point
  and a real way to use it, rather than a primitive an operator cannot reach. The picker's
  candidate list is narrowed client-side (a UX nicety); the server-side `validatePlacement` call
  is the actual gate, so a stale or bypassed client filter still gets an inline 422, not a
  silently-accepted violation. One divergence from the design surfaced while building the move
  primitive: `UpdateLocation` checks placement before the cycle guard, not after, so a move that is
  simultaneously a placement violation and a structural cycle (moving a location under its own
  descendant, where the descendant's type also does not allow this child) reports the `PlacementError`
  (422, naming both types) rather than the generic `ErrLocationCycle`; the design left the check
  order unstated, and the more specific, actionable error was chosen to win. Systems and components
  lose `rank` too but get no `allowed_parent_types` this slice, and keep their existing
  read-only-in-edit Parent field: a leaf or must-nest constraint there is closer to a boolean than
  an ordered set, deferred until a concrete need names the shape, and extending the same
  editable-Parent pattern to two more pages is a follow-up, not bundled here.
- **Closes:** issue [#239](https://github.com/hyperscaleav/omniglass/issues/239). Design:
  `docs/superpowers/specs/2026-07-14-type-placement-constraints-design.md`.

### ADR-0031: `component_make` registry slice 1, an `official` boolean, a deferred referential guard, and website scheme validation

- **Date:** 2026-07-14 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/), [Vendors guide](/guides/admin/vendors/)
- **Decision:** Three calls on the first slice of the `component_make` manufacturer registry (id,
  display_name, icon, support_phone, website), lands ahead of the rest of the make/model catalog.
  **(1) `official boolean`, not an `origin` enum.** The design sketch (below) proposed
  `origin official | seed | custom` on make and model, matching the model layer's eventual needs.
  Slice 1 ships a plain `official` boolean instead, because `component_type` and the other
  registries already distinguish seed-owned from operator rows with a boolean, and a
  two-value distinction gains nothing from a three-value enum until a real `seed` (installed,
  mutable) tier exists to fill it; `origin` can still land on `component_model` if that tier turns
  out to be real. **(2) The in-use / referential delete guard is deferred.** `component_type`,
  `location_type`, and `system_type` all refuse a delete while a location, system, or component
  still references the row (409). `component_make` ships **no equivalent guard**: nothing
  references a `component_make` yet (`component_model`, the referencing entity, does not exist),
  so a custom make deletes unconditionally (an official row is still refused, 422, the seed-owned
  rule). The guard is added when `component_model` lands and gives the registry something to be
  in-use by, rather than building an unused check now. **(3) Website URL scheme validation, client
  and server.** The create/edit form renders `website` as a live anchor; an operator-entered value
  with no scheme check is a stored-XSS vector (`javascript:`/`data:` executing on click). A
  `validWebsiteScheme` guard on the API (`http`/`https` only, empty allowed, else 422) and a
  matching `safeUrl` guard on the console (render a live link only when safe, else plain text,
  never a dead or unsafe anchor) close it in both places: server-side so a non-browser caller
  (CLI/curl) cannot persist a dangerous scheme, client-side so a value written before the
  server-side check existed (or by any path that bypassed it) still renders safely.
- **Context:** `docs/superpowers/specs/2026-07-14-component-make-model-catalog-design.md` sketches
  the full make/model catalog (`component_make`, a `component_type` genus tree, `component_model`,
  and `component.model_id`) as four independent vertical slices; this is slice 1, make alone, with
  no dependency on the tree or the model layer. A review pass on the first cut of the console page
  (Task 4) found the missing website-scheme check as a stored-XSS gap before this shipped, closed
  in the same slice rather than carried as a follow-up.
- **Divergences logged:** the design sketch's `origin official | seed | custom` enum is not what
  shipped; `official boolean` did, per (1) above. The design's delete-refused-while-referenced rule
  is not enforced yet; per (2), it is deferred to the `component_model` slice that gives it
  something to check.
- **Lands:** [epic #254](https://github.com/hyperscaleav/omniglass/issues/254), issue
  [#255](https://github.com/hyperscaleav/omniglass/issues/255). Design:
  `docs/superpowers/specs/2026-07-14-component-make-model-catalog-design.md`. Plan:
  `docs/superpowers/plans/2026-07-14-component-make-registry.md`.

### ADR-0032: the required permission is published per route, and the permission universe is route-derived

- **Date:** 2026-07-17 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/), [API](/architecture/api/), [Access guide](/guides/admin/access/)
- **Decision:** Every capability-gated route registers through one helper, `gated(op, tokens...)`,
  which sets the `authn` + `require` middleware (unchanged enforcement), stamps the operation with
  an `x-omniglass-permission` OpenAPI extension, and records the permission in an in-process
  registry. The required permission for each request is therefore **published in the generated
  `api/openapi.json`**, and the **permission universe** (the deduped set of every stamp) is
  **derived from the routes**, not a hand-kept catalog. `GET /roles` reports the universe plus, per
  role, the **held** subset (resolved by the same `rbac.Set.Allows` matcher as the effective set),
  and the console role blade renders it as a net `Held / Missing / All` view. Two build-time guards
  keep it honest: a **published-gate guard** (every gated route is stamped, allow-listed routes are
  not, so "gated" and "published" are the same set) and a **seed-drift guard** (every seed-role
  grant resolves into the universe or sits in an explicit `aheadOfRoutes` allow-list).
- **Context:** the authz contract already existed (`require(...)` enforced a permission on every
  route) but lived only in Go middleware, invisible to the spec, the clients, and any reader; and
  a role blade could show only what a role granted, never the capabilities it lacked. Three options
  were weighed for the universe source: a hand-kept catalog YAML (drifts), a runtime-only set
  (invisible in diffs), and the route-derived stamp (self-maintaining, reviewable in the committed
  spec, exactly the enforced surface). The stamp won because it makes the universe fall out of the
  API-first pipeline with no second source to drift. Held is resolved server-side so the single
  rbac matcher is not duplicated in the SPA. Grants that resolve to nothing (for example
  `alarm:*`, `interface:*` before those subsystems have HTTP surfaces) are legitimate but ahead of
  their routes; they show as held-nothing and are allow-listed until the route lands.
- **Lands:** issue [#272](https://github.com/hyperscaleav/omniglass/issues/272) under epic
  [#27](https://github.com/hyperscaleav/omniglass/issues/27). Design:
  `docs/superpowers/specs/2026-07-17-net-permissions-role-blade-design.md`.

### ADR-0033: settings persist only the override level; base layers are recomputed in memory

- **Date:** 2026-07-17 | **Status:** Accepted | **Pages:** [settings](/architecture/settings/), [scaling and deployment](/architecture/scaling/)
- **Decision:** The settings engine stores **only the override level** in Postgres (`setting_override`). The two
  base layers, the embedded `code` defaults and the operator `file`, are **recomputed into memory on every boot**
  and never written to the table, so the effective document is the in-memory base layers merged with the live DB
  override. Restore is therefore a `DELETE`: dropping a namespace's override row (or truncating the scope)
  re-exposes the base defaults, with no separate reset column and no re-seed of the file into the store.
- **Context:** The [scaling](/architecture/scaling/) page sketched a single settings store "materialized in
  Postgres ... seeded declaratively from a settings file reconciled on every boot" (an `ON CONFLICT DO UPDATE` of
  the file into the table). Building the engine showed that materializing the file into the DB is the wrong shape:
  it duplicates the GitOps source into a second authoritative copy that can drift, and it conflates ship-with
  defaults (a compile-time asset) with operator changes (the only thing worth persisting). Keeping the base layers
  in memory makes the file always-fresh (a ConfigMap change lands on restart), keeps the store lean (it holds only
  what an operator actually changed), and makes restore fall out of the model as a delete rather than a re-seed.
  This diverges from the scaling page's "materialized in Postgres" wording; the settings page carries the corrected
  model and the scaling page moves to `Partial`.
- **Closes:** issue [#271](https://github.com/hyperscaleav/omniglass/issues/271) (settings engine slice-0), under
  epic [#270](https://github.com/hyperscaleav/omniglass/issues/270). Design:
  `docs/superpowers/specs/2026-07-17-settings-engine-design.md`.
- **Amended by [ADR-0057](#adr-0057-the-cascades-least-specific-tier-is-platform-and-a-default-is-not-a-tier):** the model is unchanged, the level names are not: `code` is now `default` (off the axis, the setting's own declaration) and `global` is now `platform` (the install-wide rung).

### ADR-0034: the settings Gateway is unscoped; only the permission gates it

- **Date:** 2026-07-17 | **Status:** Accepted | **Pages:** [settings](/architecture/settings/), [storage](/architecture/storage/), [identity and access](/architecture/identity-access/)
- **Decision:** The Storage Gateway methods for settings (`GetSettingOverrides`, `UpsertSettingOverride`,
  `DeleteSettingOverride`, `DeleteAllSettingOverrides`) are **unscoped**: no ABAC storage-scope predicate is
  injected. Only the `settings:<action>` permission at the route gates them (`settings:read` admin read with
  provenance, `settings:update` write / restore / lock, both admin-tier; the client-safe `/settings/me` is
  authn-only). This is a deliberate carve-out from the "scope on every applicable query" invariant, recorded so it
  reads as intentional.
- **Context:** The two authorization layers ([identity and access](/architecture/identity-access/)) are a
  `<resource>:<action>` permission on every route and an ABAC **scope** injected on every **applicable** query.
  Platform and cascade settings describe the **platform and its principals**, not the estate, so there is no
  location / system / component subtree to scope them by, exactly as with the registry-type reads
  (`GET /types/...`), which are also unscoped. Forcing a scope predicate here would be meaningless (there is
  nothing to filter on) and would misrepresent settings as estate data. The carve-out is narrow: it applies only
  because the data is platform config. When the group and user override rungs land, override reads and writes
  **will** be constrained by the acting principal (a user edits only their own `user` row), but that is a
  per-principal ownership check, a different mechanism than estate ABAC, not a return of tree scope.
- **Closes:** issue [#271](https://github.com/hyperscaleav/omniglass/issues/271) (settings engine slice-0).
- **Amended by [ADR-0057](#adr-0057-the-cascades-least-specific-tier-is-platform-and-a-default-is-not-a-tier):** the model is unchanged, the level names are not: `code` is now `default` (off the axis, the setting's own declaration) and `global` is now `platform` (the install-wide rung).

### ADR-0035: settings resolve as a cascade over principals with a broader-wins lock

- **Date:** 2026-07-17 | **Status:** Accepted | **Pages:** [settings](/architecture/settings/), [cascade](/architecture/cascade/)
- **Decision:** A setting's effective value resolves down the **principal** hierarchy (global to group to user),
  reusing the same [cascade](/architecture/cascade/) primitive the estate uses down location to system to
  component: ordered layers deep-merged in JSON map-space (most-specific-wins by key presence), with per-key
  **provenance** (the winning level) reported alongside the value. Layered on top is a **top-down lock**: an admin
  locks a key at a level, pinning that level's value and forbidding any more-specific level from overriding it, and
  when two levels lock the same key the **broader level wins** (a `global` lock supersedes a `group` lock, so
  top-down admin authority is absolute). Slice-0 ships the global rung; group and user are a fast-follow.
- **Context:** Omniglass already had one cascade resolver (the estate's secrets / variables / tags / config,
  [config and credentials](/architecture/variables/)). Rather than write a second resolver for settings, the engine
  points the same primitive at the identity axis (doctrine 5, primitive-first): a value defined once at a broad
  scope inherits below, which is exactly the reuse a variable-reference model (Windmill-style) would buy, provided
  here by inheritance for free. The **lock** is the piece the estate cascade did not need: settings are governance
  (an admin enforcing an org default a user cannot escape), so the engine adds a per-key lock with a broader-wins
  conflict rule, the inverse of the most-specific-wins value rule, applied to the enforcement axis. Provenance
  reuses the estate's effective-values vocabulary (the winning level per key), extended from three estate bands to
  five principal levels plus a lock chip. The pure `settings` package is the primary unit-test target; the DB
  override is supplied through a narrow function seam so the package never imports storage.
- **Closes:** issue [#271](https://github.com/hyperscaleav/omniglass/issues/271) (settings engine slice-0), under
  epic [#270](https://github.com/hyperscaleav/omniglass/issues/270).
- **Amended by [ADR-0057](#adr-0057-the-cascades-least-specific-tier-is-platform-and-a-default-is-not-a-tier):** the model is unchanged, the level names are not: `code` is now `default` (off the axis, the setting's own declaration) and `global` is now `platform` (the install-wide rung).

### ADR-0036: A node is a kind=node principal with an interim bearer credential and static per-connection NATS subject permissions

- **Date:** 2026-07-07 | **Status:** Accepted | **Pages:** [nodes](/architecture/nodes/), [identity and access](/architecture/identity-access/)
- **Decision:** A node is a first-class `principal` of `kind='node'` with a 1:1 `node` detail table (keyed by
  `principal_id`, alongside `human` and `service`), exactly as [identity and access](/architecture/identity-access/)
  describes. Its `name` is `not null unique` on the detail table and stays the estate address the collection FKs
  (`interface.node_name`, `task.node_name`, `metric_datapoint.node_id`) reference. The node runtime ships with
  two deliberate calls that diverge from the present-tense design, both reversible in a later hardening slice.
  (1) The node's credential is a **bearer `credential` row** on its principal, minted, stored (only as
  `sha256`), and verified through the **same helpers a service bearer token uses** (`AuthenticateBearer`), and
  the enrollment token **doubles as the node's NATS password** (a shared secret), rather than being a single-use
  bootstrap exchanged for a distinct long-lived credential. The decentralized **nkey/JWT operator-account**
  model that identity and access describes for nodes (a `nats` credential kind, a signed nonce, a JWT carrying
  the node's subject permissions) is deferred; the `credential` kind CHECK is **not** widened for it here. (2)
  Per-node NATS isolation is **static per-connection subject permissions**: the embedded `nats-server` runs an
  in-process `CustomClientAuthentication` callback that resolves each connecting node by name, verifies its
  bearer credential, and registers a user whose publish/subscribe grants are scoped to that node's own
  `og.v1.*.<node>` subjects, so a node cannot publish or pull as another.
- **Context:** Checkpoint 2 of the reachability slice needed a real, negatively-tested per-node isolation
  mechanic against an embedded server, without carrying the full JWT/nkey machinery a single slice should not.
  The auth-callback path adds per-node users **dynamically at enrollment time with no config reload**, which is
  the simplest mechanism that keeps the isolation invariant real: the negative test proves node A cannot use
  node B's subjects (and a confused-deputy reply cannot forge another node's liveness), and a wrong credential
  is rejected at connect. The subject encodes the node name in its last token and the callback grants only that
  node's subjects, so the subject **is** the transport isolation boundary (the payload-owner admission fence is
  a later checkpoint). Modeling the node as a `kind=node` principal (rather than the standalone table an earlier
  checkpoint built) puts it on the shared identity spine from the start: it has a real `principal_id` so it can
  be an audit actor, its credential rides the audited human/service machinery, and only the credential *scheme*
  (interim bearer vs nkey/JWT) remains to tighten. JetStream is enabled on the server now (it boots and shuts
  down cleanly), but the control-plane messages (worklist, heartbeat) are JSON over core NATS; the protobuf
  telemetry `Event` over JetStream is the next checkpoint.
- **Closes the gap:** the nkey/JWT node identity (the `nats` credential kind and the signed-nonce admission)
  and the single-use enrollment token are tracked with the node-identity hardening slice.

### ADR-0037: Telemetry is a protobuf Event over JetStream with an inline owner-confining consumer

- **Date:** 2026-07-07 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [datapoints](/architecture/properties/)
- **Decision:** A node ships each collected batch as a protobuf `Event` (proto3, `proto/og/v1/event.proto`, since renamed to `TelemetryBatch` in `proto/og/v1/telemetry.proto`, #424,
  `Event` + `Datapoint` messages only, no gRPC service) published to `og.v1.telemetry.<node>`. This is
  omniglass's first protobuf; the wire is generated with `protoc` + `protoc-gen-go` via a `gen-proto` stage on
  `make gen`, and the generated `event.pb.go` is committed. The server hosts a JetStream stream
  (`OG_TELEMETRY` over `og.v1.telemetry.*`) and a **single durable consumer** (`og-telemetry-worker`,
  AckExplicit) whose handler, per Event, **derives and writes inline**: it decodes the batch, resolves the
  owner as the task's interface component, **confines** the node to its own tasks, applies reject-not-project
  against the `datapoint_type` registry, writes the surviving typed rows through the checkpoint-1
  `InsertMetricDatapoints` path (`owner_kind=component`, `provenance=observed`), and acks. A permanent
  condition (an undecodable payload, or an orphan the confinement fence drops) is terminated/acked so it is not
  redelivered; only a transient failure (a DB error) is left unacked so JetStream redelivers. **The node stamps
  no component identity**: its only assertion is the publishing subject (its own name) plus the `task_id`; the
  server binds and confines.
- **Context:** The prior (v2) design split telemetry into a hot path that persisted a raw event to a
  `telemetry` table and an async Postgres queue worker that derived from it. Checkpoint 3 deliberately
  **collapses that split**: the JetStream durable consumer **is** the at-least-once worklist, so there is no
  raw-telemetry table and no Postgres queue in this checkpoint; the handler derives, confines, writes, and acks
  in one place. This keeps the reachability slice small while keeping the two invariants **real and negatively
  tested**: a node cannot land a datapoint for a component it holds no task for (an Event carrying another
  node's `task_id` is orphan-dropped, no row written), and an unregistered datapoint name is dropped, not
  projected. Owner binding is the **interface-prebind path only** (task -> interface -> component); there is no
  separately-authored `transform_rule` (omniglass has none), so label-based multi-owner routing, discovery
  rules, and node-self binding are a later checkpoint.
- **Closes the gap:** raw-`Event` persistence (backfill/replay) and the raw -> admission -> trusted two-lane
  topology, plus label-based multi-owner resolution, are tracked with a later collection checkpoint.

### ADR-0038: The reachability verdict is a built-in state

- **Date:** 2026-07-07 | **Status:** Accepted | **Pages:** [datapoints](/architecture/properties/), [collection](/architecture/collection/)
- **Decision:** The per-interface reachability verdict `interface.reachable` (value domain `up` / `down`) is a
  first-class **state** datapoint, not a metric, seeded as an official `datapoint_type` at `kind=state`,
  `value_type=text`, `validation: {values:[up,down]}`. It is gated **per interface**: the verdict is the AND of
  that interface's applicable probe results (for the inline tcp/icmp interfaces this is degenerate, one probe
  drives the verdict; it generalizes to an interface with several probes). The **node** computes it after running
  the interface's probe(s) and emits it as an `observed` state datapoint instanced by the interface; the ingest
  consumer **routes by the registry kind** (a metric name to `metric_datapoint`, a state name to
  `state_datapoint`) after the same owner-confinement and reject-not-project, so a foreign or unregistered state
  is dropped identically to a metric. The series is **transition-only**: the node remembers the last verdict per
  interface and emits only on a flip or first observation, and the ingest side re-guards by skipping a write whose
  value equals the latest stored value (the net for a node restart). Availability is `time_in_state` over this
  state (health's primitive one tier down), a later slice; the raw probe metrics (`tcp.open`, `icmp.reachable`,
  the rtts) keep emitting unchanged. Readiness config (an ssh command + regex, an snmp OID) is an
  **interface-type default, interface-overridable** concern executed **on the node**, not a server-side
  `calc_rule`; 5a builds no readiness-config column, its verdict is the inline probe result.
- **Context:** Reachability history is only honest if the verdict is a **dwell-measurable** signal: availability
  is time-in-state, which needs a categorical state with transitions, not a numeric sample per tick. Modelling
  the verdict as a metric would conflate the raw per-probe reading (`tcp.open`, a firehose sample) with the
  interface-level judgement (an availability substrate), and it would make `time_in_state` a re-derivation over a
  numeric series rather than a read over the state's own transitions. Making it a state, and computing it at the
  node as the AND of the interface's probes, keeps the verdict where the probe results are, keeps the raw metrics
  untouched, and lets the read side reconstruct the availability strip directly from `state_datapoint`.
- **Divergence:** checkpoint 1 seeded the `datapoint_type` canon **metric-only** (the reachability probe metrics),
  and cp3's ingest consumer assumed every surviving datapoint was a metric (`InsertMetricDatapoints` for all).
  This entry records the divergence: 5a adds the first **state** to the seed and makes the ingest consumer route
  by kind (the cp3-deferred "route by kind, not assume metric" note now come due). The `state_datapoint` table
  mirrors `metric_datapoint` (same owner exclusive-arc, same lineage CHECK) with a categorical `value text` plus
  an optional `value_json`.
- **Closes the gap:** the availability SLI (`time_in_state` over `interface.reachable`) and the operator surfaces
  that render the transitions are a later slice (5b); readiness config as an interface-type default is a later
  interface-type concern.

### ADR-0039: An interface is a device API; the interface type is its transport, not its driver

- **Date:** 2026-07-08 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [nodes](/architecture/nodes/)
- **Decision:** An `interface` is an **API endpoint we intend to call** on a component, identified by the
  **protocol it speaks** (`web`, `qrc`, `ttp`, `snmp`), not a network interface; a host or IP is a variable it
  consumes, not its identity. It is named by that protocol and is unique within its component
  (`unique(component, name)`), never a hand-typed label. Two axes are **decoupled**: the **transport** (how bytes
  move) and the **driver** (the protocol handler that produces the normalized functions and datapoints).
  `interface_type` is the **transport** (`ssh`, `tcp`, `http`, `snmp`, `udp`, `telnet`, `icmp`): a node-side wire
  capability that also carries the default **reachability** probe (tcp/ssh/http open the port, icmp pings).
  Reachability is the **first gate of a ladder** (reach to auth to responds to collecting) and needs only the
  transport. A **driver** is the **collect** layer: a protocol handler plus the transport(s) it can run over plus
  the normalized catalog (functions and datapoints, how to fetch them as commands/OIDs/paths, parse, a version).
  The same handler can run over several transports (a CLI over `ssh` or `telnet`), so the driver declares its
  transports and the instance picks one; a genuinely different grammar over a different transport (an ssh CLI vs a
  tcp JSON-RPC) is a **different driver** producing the same catalog. Device-specific fetch detail lives in the
  driver, never the template: `snmp` is the transport, a `biamp-snmp` (or `generic-snmp`) **driver** holds the OID
  map. The entities then split on **CAN / SHOULD / IS**: the **driver** owns what a device family CAN do and how
  (transports, catalog, normalization, discovery rules, version); a **template** (per model) owns what an operator
  SHOULD watch and how it looks (curate the driver's menu to a default subset, thresholds and event rules, an
  icon); the `interface` instance owns what IS actually there (transport, host, credentials, a driver when it
  collects, the discovered subset, per-device overrides). Discovery is a driver rule whose **result lands on the
  instance**; filtering-for-choice is a template default plus an instance override; capability is the driver. The
  reusable driver is **data on one generic engine** (a declarative `canonical datapoint <- fetch <- parse`),
  official or org-custom via the `(namespace, id)` shadow registry, with a pluggable-Go escape hatch only for a
  wire the engine cannot express; a "device pack" bundles a driver plus a template, and a template **declares its
  driver deps** (version-pinned) so a missing or shadowed driver surfaces, never silently misbinds. The house
  `<entity>` / `<entity>_type` pattern holds: `interface_type` is the transport (a reachability interface's type
  genuinely is its transport), and `driver` earns its own registry (SNMP and multi-transport protocols prove it
  folds into neither transport nor template).
- **Context:** The 5a build named interfaces by a hand-typed string (`boardroom-tcp`) with `type` = the probe
  (tcp/icmp), which conflated identity with transport and implied operators name and wire-configure devices by
  hand. The reframe: operators are not programmers, so the value is a **driver that normalizes a device family
  into a pick-from menu**, which makes the template a light curation, policy, and presentation layer and means the
  operator never authors a protocol. You cannot cleanly split "how you talk" from "what you say" (the command is
  both), so the seam is elsewhere: the **transport** is the reusable connection, the **driver** is the reusable
  normalized menu over it, and the **template** is a selection plus policy. Keeping the driver as data (not Go per
  family) is what makes it community-shippable;
  growing the canonical menu device-by-device (not a universal ontology up front) is what keeps it honest;
  separating menu-of-types from discovered-instances is what fits programmable devices (a DSP's blocks are
  per-install); versioning the driver is what lets a template's picks resolve as the menu matures.
- **Scope now (tier-0):** this slice (#114) ships only the first gate. `interface_type` is the transport
  primitive (`icmp`, `tcp`, `ssh`, `http` seeded `built`), each carrying a tcp-connect or ping reachability probe;
  an `interface` is named by its protocol and typed by its transport; the dev seed models a lab **polaris DSP**
  with a `web` (http) and a `qrc` (tcp) interface, the "two APIs on one device" story. The driver catalog,
  normalization, discovery, templates, versioning, and the shadow-resolved device pack are later slices of the
  [collection epic](https://github.com/hyperscaleav/omniglass/issues/113) (slices 2 to 4 realize this model).
- **Refines:** [ADR-0038](#adr-0038-the-reachability-verdict-is-a-built-in-state) (the reachability verdict is the
  first rung of the gate ladder this ADR names).
- **Status note (2026-07-08):** the `interface = API` / `interface_type = transport` half is **built and stable**
  (this slice). The **driver / collect layer** (the separate `driver` entity, the normalized menu, and the
  driver-centric split itself) is **under active design**: it departs from the original template-centric
  architecture (where protocol handling lived in the template), which is a serious enough change to redesign
  deliberately rather than on momentum. Recorded here as the current-best direction, **not a locked gate**;
  driver-centric vs template-centric is re-examined, and this ADR revised or superseded, in a later ADR before
  the collect layer is built.

### ADR-0040: The task is derived read-only plumbing, projected from its interface

- **Date:** 2026-07-14 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [api](/architecture/api/)
- **Decision:** The `interface` is the **only authored** collection primitive; the `task` is **derived**.
  Creating an interface **derives its one poll task**, so the task surface is read-only (`GET /tasks`,
  `GET /tasks/{id}` only): the `POST` / `PATCH` / `DELETE /tasks` routes and the `task:create` / `task:update`
  grants are removed. A task carries **no node column**; `task.node_name` is dropped and its placement is
  **projected** from `interface.node_name`, so the worklist and the telemetry owner-confinement join the
  interface rather than reading a task-local node. A **node purge cascades** its interfaces and their derived
  tasks (`interface.node_name` and `task.interface_id` are `ON DELETE CASCADE`).
- **Context:** The checkpoint-5d build gave both primitives a full CRUD surface and a node placement of their
  own. That let an operator author a task divorced from its interface, and left a task's node and its interface's
  node as two independently-set fields that could disagree. The reframe makes the interface the one thing an
  operator authors (an API on a component, [ADR-0039](#adr-0039-an-interface-is-a-device-api-the-interface-type-is-its-transport-not-its-driver)):
  a reachability check is an interface, its poll task is the plumbing that runs it, and placement is a property
  of where the interface is reached from, stated once. This is the honest shape for the reach tier; the richer
  driver-authored collection surface (multiple functions over one interface) is a later slice and does not
  reintroduce operator task CRUD.
- **Refines:** [ADR-0039](#adr-0039-an-interface-is-a-device-api-the-interface-type-is-its-transport-not-its-driver)
  (the interface is the authored API; this ADR settles that its task is derived, not co-authored).

### ADR-0041: settings are a reflected typed struct with generated client and server validation

- **Date:** 2026-07-19 | **Status:** Accepted | **Pages:** [settings](/architecture/settings/)
- **Decision:** A setting is declared **once**, as a tagged field on a canonical `Settings` Go struct
  (`internal/settings/schema.go`), and that single declaration is the whole source of truth. Reflection over the
  struct produces the `code` defaults layer (`Defaults()`, from each leaf's `default:` tag) and the namespace
  registry (`Namespaces()`, from the `json` and `settings:` tags), replacing the hand-kept embedded
  `defaults.yaml` and the hand-kept `Namespaces()` slice (both retired). Huma reflects the struct into the
  OpenAPI schema, so `make gen` yields the typed SPA client `values` (a `Settings` struct, not a free-form
  object). Writes validate against that **same reflected schema** on both sides: the server backstops
  `PATCH /settings/{namespace}` (unknown namespace to 404, unknown key / wrong type / `enum` or `pattern`
  violation to a 422 naming the `namespace.key`, `null` allowed as a delete), and a `make gen` step slices the
  field constraints out of `api/openapi.json` into a committed client artifact
  (`web/src/api/settings.schema.gen.ts`) that drives inline form validation (enum-as-select, Save blocked while
  invalid). The cascade merges partial generic maps as before; typing lives only at the edges (the effective
  read unmarshals into `Settings`, and Go code reads a setting through the `EffectiveTyped` accessor).
- **Context:** Slice-0 shipped the engine with **untyped** values: a setting lived in two hand-kept places (the
  `Namespaces()` slice and the embedded `defaults.yaml`), the API exposed `values` as a free-form object, the
  generated client typed it as `Record<string, unknown>`, and the PATCH write accepted any namespace, key, or
  value and stored it as-is (the documented write-validation thin cut). That is the one surface that dodged
  doctrine 1 (API-first, typed, generated). Making `Settings` a reflected struct pulls the default, the schema,
  the typed client, both validators, and the typed accessor from a single declaration, so adding a setting is
  one tagged field and there is no second place to drift. The cascade keeps merging partial maps because a Go
  struct cannot express "unset" versus a zero value; typing is applied only at the edges. This **closes the
  slice-0 write-validation thin cut** and retires the `defaults.yaml` asset and the `Namespaces()` list.
- **Deferred:** the declarative operator-file machinery (a generated JSONSchema for the operator `settings.json`,
  validation of the **file** layer at boot, and letting the file layer take precedence over the database, the
  GitOps-wins / read-only lever) is a future slice on the same epic, as are operator-open namespaces (a typed
  map with a `Default()` method) and the group and user cascade rungs; none is built here.
- **Closes:** issue [#288](https://github.com/hyperscaleav/omniglass/issues/288) (settings engine slice-1), under
  epic [#270](https://github.com/hyperscaleav/omniglass/issues/270).

### ADR-0042: Field cascade and the type-default floor

- **Date:** 2026-07-19 | **Status:** Accepted | **Pages:** [config, secrets, and variables](/architecture/variables/)
- **Decision:** A field's **resolved value** is **deepest-set-wins** down the field arc
  `product -> location -> system -> component`: a value set at any scope beats every broader scope. When
  **nothing is set at any scope**, the value falls to the field's **type default**. The type default is
  therefore the **floor** of the cascade, not a competitor to it: a value set at any higher scope always
  beats the default, and no cascade rule is bent to make that true. Raised during design as "does a value
  set higher in the cascade beat the type default?", the answer is **yes**, and it costs the model nothing,
  because the default is simply the bottom rung.
- **Context:** The override-rendering slice needed the resolution rule pinned before the renderer could say
  what "inherited" means. Modelling the type default as a **competing scope** would force an ordering
  question at every read (does a `location` value or the type default win?); modelling it as the **floor**
  removes the question: any set value at any scope wins, and the default is what remains when the arc is
  empty. This slice is **component-only**: resolved = this component's set value, else the type default, so
  the deeper arc (`product`, `location`, `system`) is drawn in the model but not yet walked. The
  multi-scope cascade itself lands later.
- **Closes the gap:** the multi-scope cascade is tracked by
  [#291](https://github.com/hyperscaleav/omniglass/issues/291); this ADR settles only the resolution rule
  and the type-default floor.
- **Amended by [ADR-0057](#adr-0057-the-cascades-least-specific-tier-is-platform-and-a-default-is-not-a-tier):**
  the resolution outcome is unchanged (any value set at any scope beats the default, and the default is what
  remains when the arc is empty), but the default is **off the axis**, a column on the definition row, rather
  than the cascade's bottom rung. The vocabulary moved; the rule did not.

### ADR-0043: The property catalog

- **Date:** 2026-07-19 | **Status:** Accepted | **Pages:** [config, secrets, and variables](/architecture/variables/)
- **Decision:** The `datapoint_type` catalog is generalized into a primitive-agnostic **`property`**
  catalog: one typed catalog whose entry (a **property**) is a canonical, typed name that a datapoint
  **observes** and a field **declares**, identified by a **`key`** (its canonical name). The physical
  table is `property`; the concept, the API resource (`/properties`), the Go `Property` type, and the
  console all read `property`, while a property's identifier (its `key`, and the `field_definition.key`
  reference) stays `key`. Four shape changes fold `datapoint_type` in: the unused `(scope, name)` ladder
  (`org`/`template` never had an operator write path, `template_id` was a dangling column) collapses to a
  `name` primary key plus an **`official`** boolean (seed-owned rows are read-only); `value_type` becomes
  **`data_type`** over the unified set `{string, int, float, bool, json}` (`text` backfills to `string`,
  `bool` is added); **`kind`** (metric/state/log) becomes nullable, since a declared-only attribute
  property has no observed kind; and **`validation`** is a **JSON Schema** fragment (`pattern`/`enum`/`minimum`/
  `maximum` and, for a json-typed property, a nested object schema), enforced by Huma's own validator with the stored
  schema loaded through `yaml.v3`, so there is **no new dependency**. Value and source tables continue to
  key by the **name string** (no foreign key, reject-not-project at ingest exactly as before), so the
  rename is behavior-preserving: the collection registry, the reachability BFF, and the metric/state sinks
  keep working unchanged.
- **Context:** Datapoints already had a typed canonical-key catalog (`datapoint_type`: name, value_type,
  display_name, unit, validation, kind), while fields, variables, secrets, and tags had no catalog at all,
  an operator typed a key and it was registered nowhere. Rather than build a parallel table, this slice
  makes the one catalog primitive-agnostic so `serial_number` is the same concept whether a device reports
  it (observed, a datapoint) or an operator types it (declared, a field). The `official` boolean chassis is
  chosen over finishing `datapoint_type`'s never-built scope-shadow precedence: it is the proven,
  finished model the `*_type` registries already use.
- **Values reference the key, not a `<primitive>_key` layer:** the only binding is the type-schema
  (`field_definition` gaining a `key` reference so a field draws from the catalog), which is **PR-B**, not
  this slice. Provenance rides the value (an observed metric versus a declared field value share the key),
  so reconciliation of declared-versus-observed sources needs no middle table; it is deferred.
- **Deferred:** the `field_definition.key` reference (PR-B); the type-schema editor (how a `component_type`
  selects properties); reconciliation (the declared-versus-observed drift signal); a console editor for the
  validation JSON Schema (set via the API for now); variables/secrets/commands/tags adopting a `key`
  reference; and an operator shadow of an official property.
- **Closes:** issue [#297](https://github.com/hyperscaleav/omniglass/issues/297) (the field catalog,
  expanded to the property catalog), under epic
  [#266](https://github.com/hyperscaleav/omniglass/issues/266).

### ADR-0044: The component classification catalogs

- **Date:** 2026-07-20 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/)
- **Decision:** The [`component_make`](#adr-0031-component_make-registry-slice-1-an-official-boolean-a-deferred-referential-guard-and-website-scheme-validation)
  catalog is generalized into a **`vendor`** catalog carrying a **`kind`** (`manufacturer` / `integrator` /
  `developer`), so the one organization that makes, integrates, or writes for a component is a single
  reference entity rather than a make-only registry. Two new **leaf** catalogs join it as the rest of the
  component-classification reference data: a **`driver`** (`id`, `display_name`, `version`, the software that
  speaks to a component) and a **`capability`** (`id`, `display_name`, a thing a component can do). Each of the
  three is a **gated CRUD Catalog console page** (`/vendors`, `/drivers`, `/capabilities`), reusing the
  `official`-boolean chassis the type and property registries already use: seed-owned official rows are
  read-only (an official row refuses update and delete, 422), a custom row is full CRUD gated by the
  resource's `<resource>:create` / `:update` / `:delete` permission and audited in the same transaction. The
  official rows are seeded at boot. This is a pure classification slice: **`product`** (the specific model an
  organization sells), the **`product_capability`** link, and the **`component.product`** pointer that binds a
  component to its product are the **next slice**, not this one.
- **Context:** The estate model is shifting from the make/model catalog sketch toward a fuller classification
  vocabulary: **property / event / command** on the signal side (property landed in
  [ADR-0043](#adr-0043-the-property-catalog)) and **vendor / product / driver / capability / standard / role /
  health** on the component side. This is **PR2** of that shift. `component_make` was manufacturer-only, but the
  same organization concept covers an integrator who installs the estate and a developer who writes a component's
  software, so generalizing make into a `kind`-tagged vendor is the honest widening rather than three parallel
  organization registries. Driver and capability are leaf catalogs (no tree, no cross-references yet), so they
  ship as the plain seeded-plus-CRUD pattern the registries already prove; they gain their bindings (a driver to
  an interface / product, a capability to a product) when the product slice gives them something to reference.
- **Deferred:** `product`, `product_capability`, and `component.product` (the next slice); a referential delete
  guard on vendor / driver / capability (nothing references them yet, exactly as `component_make` shipped with no
  guard until `component_model` was to land); and an operator shadow of an official row.
- **Refines:** [ADR-0031](#adr-0031-component_make-registry-slice-1-an-official-boolean-a-deferred-referential-guard-and-website-scheme-validation)
  (the `component_make` registry is renamed and generalized to `vendor` with a `kind`; its `official`-boolean,
  deferred-delete-guard, and website-scheme-validation calls carry over unchanged).

### ADR-0045: The product catalog

- **Date:** 2026-07-20 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/), [Products guide](/guides/admin/products/)
- **Decision:** **`product`** is a first-class catalog entity, the concrete **SKU** (a Cisco Room Bar, a
  Samsung QM55) that ties the [ADR-0044](#adr-0044-the-component-classification-catalogs) leaf catalogs
  together. A product carries a stable `id` and `display_name`, a **`kind`** from a fixed enum (`device` /
  `app` / `service` / `vm`, default `device`, enforced by a DB CHECK and at the API edge), an optional
  **`vendor_id`** (who makes it) and **`driver_id`** (what talks to it), an optional **`parent_product_id`**
  (a self-reference: a variant points at its base product), and the `official` boolean the type and
  classification registries already use. The **capabilities** a product provides are a many-to-many set in
  the **`product_capability`** join (a video bar provides microphone, speaker, camera, codec); setting
  capabilities on an update replaces the whole set. It is a gated CRUD Catalog console page (`/products`) on
  the same chassis as the leaf catalogs: seed-owned official rows read-only (update and delete 422), custom
  rows full CRUD gated by `product:create` / `:update` / `:delete` and audited in the same transaction,
  official rows seeded at boot. Crucially, **`component.product_id`** (`on delete restrict`) now points a
  component at the product it **is**: the product is the source of a component's shape (its vendor, driver,
  and capability set), **replacing the `component_type`-as-shape notion**. The restrict FK is the referential
  guard the leaf catalogs deferred: a product still referenced by a component cannot be deleted (409). The
  vendor, driver, and parent FKs are `on delete set null` instead (deleting a vendor nulls a product's
  pointer, it does not block).
- **Context:** The estate model is shifting toward property / event / command on the signal side and vendor /
  product / driver / capability / standard / role / health on the component side.
  [ADR-0044](#adr-0044-the-component-classification-catalogs) landed vendor, driver, and capability as leaf
  catalogs with nothing to reference them; **product** is **PR3**, the layer they were built for and the first
  consumer of all three. A component's shape used to be a job for a `component_type` genus; binding a component
  to a product instead makes the shape data-driven from the SKU (the same product supplies the same vendor,
  driver, and capabilities to every component that is one), which is why `component.product` is a `restrict` FK,
  not `set null`: a product in use is load-bearing for its components.
- **Deferred:** a product's own template or field-schema binding; and the remaining component-side catalogs
  (standard, role, health).
- **Supersedes:** the `component_type`-as-shape notion (a component's shape now comes from its `product`, not
  its genus type). **Consumes** [ADR-0044](#adr-0044-the-component-classification-catalogs) (product is the
  `product` layer that ADR deferred; it references vendor, driver, and capability). One divergence from the
  leaf catalogs' prediction: their deferred delete guard lands only as the `component.product` restrict (409),
  while a product's own vendor / driver references null out (`on delete set null`) rather than blocking the
  referenced row's delete.

### ADR-0046: The `event` log-kind sink

- **Date:** 2026-07-20 | **Status:** Accepted; superseded in part by [ADR-0066](#adr-0066-logs-are-a-raw-ingest-lane-not-events) (the log-to-event ingest promotion and the seeded `log.line` type were removed) | **Pages:** [core entities](/architecture/core-entities/), [datapoints](/architecture/properties/), [data collection](/architecture/collection/), [API](/architecture/api/), [Nodes and reachability guide](/guides/operator/collection/)
- **Decision:** A collected **log**-kind observation now has a durable home. A new **`event`** table is the
  **log-kind sink** of the collection pipeline, the counterpart of `metric_datapoint` / `state_datapoint`: where
  a datapoint records a **sampled present value**, an `event` records a **past occurrence** (a device log line, a
  structured frame). It carries the **same datapoint owner exclusive-arc** (`owner_kind` plus
  `component_id` / `system_id` / `location_id` / `node_id`, one-set CHECK) and the **same provenance** vocabulary
  (`observed` / `calculated` / `intended` / `declared`, default `observed`) as the datapoint sinks, plus a
  **`message`** (text) and structured **`attributes`** (jsonb). The ingest consumer's `deriveDatapoints` now
  returns metrics, states, **and** events, and the persistence path calls **`InsertEvents`**: a **log**-kind
  datapoint that used to be **dropped** at ingest (it had no sink) is routed to `event`, riding `string_value`
  (its message) or `json_value` (its attributes), under the **same** owner-confinement and reject-not-project
  gates as the metric and state sinks. A boot-seed property **`log.line`** (kind `log`) is the canonical
  log-kind starter. The reserved **`event_id`** columns on `metric_datapoint` and `state_datapoint` are closed
  into **real foreign keys** to `event(id)` (`on delete set null`), so an **intended**-provenance datapoint
  references the `event` that produced it. Storage adds `InsertEvents` (batch, in-tx, provenance `observed`) and
  `ListComponentEvents(name, since, limit)` (newest first); the read route **`GET /components/{name}/events`**
  (operationId `list-component-events`, gated `component:read`, non-disclosing 404 out of scope) returns the last
  24 hours, capped at 200, and is the log-kind mirror of the reachability read. The console component detail page
  gains an **Events** panel over it.
- **Context:** The estate model is shifting toward property / event / command on the signal side; property
  landed in [ADR-0043](#adr-0043-the-property-catalog). Log was already a first-class datapoint **kind** in the
  registry, but the ingest consumer had **no sink** for it: a log-kind datapoint was silently dropped after the
  metric/state route split ([ADR-0038](#adr-0038-the-reachability-verdict-is-a-built-in-state)), a checkpoint gap
  rather than a design choice. This is the **P1 follow-up** of the estate-model roadmap: give the log kind a
  durable home so the third sink flows like the other two, and close the `event_id` stubs the datapoint tables
  reserved for exactly this. Reusing the datapoint owner-arc and provenance (not a bespoke shape) keeps a log
  occurrence owned, addressed, and traced identically to the values beside it, the primitive-first move.
- **Deferred:** the **`datapoint` -> `sample`** rename (a naming cleanup that lands in a later slice, so the
  datapoint tables keep their current names here); **`property_value`** and the materialized current-value store
  (the [fold-fields slice](/architecture/properties/#reads-current-value-is-a-view)); the normalized **event_type**
  registry and the **promotion** of a raw `event` occurrence into a registered event ([events](/architecture/events/));
  a scope-wide `event` read (this ships the per-component read only); and any `calculated` / `intended` / `declared`
  event producer (the write path is `observed` collection only).
- **Supersedes:** the checkpoint behavior where a **log**-kind datapoint had no sink and was **dropped** at ingest
  (recorded in [ADR-0038](#adr-0038-the-reachability-verdict-is-a-built-in-state)); the log kind now persists to
  `event`. **Divergence from [datapoints](/architecture/properties/):** that page's present-tense design routes the
  log kind to a **`log_datapoint`** table and treats `event` as a strictly normalized, `event_type`-registered
  occurrence promoted from a raw line. The built log sink is the **`event`** table directly (the raw occurrence
  lands there, not in a separate `log_datapoint` table), and the `log_datapoint` table plus the promotion ladder
  stay `Design`; the pages carry an inline note pointing here until the two models are reconciled in the
  fold-fields / rename cleanup.

### ADR-0047: The fields fold: `product_property` and `property_value`

- **Date:** 2026-07-21 | **Status:** Accepted; superseded in part by
  [ADR-0085](#adr-0085-the-component_type-registry-returns-as-the-device-class-genus) (the
  `component_type` registry returns, reshaped above the product) | **Pages:** [core entities](/architecture/core-entities/),
  [config, secrets, and variables](/architecture/variables/), [API](/architecture/api/),
  [Properties guide](/guides/admin/properties/), [Products guide](/guides/admin/products/)
- **Decision:** The standalone **fields** feature is **retired** and folded into the estate model, because a
  **field was never a primitive**: it was a **property with `declared` provenance**, and the same is true of
  **config** (a property with `intended` provenance). Two tables replace it. **`product_property`** is the
  product's declared-property **contract**: `(product_id -> product, property_name -> property,
  default_value jsonb, required bool)`, unique per `(product, property)`, so what a product's instances expose
  is data on the SKU rather than a catalog hung off a genus type. `data_type` and `validation` are
  **not duplicated** here; they stay in the [`property` catalog](#adr-0043-the-property-catalog), the single
  source. **`property_value`** is the value store: it carries the **same owner exclusive-arc** as
  `metric_datapoint` and [`event`](#adr-0046-the-event-log-kind-sink) (`owner_kind` plus
  `component_id` / `system_id` / `location_id` / `node_id`, one-set CHECK), plus `property_name`, an
  `instance` discriminator, a **`provenance`** (`observed` / `calculated` / `intended` / `declared`, default
  `declared`), and a jsonb `value`. Its series key is `unique nulls not distinct`, since the arc leaves three
  owner columns NULL and Postgres's default NULLS DISTINCT would let duplicate rows through. This slice writes
  only `owner_kind=component` with `provenance=declared`; the rest of the arc and the other three provenances
  are the seats later producers sit in. The resolver is **`EffectiveProperties(component, scope)`**, one SQL
  UNION of two arms: the **contract arm** (every `product_property` of the component's product, value =
  `coalesce(the component's declared value, the contract default)`, `from_contract=true`) and the **ad-hoc
  arm** (declared values the contract does not declare, `from_contract=false`), so a **productless component
  still resolves**, to its ad-hoc set alone. Six routes carry it: `GET /products/{id}/properties` and
  `PUT` / `DELETE /products/{id}/properties/{property}` (gated `product:read` / `:update` / `:delete`, an
  official product read-only 422), and `GET /components/{name}/properties` plus
  `PUT` / `DELETE /components/{name}/properties/{property}` (gated `component:read` / `:update`, ABAC-scoped
  with a non-disclosing 404 out of scope, audited). The console renames the operator word from **Fields** to
  **Properties**: a **Properties** panel on the component detail (contract rows, plus a dashed-bordered
  **off contract** group for the ad-hoc ones, an override toggle with an accent dot, a required property
  blocking Save) and a **Declared properties** contract editor on the product detail (declare, edit, withdraw,
  read-only for an official product). Retired with the feature: **`field_value`**, **`field_definition`**,
  **`component.component_type`**, and the **`component_type`** table itself with its routes
  (`/types/component`), its console registry section, and its seed. A component's shape now comes from its
  **product** ([ADR-0045](#adr-0045-the-product-catalog)), still optional: a productless component simply has
  no contract, and the category `component_type` used to carry (display, codec) is expressed by the
  **capabilities** that product provides. The seeded products ship a starter contract (`cisco-room-bar` and
  `samsung-qm55` declare `serial_number`, `firmware_version`, and `model_number` with defaults), and
  `roles.yaml` drops the now-unclaimed `field:*` permissions, since `property:*` already covers the tier.
- **Context:** [ADR-0043](#adr-0043-the-property-catalog) made the catalog primitive-agnostic and deferred the
  one binding it needed, `field_definition.key`, so a field could draw its type from the catalog. Building
  that binding forced the realization that the binding was the wrong shape: once a field's name, type, and
  validation all come from a property, a "field" is a property the operator **declares** rather than the
  device **observes**, and the only thing left that was field-specific was **where the schema hangs**. The
  answer was already on the table: [ADR-0045](#adr-0045-the-product-catalog) made the **product** the source
  of a component's shape, so the per-type field catalog becomes the per-product **contract** over the property
  catalog, and the field value becomes an arc-owned, provenance-tagged **property value** beside the samples
  and occurrences it sits next to. Folding is primitive-first: one value store the cascade, reconciliation,
  and the current-value read can all be built on, rather than three parallel ones (`field_value` for declared,
  the datapoint tables for observed, an unbuilt `config` table for intended).
- **Deferred:** **`standard_property`** and **`location_type_property`** (the other contract owners, each
  waiting on its owner entity, `standard` and a `location_type` schema); the **driver access/mode column** on
  `product_property` (whether a driver can get, set, or only declare a property, which lands with the driver
  slice); the **non-declared provenance producers** (`intended` config writing a desired value,
  `observed` materializing a current value out of the datapoint stream, `calculated` from a rule), which the
  provenance column seats but nothing writes yet; the multi-owner arc on `property_value` (only the component
  arm is written and scope-injected today); and the **`datapoint` -> `sample`** rename, still a later cleanup.
- **Supersedes:** [ADR-0043](#adr-0043-the-property-catalog)'s deferred **`field_definition.key`** property
  binding. This **is** that binding, done differently: rather than a field definition gaining a key reference,
  the field catalog itself became the **product contract over the property catalog**, and `field_definition`
  retires. Also completes [ADR-0045](#adr-0045-the-product-catalog)'s partial supersession of the
  **`component_type`-as-shape** notion: that ADR repointed shape at the product but left the table standing;
  this one drops `component.component_type` and the `component_type` registry outright.
- **Tracked under** epic [#266](https://github.com/hyperscaleav/omniglass/issues/266). This is **PR5** of the
  estate-model shift toward property / event / command plus vendor / product / driver / capability / standard /
  role / health.
- **Amended by [ADR-0057](#adr-0057-the-cascades-least-specific-tier-is-platform-and-a-default-is-not-a-tier):**
  the contract default is unchanged, its vocabulary is. `coalesce(the instance's set value, the contract
  default)` is the fall-through to a **declaration**, not the bottom rung of a cascade, so
  `product_property.default_value` (and its two siblings) is the shipped instance of the off-axis default
  rather than a tier under `platform`.
- **Partially reversed by [ADR-0085](#adr-0085-the-component_type-registry-returns-as-the-device-class-genus):**
  this ADR's retirement of `component.component_type` and the `component_type` registry stands; naming
  and rendering (a generated component name needs a device-class stem the product's SKU cannot supply)
  forced the registry's return, deliberately reshaped: **above the product**
  (`product.component_type_id`), not beside the component and not a second classifier the component
  itself carries. What this ADR actually decided about **`product_property`** and **`property_value`**
  is untouched; only the "and the whole `component_type` registry retire" clause is reversed.

### ADR-0048: The `standard` blueprint and the template-fork seed model

- **Date:** 2026-07-21 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [API](/architecture/api/), [identity and access](/architecture/identity-access/),
  [storage](/architecture/storage/), [Standards guide](/guides/admin/standards/),
  [Types guide](/guides/admin/types/), [Properties guide](/guides/admin/properties/)
- **Decision:** Three moves land together, because each one only makes sense with the others.

  **1. `system_type` is promoted to `standard`.** A **standard** is the **blueprint a system conforms to**
  (huddle room, classroom, auditorium): the system-side counterpart of
  [`product`](#adr-0045-the-product-catalog), not a label hung off a system. The table is renamed and gains
  **`parent_standard_id`** (a variant points at its base, mirroring `product.parent_product_id`) and a
  declared-property **contract**. `system.system_type` becomes **`system.standard_id`** and is now
  **optional**, exactly like `component.product_id`: a **one-off system that conforms to no standard is
  first-class** and carries only its own ad-hoc values. The seeded rows carry over unchanged. Because a
  standard now owns a contract, it leaves the shared `type:*` registry permission and takes its own
  **`standard:read` / `:create` / `:update` / `:delete`** Catalog resource (read on the viewer `*:read` floor,
  the writes at the admin tier, exactly like `product:*`), and its routes move from `/types/system` to
  **`/standards`**.

  **2. Two more contract tables, and one owner-generic resolver.** **`standard_property`** and
  **`location_type_property`** join `product_property` on the identical shape (`<classifier>_id`,
  `property_name`, an optional `default_value`, a `required` flag, unique per pair). `data_type` and
  `validation` are **never** duplicated onto a contract; they stay in the [`property`
  catalog](#adr-0043-the-property-catalog). The resolver then generalizes:
  **`EffectiveProperties(ctx, ownerKind, ownerID, read)`** resolves **component, system, location, and node**
  off **one** parameterized SQL template driven by an **`ownerContract`** table (instance table, classifier
  column, contract table, contract key, arc column). Component reads its contract through
  `component.product_id`, system through `system.standard_id`, location through `location.location_type`; a
  **node has no classifier**, so it resolves ad-hoc values only. The query shape is unchanged from
  [ADR-0047](#adr-0047-the-fields-fold-product_property-and-property_value): a contract arm
  (`coalesce(the instance's value, the contract default)`, `from_contract=true`) UNION an ad-hoc arm. This is
  the primitive-first move: three classifier/instance pairs, one resolver, so they cannot drift. Alongside it,
  `guardOwnerScope` now scope-checks **every** owner arc on a value write (it previously returned nil for
  everything but the component arc), so an out-of-scope system or location is a non-disclosing 404 on the
  write path as well as the read.

  **3. The seed model: templates live in code, not the database.** A standard (and a location type) is
  created by **forking an in-code template**. The fork is **one-time, with no inheritance**, so nothing in any
  tenant ever points back at a template and templates can be improved in any release. That dissolves the
  shipped-defaults-versus-local-edits problem at the root: the thing the vendor updates (the template) and the
  thing the operator owns (the row) are **never the same object**. Four consequences follow.

  - **Forking applies to template -> standard, not standard -> system.** A system does not fork its standard,
    it **conforms** to it, with **live inheritance**: the standard's contract default resolves for every
    conforming system until that system overrides it, and revising the default moves every system that has
    not.
  - **Therefore a shipped standard or location type is operator-owned, not official.** Both seed with
    **`official: false`** through **seed-if-absent** paths (`SeedStandard` / `SeedLocationType`, `ON CONFLICT
    DO NOTHING`), never the authoritative `Upsert*`. They are freely editable and deletable from the moment
    they land.
  - **An authoritative upsert here would be a bug, not a policy.** `ON CONFLICT DO UPDATE` would silently
    revert an operator's edit on the next boot, which is the exact failure this model avoids. A regression
    test edits a seeded standard, re-runs the seed, and asserts the edit survived.
  - **The canonical catalogs are the exception** and keep the authoritative upsert with `official: true`:
    **`property`** (and later `command` and `event_type`) is the **shared vocabulary a driver maps onto**, so
    a release must be able to correct it. The classification catalogs (`vendor`, `driver`, `capability`,
    `product`, `interface_type`, `secret_type`) and `role` stay on that same authoritative path for now.

  Four route groups carry the contracts and the values, all regenerated into the OpenAPI document, the cobra
  CLI, and the typed client: `GET /standards/{id}/properties` plus
  `PUT` / `DELETE /standards/{id}/properties/{property}` (gated `standard:read` / `:update` / `:delete`);
  `GET /location-types/{id}/properties` plus `PUT` / `DELETE .../{property}` (gated `type:*`, since the
  location type registry is still a `type` registry); and the value sides
  `GET /systems/{name}/properties` plus `PUT` / `DELETE .../{property}` (gated `system:read` / `:update`) and
  the same for `/locations/{name}/properties` (gated `location:read` / `:update`). The value routes are
  **scope-injected**, so an out-of-scope system or location is a non-disclosing **404**.
- **Context:** [ADR-0047](#adr-0047-the-fields-fold-product_property-and-property_value) deferred
  `standard_property` and `location_type_property` because neither owner was ready: `system_type` was a bare
  label registry with no contract to hang anything on. Building that contract forced the promotion, since a
  registry that declares what its instances expose **is** a blueprint, and a blueprint is the system-side
  `product`. Making `standard_id` optional followed immediately: a productless component was already
  first-class, and a system that matches no blueprint has the same claim. The seed question surfaced during
  the build and is the harder half. Shipping a room standard as an authoritative `official` row would make it
  **read-only** (an operator could not tune "Huddle Room" to their own estate) and would **revert local edits
  on every boot** if it were writable. Both failures come from one mistake: treating example content and
  canonical vocabulary as the same kind of thing. Splitting them (a template in code that is forked once,
  versus a catalog row that is upserted authoritatively) lets the release improve its examples forever without
  ever touching an estate's data, and keeps the one thing that genuinely must stay identical install to
  install, the property vocabulary, under release control.
- **Deferred:** the **in-code template mechanism** itself and its create-from-template console affordance
  ([#317](https://github.com/hyperscaleav/omniglass/issues/317)); this slice ships the seed-if-absent behavior
  and the operator-owned rows that the mechanism will produce, with the shipped starter set still declared as
  seed YAML. The **official / community / private catalog tiering** for `product` / `driver` / `property` /
  `event_type` plus a **disable flag** ([#318](https://github.com/hyperscaleav/omniglass/issues/318)). Also
  still deferred: a standard's **role set** and health composition, the non-`declared` provenance producers,
  the cross-owner cascade over `property_value`, and the `datapoint` -> `sample` rename.
- **Supersedes:** the **`system_type`-as-label** notion (a system's blueprint is a first-class Catalog entity
  with its own contract and its own permission, and it is optional), completing on the system side what
  [ADR-0045](#adr-0045-the-product-catalog) and
  [ADR-0047](#adr-0047-the-fields-fold-product_property-and-property_value) did on the component side. Also
  supersedes the assumption running through [ADR-0044](#adr-0044-the-component-classification-catalogs),
  [ADR-0045](#adr-0045-the-product-catalog), and
  [ADR-0047](#adr-0047-the-fields-fold-product_property-and-property_value) that **everything the seed ships
  is `official` and read-only**. That now holds **only for the canonical catalogs**: the shipped standards and
  the four shipped location types (`campus` / `building` / `floor` / `room`) are `official: false` and fully
  editable, so any prose promising a read-only seed-owned row for those two registries is stale.
- **Tracked under** epic [#266](https://github.com/hyperscaleav/omniglass/issues/266). This is **PR6** of the
  estate-model shift toward property / event / command plus vendor / product / driver / capability / standard /
  role / health.

### ADR-0049: The system role: capability-gated staffing and the resolved capability set

- **Date:** 2026-07-21 | **Status:** Superseded by [ADR-0087](#adr-0087-capability-gated-staffing-retires-an-alarm-impairs-its-component-not-a-named-capability) | **Pages:** [core entities](/architecture/core-entities/),
  [API](/architecture/api/), [glossary](/architecture/glossary/), [templates](/architecture/templates/),
  [health](/architecture/health/), [Standards guide](/guides/admin/standards/),
  [Capabilities guide](/guides/admin/capabilities/), [Work with an entity](/guides/operator/entities/)
- **Decision:** A system says **what it needs filled**, and the platform **refuses** a component that cannot
  fill it. Four tables and two resolvers carry that.

  **1. A `system_role` is a slot, declared on the arc.** A role (a table microphone, a main display) is
  declared either on a **`standard`**, where **every conforming system inherits it live**, or **directly on
  one `system`** (ad-hoc, which is how a one-off system gets roles at all). The two owners ride the **same
  exclusive-arc pattern `property_value` uses**: an `owner_kind` plus `standard_id` / `system_id`, a one-set
  CHECK, and a `unique nulls not distinct` key over the arc columns and the role name (the default NULLS
  DISTINCT would let duplicates through the NULL arm). A role carries a **`quorum`**: how many components
  should fill it, at least one, because a role no component need fill is not a role.

  **2. `role_capability` is conjunctive.** A role requires a set of [`capability`](#adr-0044-the-component-classification-catalogs)
  rows, and a component must provide **every** one of them. Requiring nothing admits anything, which is the
  honest reading of an empty requirement, not a special case.

  **3. A component's capabilities become a resolved set.** **`component_capability`**
  (`component_id`, `capability_id`, `present`) is the component's **own** capability facts, layered over its
  product's: `present=true` **adds** one the product does not claim, `present=false` **suppresses** one it
  does. **`EffectiveCapabilities(component)`** is then the product's set UNION the additions MINUS the
  suppressions, and a **productless component resolves to just its own declarations**. This is the single
  definition of "what this component can do" for the whole platform, and it is the set the guard checks.

  **4. `EffectiveRoles(system)` merges both arms.** The roles the system's standard declares (marked
  `from_standard`) UNION those declared directly on it, each with its required capabilities, its quorum, and
  the components filling it here. A one-off system has only the ad-hoc arm. The resolver serves `Assigned()`
  and `Understaffed()` (quorum minus assignments, floored at zero) rather than leaving arithmetic to each
  surface, so staffing reads the same way everywhere.

  **5. The guard refuses, and the refusal names the gap.** `AssignRole` is a **422 when the component's
  resolved capabilities do not cover every capability the role requires**, and the message names the missing
  ones (`component "panel-1" cannot fill role "table-mic": missing microphone, speaker`), sorted so the same
  gap always reads the same way. It joins the **location placement constraint** as a refusal on **modeled**
  grounds, and follows the same rule that one set: **name the parties**. A bare "no" leaves the operator
  nothing to do, and the whole value of modeling capability is that the refusal is actionable. Assignment is otherwise idempotent, and **`role_assignment.component_id` is
  `on delete restrict`**, so a component staffing a role cannot be deleted out from under the system.

  Eight routes carry it, regenerated into the OpenAPI document, the cobra CLI, and the typed client:
  `GET /standards/{id}/roles` plus `PUT` / `DELETE /standards/{id}/roles/{role}` (gated `standard:read` /
  `:update` / `:delete`); `GET /systems/{name}/roles` (the resolved read) plus
  `PUT` / `DELETE /systems/{name}/roles/{role}` and
  `PUT` / `DELETE /systems/{name}/roles/{role}/assignments/{component}` (gated `system:read` / `:update`);
  and `GET /components/{name}/capabilities` plus `PUT` / `DELETE /components/{name}/capabilities/{capability}`
  (gated `component:read` / `:update`). Every system and component route resolves its owner **within the
  caller's scope first**, so an out-of-scope target is a non-disclosing **404**. The shipped `meeting-room`
  standard declares `room-mic` (microphone + speaker, quorum 2) and `main-display` (flat-panel-display, chosen
  so the shipped Samsung QM55 can actually fill it), **seeded if absent** on the
  [operator-owned lane](#adr-0048-the-standard-blueprint-and-the-template-fork-seed-model), so an operator's
  quorum retune survives a re-seed.
- **Context:** The strict refusal was decided first: a role that names a requirement and then lets anything
  fill it is decoration. But a component's capabilities came **only from its product**, and `product` is
  deliberately **optional** on a component
  ([ADR-0047](#adr-0047-the-fields-fold-product_property-and-property_value)), so under a strict guard a
  **productless component could have filled no role at all**. Three ways out, and only one of them keeps both
  halves: make product mandatory (reverses a call made one slice ago for good reasons), soften the guard to a
  warning (throws away the point of the model), or let a **component declare its own capabilities over its
  product's**. Layering resolves the tension without touching either commitment, and it is not a new shape: it
  is exactly the **contract-plus-override** the declared properties already use, where a product declares a
  default and an instance overrides it, applied to capabilities instead of values. Quorum lands in this slice
  and impact does not, because **staffing is visible without health at all**: a role wanting 2 with 1 assigned
  is under-staffed today, on data the operator entered, with no engine reading anything.
- **Deferred:** **`impact`** (`outage` / `degraded` / `none`, what an unfilled or failing role does to its
  system) and the whole **SLI rollup**, which land in **PR8** with the engine that reads them; the console
  surfaces for both arcs (the standard's role editor, the system's roles panel, the component's capability
  editor); **role-scoped config**, a value declared against a role slot and resolving onto whichever component
  fills it ([templates](/architecture/templates/) describes it, nothing builds it); a **cap at quorum** (a role
  may be over-staffed, and nothing refuses the extra assignment, because "more than enough" is not an error);
  and the **`system_member`** composition table, which stays `Design`.
- **Supersedes:** the **`system_template_member` role-requirement** design on
  [templates](/architecture/templates/). That page still describes a role slot **frozen into a
  `system_template_version`**, whose requirement is a set of **canonical datapoints and commands** and whose
  instance assignment is a **`system_member`** row. What shipped puts the slot on the **standard / system
  arc** (a standard is the blueprint now, so the role belongs with it), states the requirement as a
  **capability** set (a coarser, operator-legible vocabulary that exists as a catalog today, where canonical
  commands do not), and records the assignment in **`role_assignment`**. Templates and their frozen BOM stay
  `Design`; the two models are reconciled when template pinning is built, and until then the built role model
  is the one on [core entities](/architecture/core-entities/). Also supersedes the reading, running through
  [ADR-0044](#adr-0044-the-component-classification-catalogs) and
  [ADR-0045](#adr-0045-the-product-catalog), that **a capability is a product-only fact**: a capability is now
  a fact about a **component**, which its product supplies a default for.
- **Tracked under** epic [#266](https://github.com/hyperscaleav/omniglass/issues/266). This is **PR7** of the
  estate-model shift toward property / event / command plus vendor / product / driver / capability / standard /
  role / health.

### ADR-0050: Health is a recorded transition, computed from the alarm-capability-role chain

- **Date:** 2026-07-21 | **Status:** Accepted | **Pages:** [health](/architecture/health/),
  [core entities](/architecture/core-entities/), [API](/architecture/api/), [glossary](/architecture/glossary/),
  [Standards guide](/guides/admin/standards/), [Work with an entity](/guides/operator/entities/)
- **Decision:** Health is a **verdict** on a system or a location, **derived** from what is wrong with the
  components staffing it and **recorded as a transition** at the moment it changes. Five calls carry that.

  **1. Capability is the routing key, and an alarm is how a component loses one.** An **`alarm`** is
  **component-local** (`component_id`, a `severity` of `info` / `warning` / `critical`, a `message`, a
  `raised_at`, and a **nullable `cleared_at`**), and **`alarm_capability`** names the
  [capabilities](#adr-0044-the-component-classification-catalogs) it degrades. Clearing **keeps the row**, so
  the record of what was wrong and when survives the fix. The chain from there is one sentence per hop: a
  component **satisfies** a role only when it provides **every** required capability and **none of those is
  currently degraded**; a role with fewer satisfying components than its **quorum** is **impaired**; an
  impaired role contributes its declared **`impact`** (`outage` / `degraded` / `none`, a column on
  `system_role`, defaulting to `degraded`); a system takes the **worst** contribution among its roles, and a
  location the worst among the systems placed anywhere beneath it. That chain is why a capability is **flat**
  and why a role requires a **set** of them: capability is the only vocabulary shared by the thing that breaks
  (a component) and the thing that cares (a slot in a room), so it is the only honest place to route through.
  **Impact lives on the role**, not on the alarm or the component, because the same broken box matters
  differently depending on the slot it was filling: a dead confidence monitor is not a dead main display.

  **2. The judgement is a pure package.** **`internal/health`** takes resolved inputs and returns a verdict,
  with **no database**: `Component.Satisfies`, the quorum boundary, worst-wins at both levels, and the
  impact mapping are unit tests, not SQL. Two of its defaults are deliberate **safety** calls in opposite
  directions. An **unrecognized impact reads `degraded`**, so a bad value can never make an impaired role
  silently harmless. An **unrecognized recorded value reads `healthy`**, so one stray row cannot paint an
  estate broken. The rule behind both: fail loud about a **judgement**, fail quiet about a **record**.

  **3. Health is recorded as a transition-only state, on `state_datapoint`.** The requirement this whole
  design serves is an **accurate history of the edges**: exactly when a system stopped working, answerable
  weeks later. `state_datapoint` is already that primitive (the ingest path writes a row only when the value
  differs from the last one stored, and `StateTransitions` reads the ordered flips the reachability
  availability strip draws), so health reuses it rather than adding a history table: the **owner arc**,
  `provenance='calculated'`, and `source_rule='health-rollup'` (the lineage CHECK requires a non-null
  `source_rule`). The first value for an owner is always recorded, even `healthy`, so a reader can tell
  "healthy since we started watching" from "never evaluated".

  **4. Recompute happens at the writes that can change health, in the same transaction.** Every mutation that
  can move a verdict recomputes the affected chain before it commits: **raising** or **clearing** an alarm,
  **assigning** or **unassigning** a component, **declaring** or **withdrawing** a role, changing a role's
  **quorum** or **impact**, changing a component's **capabilities** or its **product**, **creating** a system,
  and changing the **standard** it conforms to or the **location** it sits in (recomputing **both** the old
  and the new location, since the one it left may have just improved). **A read never writes.** Two
  alternatives were considered and rejected, and both fail the same requirement. **Compute-on-read** keeps no
  history at all, so "when did this break" is unanswerable by construction. **Compute-and-write-through-on-read**
  keeps a history that is **sampled by whoever opens a page**: the edge timestamp becomes the moment somebody
  looked, not the moment the estate changed, and an estate nobody watched over a weekend has no weekend. A
  transition is only worth recording if it is recorded **where the change happened**.

  **5. A report computes the verdict it serves from the evidence it shows.** The health report originally
  served the **last recorded** verdict while resolving the contributing roles **live**, which let a system
  with nothing recorded yet report `healthy` beside an impaired `outage` role: the report contradicted
  itself. The served verdict is now derived from the same resolved rows the report displays, so the headline
  and the reason can never disagree. This is **not** self-healing on read: nothing is written, and the
  **recorded transitions remain the source for history**. A missing trigger can therefore cost an edge in the
  history, but it can never make a report lie about the present.

  **Routes**, regenerated into the OpenAPI document, the cobra CLI, and the typed client:
  `GET` / `POST /components/{name}/alarms` and `DELETE /components/{name}/alarms/{id}` (gated
  `component:read` / `:update`); `GET /systems/{name}/health` and `GET /locations/{name}/health` (gated
  `system:read` / `location:read`, scope-injected, an out-of-scope owner a non-disclosing 404), each returning
  the verdict, the contributing roles (with the degraded capabilities and the causing alarms) or the systems
  beneath, and the recorded transitions over the last 30 days. The CLI reads
  `omniglass component alarms|raise-alarm|clear-alarm`, `omniglass system health`, and
  `omniglass location health`. The seed adds a **`health`** state-kind property
  (`healthy` / `degraded` / `outage`) so the recorded series is typed like any other.
- **Context:** The architect's requirement was stated plainly: *"The most important thing about health is that
  we have a real, accurate history of the edges. We need to know exactly when a system went from healthy to
  unhealthy, and be able to look back at it weeks later."* Every call above falls out of taking that
  literally. Once the history has to be **accurate**, the write side is the only correct place to compute
  from, and once the carrier has to be **edges**, `state_datapoint` is already the right table and a new
  `health_history` would have been a second, worse copy of it. Recording an **opening verdict at system
  creation** then surfaced a latent bug the rest of the schema had been quietly carrying: every system now
  had a `state_datapoint` row from birth, and **every rename failed on the owner foreign key**, because those
  FKs address the owner **by name** and declared no `ON UPDATE`. Migration `20260721170000` re-adds all four
  `state_datapoint` owner FKs with **`on update cascade`**, which is what name-as-address always meant: the
  history follows the entity rather than pinning its old name. Health did not create that bug, it made it
  **reachable for every system**, which is the useful kind of forcing function.
- **Deferred:** the **same FK gap** on `metric_datapoint`, `event`, `property_value`, `alarm`, and the role
  tables' name-addressed columns, tracked in
  [#314](https://github.com/hyperscaleav/omniglass/issues/314). An alarm today is **written by an operator or
  an API caller**, not produced by an [`event_rule`](/architecture/alarms-actions/) over datapoints; the rule
  that opens and clears one automatically is the next tier. Also deferred: **system-** and **location-owned**
  alarms (the alarm arc is component-only today), the **`unknown`** verdict with its coverage and staleness
  reasons, the **`global`** estate top, the **SLI / SLO / SLA** family and the **KPI** set, an alarm's
  interaction with **operational mode** (maintenance suppressing a contribution), and **dependency
  suppression**.
- **Supersedes:** three earlier calls on [health](/architecture/health/). (a) **The value vocabulary**:
  [ADR-0003](#adr-0003-health-reads-ok-not-up) named the healthy state `ok` over an ordered
  `ok < degraded < down`; the built domain is **`healthy` < `degraded` < `outage`**, keeping that entry's
  reasoning (name the verdict, not the ping) and changing only the words, since `outage` says what a broken
  room means to the people in it. (b) **Where impact is declared**: the design hung an optional `health`
  impact on the **`event_rule`**, so an alarm moved its owner's health directly. Impact now lives on the
  **role**, and an alarm reaches a system **only** through the capabilities it degrades. An alarm on a
  component that fills no role moves that component's own verdict and nothing above it, which is the correct
  answer and was previously an accident of tagging. (c) **`health_role`**: the `required` / `redundant` /
  `informational` member tag on a `system_template_member` is superseded by **quorum plus impact** on a
  `system_role`, which expresses the same three cases without a fourth vocabulary (required is quorum 1 with
  impact `outage`, redundant is a quorum below the number assigned, informational is impact `none`). It also
  closes [ADR-0049](#adr-0049-the-system-role-capability-gated-staffing-and-the-resolved-capability-set)'s
  deferral of `impact` and its "quorum ships without health" note.
- **Tracked under** epic [#266](https://github.com/hyperscaleav/omniglass/issues/266). This is **PR8** of the
  estate-model shift toward property / event / command plus vendor / product / driver / capability / standard /
  role / health, and the slice that **closes the epic**: it is the one that consumes what the previous seven
  built.
- **Amended by [ADR-0087](#adr-0087-capability-gated-staffing-retires-an-alarm-impairs-its-component-not-a-named-capability):**
  chain item 1 above (capability as the routing key: an alarm names the capabilities it degrades, a component
  satisfies a role only when it provides every required one) retires with the whole capability registry
  ([#626](https://github.com/hyperscaleav/omniglass/issues/626)). An alarm now impairs its component's own
  verdict wholesale, and a role's occupant satisfies it whenever that verdict is not `outage`. Items 2 through
  5 (the pure judgement package, the transition-only record on `state_datapoint`, recompute-at-the-write, and a
  report computing what it serves) are unchanged.

### ADR-0051: Membership is the attachment, and a role is what it does

- **Date:** 2026-07-21 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/)
- **Context:** a component's relationship to a system was **two unrelated facts that could silently
  disagree**. `component.system_id` was a single pointer, set once at create with no path to change it,
  which no authorization and no health path ever read; `role_assignment` was many-to-many and carried what
  the component actually does. Nothing reconciled them, and the console rendered the first under the heading
  "Components" while the panel directly below listed the second, so a fully staffed system displayed
  **`0 components`**. The contradiction was visible to operators before it was understood by us.
- **Decision:** membership is a **first-class binding**, `system_member (system_id, component_id,
  is_primary)`, and a role attaches to it. **Staffing a role creates the membership**, because a component
  filling a job in a system that the system does not count as a member is a contradiction. The reverse is
  **not** symmetric: giving up a role leaves the membership, because the device is still in the room, and a
  member carrying no role (a power conditioner, a spare) is ordinary.
- **Why membership cannot simply replace the pointer:** the cascade seeds its system band from **one** row
  and ranks with `row_number() over (partition by ... order by band desc, depth asc)`, which has **no
  tiebreaker after depth**. A many-valued seed would make an effective tag, variable, or secret resolve
  nondeterministically for precisely the shared-device case. Membership is therefore many-valued while
  **`is_primary`** keeps a single answer for callers with **no system in hand**. It is a **default, not a
  resolution rule**: anything naming a system resolves against that system, and a component's first
  membership takes the default with nobody asking, so the single-system case never meets the concept.
- **Cascade from both ends, and deliberately no restrict on the component.** A binding is meaningless once
  either side is gone. `role_assignment` keeps its `on delete restrict` because deleting a component that
  fills a job would silently break a system's health; duplicating that restrict on membership would add a
  step to every component removal while protecting nothing new.
- **Backfill reads both of the old places.** The role table alone drops every component that belonged to a
  system without filling a declared role; the pointer alone drops the shared device's other systems. The old
  pointer seeds `is_primary`, since answering which system chain feeds a component's config is exactly what
  it used to do. A component left with several memberships and no pointer gets **no** default, because there
  is no honest way to guess which one was meant.
- **Thin cut:** resolution behaviour does not move in this slice. `component.system_id` stays and keeps
  feeding the four cascade resolvers unchanged, so this ships and is verified on its own.
- **Supersedes:** [core-entities](/architecture/core-entities/)'s "a truly shared device **skips the system
  layer**", which was the best available answer while the only binding was a single pointer. A shared device
  is now a member of every system it serves. It also narrows that page's `system_member` design: the shipped
  row is the binding alone, without the role column or the pin to a frozen `system_template_version`, so a
  member can exist without a role.
- **Tracked under** epic [#324](https://github.com/hyperscaleav/omniglass/issues/324), slice
  [#325](https://github.com/hyperscaleav/omniglass/issues/325).

### ADR-0052: The cascade resolves through membership, and secrets carry no system band

- **Date:** 2026-07-21 | **Status:** Accepted | **Pages:** [cascade](/architecture/cascade/)
- **Context:** [ADR-0051](#adr-0051-membership-is-the-attachment-and-a-role-is-what-it-does) made
  membership explicit but deliberately left resolution alone: the tag, variable, and secret cascades
  still seeded their system band from `component.system_id`, the write-once pointer. That left the
  pointer alive for one reason only, and left the "config differs per system" case unanswerable.
- **Decision:** the system band is seeded from **`system_member`**. Tag resolution **takes the system
  to resolve against**, and resolves against it only if the component is a member: naming a system it
  has no binding to must not lend it configuration. With **no system given** it falls back to the
  **primary** membership, which is the entirety of what `is_primary` is for. `GET
  /components/{name}/effective-tags?system=` exposes the first case.
- **The seed stays single-valued, as a correctness requirement.** The rank orders by band then depth
  with no tiebreaker after that, so two seeds in one band resolve nondeterministically. Membership is
  many-valued; the chain it feeds is not. This is the same fact that made the pointer worth keeping
  under ADR-0051 and is now satisfied without it.
- **Secrets lose the system band entirely**, on ownership rather than determinism: an interface
  belongs to a component, a shared device has one password, and the room it serves is the wrong owner
  for a credential. It also removes the one case where an ambiguous inheritance would have been
  dangerous rather than merely wrong.
- **`component.system_id` is dropped.** With nothing reading it, the column, its API field, and its
  console consumers go. The component body now reports `system` (the primary, by **name**) and
  `system_count`, which also retires one of the three places the API emitted a raw uuid for a field it
  accepts by name ([#328](https://github.com/hyperscaleav/omniglass/issues/328)).
- **Written test-first because the failure mode is silent.** A mis-seeded `sys_chain` is still valid
  SQL that returns fewer rows: a system-owned tag would simply stop reaching its components, with no
  error and no 500, and the resolution blade would show the location winner as though the system band
  never had a candidate.
- **Supersedes** [cascade](/architecture/cascade/)'s "the primary-system pointer is the single system
  chain that feeds the cascade", which described the mechanism when a pointer was the only binding
  available.
- **Tracked under** epic [#324](https://github.com/hyperscaleav/omniglass/issues/324), slice
  [#327](https://github.com/hyperscaleav/omniglass/issues/327).

### ADR-0053: A name is the address, a uuid is identity

- **Date:** 2026-07-21 | **Status:** Superseded in part by
  [ADR-0056](#adr-0056-every-foreign-key-stores-a-primary-key). Its API half stands: a reference is
  addressed by name. Its schema half ("a new table references an estate entity by `name` with `on
  update cascade`") is reversed, and responses now carry the id **beside** the name rather than
  instead of it.
- **Context:** the pattern was real and dominant but never applied to the original entities.
  Eleven tables keyed their estate references by `name`, six by `id`, split by age. Worse, the API
  **accepted names on write and returned uuids on read**: a component created with
  `{"parent": "rack", "location": "hq-b1"}` read back as `{"parent_id": "0198f...", "location_id":
  "0198f..."}`. The body did not round-trip, so every client fetched a second collection and joined by
  uuid to render one label. The console carried exactly that map until it was deleted in
  [#329](https://github.com/hyperscaleav/omniglass/issues/329).
- **Decision:** every request **and response** addresses another entity by its **name**. A uuid appears
  only as an entity's own `id`. Two exceptions: an entity with **no name** (an interface, a stored
  value, an audit row, a grant, a principal) and a **slug-keyed catalog**, whose id already is a name.
  A new table references an estate entity by `name` with `on update cascade`.
- **Normalized:** `parent_id` and `location_id` on component and system, `parent_id` on location, and
  the redundant `owner_id` on tag bindings, variables, and secrets, which already carried `owner_name`
  beside it. Nine fields, seven of them found by survey and **two by the guard test**, which caught a
  `SecretBody.owner_id` the survey missed.
- **Enforced by contract, not prose.** `TestResponsesAddressEntitiesByName` walks the generated
  OpenAPI and fails on any field naming another entity by uuid. The failure it prevents is invisible
  otherwise: a body emitting `parent_id` still serves 200s, and the cost only appears in the clients.
- **Deliberately not in scope:** `secret`, `variable`, and `tag_binding` still key their owner arcs by
  uuid in the schema, and the cascade resolvers compare those uuids directly. Converting them is a data
  migration plus a rewrite of resolution SQL reworked in
  [ADR-0052](#adr-0052-the-cascade-resolves-through-membership-and-secrets-carry-no-system-band).
  The API contradiction is what operators saw and is fixable without touching resolution. The rule binds
  new tables; the stragglers convert when something else needs to touch them.
- **Breaking.** Response shapes change. At v0.0.0 this is the right moment, since the cost only grows.
- **Tracked as** [#334](https://github.com/hyperscaleav/omniglass/issues/334), following
  [#328](https://github.com/hyperscaleav/omniglass/issues/328).

### ADR-0054: The shell owns a panel's action rail; the body registers and never draws

- **Date:** 2026-07-21 | **Status:** Accepted | **Pages:** [design system](/contributing/design-system/)
- **Decision:** A panel's action buttons are **declared as data, not laid out as markup**. A blade body
  binds `destructive` / `secondary` / `primary` (plus the Edit -> Save cycle) through `lib/blades`; a
  Drawer form body binds `submitLabel` / `submitIcon` / `submit` / `busy` / `disabled` / `cancel`
  through `lib/formactions`. `BladeStack` and `Drawer` each compose their own button vocabulary but
  draw it through the single `PanelFooter` rail. A form body renders **no footer markup at all**.
- **Context:** The blade already worked this way. The Drawer did not: its rail was an opt-in
  `DrawerFooter` helper that every form body had to remember to import and wrap its buttons in.
  Fourteen forms remembered. Two did not, and hand-rolled their own right-aligned row instead, where
  they survived nine merged PRs unchanged while the helper was copied into six newly added pages
  around them. The cost was not only the two misses: among the forms that did use it, some rendered a
  Cancel and some did not, some spun a spinner and some swapped the label to "Creating...". A rail
  reached by convention drifts in both directions at once.
- **Why a slot rather than a lint rule:** A lint rule finds the miss after it is written. A slot makes
  it unwriteable: there is no exported helper to forget, and a body that wants a button has exactly
  one way to ask for one. The enforcement is the deleted export, and `rail-ownership.test.ts` is the
  belt to that braces.
- **Scope, honestly:** This converges **two** of the three rails. Full-page create forms
  (Locations, Systems, Components) still draw an inline `border-t ... pt-4` row of their own. That
  rail is inline in a scrolling page rather than pinned to the viewport, so it is a different layout
  problem, and it converges when the CRUD form primitive lands and owns both form factors.
- **Deliberate convergences:** submit labels no longer change while in flight (the shell's spinner
  says it), and the new-interface blade lost its Cancel button, since a blade already dismisses two
  ways and no other blade in the stack carries one.
- **Tracked under** [#332](https://github.com/hyperscaleav/omniglass/issues/332).

### ADR-0055: The tag, variable, and secret owner arcs key by name

- **Date:** 2026-07-21 | **Status:** Superseded by [ADR-0056](#adr-0056-every-foreign-key-stores-a-primary-key), which
  converts these nine columns back to uuids along with every other name-keyed foreign key. Kept in
  full because the reasoning below is a worked example of the mistake: it is internally consistent,
  it shipped green, and it is wrong at the premise.
- **Context:** [ADR-0053](#adr-0053-a-name-is-the-address-a-uuid-is-identity) fixed what operators saw
  and deliberately left the schema alone. Three tables still keyed their owner arcs by uuid while every
  table from the collection era onward keyed by name, so the two conventions met inside single queries:
  the cascade resolvers walked chains of **uuids** purely to match these three, and a component's name
  had to be carried alongside its id to bridge them.
- **Decision:** the nine arc columns on `tag_binding`, `variable`, and `secret` become `text references
  <entity> (name) on update cascade on delete cascade`. The columns keep their `_id` suffix, matching
  `role_assignment.component_id` and `state_datapoint.component_id`, which are likewise text referencing
  a name.
- **`on update cascade` is the load-bearing clause.** A name is only safe as a key if a rename carries.
  `TestOwnerArcsSurviveARename` is the proof and is **mutation-checked**: with the clause removed the
  rename is refused outright with a foreign-key violation, so the test cannot pass vacuously. It also
  could not have failed before this change, since the arcs held uuids that a rename never touches.
- **The resolvers now project names.** Each chain still **recurses on `parent_id`**, which stays a uuid,
  and only what it projects changed. `owner_id` in the `owners` CTE is a name, so the final joins that
  resolve a display name match on `name` rather than `id`.
- **The scope walk still uses ids**, resolved from the name at the point of the check. Identity stays
  internal, and the walk is the only place that needs it, which is the rule working as intended rather
  than an exception to it.
- **Not converted, deliberately:** `tag_binding.node_id` references `node.principal_id`, a node's
  enrollment identity and the only handle it has. `tag_binding.tag_id` is a genuine instance of the same
  rule (`tag` is uuid-keyed with a unique name) but is the binding's **subject** rather than its owner,
  and it touches the tag CRUD surface rather than resolution; tracked as
  [#340](https://github.com/hyperscaleav/omniglass/issues/340). Removing it from the guard test's
  slug-keyed allow-list, where it had been listed on a **false claim** that the tag catalog is
  slug-keyed, is part of this change.
- **No `migrate:down`.** Reversing would have to resolve names back to uuids the forward migration no
  longer records, and any rename since would make that resolution wrong rather than merely absent.
- **Tracked as** [#339](https://github.com/hyperscaleav/omniglass/issues/339).

### ADR-0056: Every foreign key stores a primary key

- **Date:** 2026-07-22 | **Status:** Accepted; the slug-keyed carve-out below is retired by
  [ADR-0062](#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle) | **Pages:** [storage](/architecture/storage/),
  [api-first](/contributing/api-first/)
- **Decision:** every foreign key stores the target's **primary key**: a uuid for an estate entity,
  `principal_id` for a node, and the slug itself for a slug-keyed catalog (`product`, `standard`,
  `property`, `interface_type`) where the name already *is* the key. No column references a `name`.
  In exchange, the API accepts **either form** wherever a reference is written (a path segment or a
  join field in a body), trying the uuid first, and every response carries **both**: the name an
  operator reads and the id it resolves to.
- **Context: this reverses a direction set two ADRs ago.** [ADR-0053](#adr-0053-a-name-is-the-address-a-uuid-is-identity)
  found eleven tables keyed by name and six by id, and resolved the split by declaring the majority
  correct. [ADR-0055](#adr-0055-the-tag-variable-and-secret-owner-arcs-key-by-name) then converted the
  six to match, and the follow-on work was converting the rest. The premise was never examined: a
  friendly, renameable key is valuable *because* it can change, which is the one thing a foreign key
  must not do.
- **The tell was `on update cascade`.** ADR-0055 called it "the load-bearing clause" and was pleased
  that removing it made the test fail. That machinery exists only to fund the wrong choice: it is
  write amplification across every referencing row, on a rename, to protect a key that did not need
  to be a name. A uuid arc needs no clause, and the equivalent test passes because there is nothing
  to rewrite.
- **A rename was not merely inefficient, it was refused.** With `interface.component` referencing
  `component (name)` and no `on update` clause, renaming a component that owned any interface failed
  outright with a foreign-key violation. The name-keyed convention had spread past the point where
  its own cascade covered it, and the operator-facing symptom was a rename that returned an error.
- **Scope:** all 30 name-keyed foreign keys, converted across five slices grouped by subsystem rather
  than by table, so each file changed once: the estate arcs, then health and roles, then the
  collection tier (`metric_datapoint`, `interface`, `node`) and every node reference.
- **What stays a name.** The columns whose target is slug-keyed are already conformant and were not
  touched. Health passes names internally on purpose: its advisory lock hashes `health/<kind>/<name>`,
  and a mixed currency would hash two keys for one owner and silently stop serializing. (**That
  sentence stopped being true at #627; see the amendment below.** It is left standing because the log
  is append-only and because its argument is the one that reversed it.) (The
  slug-keyed targets themselves later took uuid keys too, so those columns moved to the uuid; see
  [ADR-0062](#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle).)
- **Amended (#717): the health carve-out above is no longer true, and what unmade it is the reason it
  was a carve-out at all.** The lock hashes **`health/<kind>/<id>`**, and has since the identity epic
  ([#627](https://github.com/hyperscaleav/omniglass/issues/627)) landed its addressing slice
  ([#647](https://github.com/hyperscaleav/omniglass/issues/647)); health resolves a reference to the
  row's id once, before any lock is taken. A name-keyed lock partitions an estate only while names
  partition it, and that epic scoped a location's name uniqueness to its **placement**, so two rooms
  under different buildings may both be `415a`. One key for two unrelated owners is a silent loss of
  concurrency; the mixed currency this bullet warned about is a silent loss of the serialization the
  compare-then-act recompute needs for correctness. Both halves of the argument survive, and only the
  conclusion moved. The same fact moved the lock ORDER a slice later, because a comparison that leaves
  two owners tied is not an order at all
  ([#670](https://github.com/hyperscaleav/omniglass/issues/670)); the [health](/architecture/health/)
  page has said `id` since. This entry was the last page in the corpus describing a keying scheme that
  would be a live defect if implemented from it, so the key now lives in one named function
  (`healthLockKey`) with a unit test asserting that two same-named rooms do not share a lock.
- **Guarded both ways.** `TestResponsesAddressEntitiesByName` fails on a response that names an entity
  by uuid alone; the per-tier rename tests fail if an arc stops following a rename. Each conversion was
  **mutation-checked** rather than trusted: breaking the projection had to turn the suite red.
- **Tracked as** [#343](https://github.com/hyperscaleav/omniglass/issues/343).

### ADR-0057: The cascade's least-specific tier is `platform`, and a `default` is not a tier

- **Date:** 2026-07-21 | **Status:** Accepted | **Pages:** [cascade](/architecture/cascade/), [settings](/architecture/settings/), [config, secrets, and variables](/architecture/variables/), [tags](/architecture/tags/), [identity and access](/architecture/identity-access/), [scaling and deployment](/architecture/scaling/)
- **Decision:** Six calls, one vocabulary.
  1. **`global` becomes `platform`** as the cascade's least-specific **binding** tier, on **both** axes: the
     estate arc (`owner_kind` on `variable`, `secret`, and `tag_binding`) and the settings level
     (`setting_override.scope`). It occupies exactly the rung it occupied before (`segment_rank 0`). It is a
     decision like every other rung, what an admin set for the **whole install**, not a floor beneath the chain.
  2. **`code` becomes `default`, and a `default` is off the axis on both engines.** A default is what a value
     **is** absent any decision: a column on a definition row, beside the unit, the kind, and the validation
     rule. It is not a rung, it shadows nothing, and nothing shadows it; the fold **falls through** to it when
     no rung bound anything, and the resolve view reports it as a declaration rather than as a winning source.
     A default is a column on a declaration row, so **a kind with no declaration row has no default**: a
     setting has one (its struct tag) and a property has one on its classifier's contract
     (`product_property.default_value` and its `standard_property` / `location_type_property` siblings, read as
     `coalesce(the instance's set value, the contract default)` by `EffectiveProperties`,
     [ADR-0047](#adr-0047-the-fields-fold-product_property-and-property_value)), while a variable, a secret,
     and a tag have none. Absent means absent. Note the property default sits on the **contract**, not on the
     `property` catalog entry: the catalog declares what a name means, a classifier declares what it is for
     the things that conform to it. That is a narrower claim than the one this ADR was drafted against, when
     the precedent was the retired `field_definition.default_value` hanging off a `component_type`, and it is
     the stronger one for the rule here, since the coalesce is the fall-through in the code path itself.
  3. **There is no root location.** The location tree keeps N unparented tops. A tier above today's tops is a
     new `location_type` and a real node, never a magic one, and a top-level location is not a substitute for
     `platform`: binding at one top misses every sibling, and a top added later is silently uncovered.
  4. **The install-wide tier survives on the estate axis**, uniform across kinds:
     `platform | location | system | component`.
  5. **A write at the tier needs `platform:<action>`**, checked in addition to the resource permission and
     published per route as an `x-omniglass-platform-permission` stamp. This separates full-estate **scope**
     from install-wide **authority**: a senior operator may hold an all-scope grant without being able to
     change the value that applies to the whole install. `platform:*` is seeded to `admin` (and reaches
     `owner` through `>`); `operator` and `deploy` hold no `platform` write, and nothing implies one.
     "Nothing implies one" is enforced by putting `platform` in the **sensitive-resource set** beside
     `secret` and `settings` ([ADR-0025](#adr-0025-secret-is-a-sensitive-resource-a-per-secret-admin_sensitive-flag-flips-a-secret-to-the-admin-tier)),
     so a bare single-token `*` never names it: a custom role carrying `*:update` holds every estate write
     and still no install-wide authority. Only a literal, a `platform:*`, or a `>` names the tier.
  6. **`root` is not used as a tier name**, so `location_type.allowed_parent_types` keeps its reserved `"root"`
     sentinel meaning "top, no parent", unchanged.
- **Context:** One word named two unrelated things, in two engines, with three spellings. On the estate axis
  `global` was both a **tier** an operator writes at and, in the prose, a **floor** where ship-with policy
  supposedly lived: [cascade](/architecture/cascade/) read "Ship-with default policy lives at `global`, the
  floor of the chain", three lines under a heading that says the registry is **outside** the cascade. That was
  drift, not design: `internal/seed/` writes eight YAML files and every one defines a **type**; none writes a
  **binding**, and there has never been a ship-with row at the tier. Meanwhile the settings engine had already
  split the two ideas and picked different words for them (`code` for the declaration, `global` for the
  install-wide override), so the same distinction existed twice under three names. Separately, `global` also
  names the singleton estate **owner** where health and KPIs roll up (a different concept that keeps the name),
  which made "global" ambiguous in the one place ambiguity is most expensive. Naming the binding tier
  `platform` and the declaration `default` gives each idea one word, and it drops an assumption the estate
  never had: that "everything" and "the planet the sites are on" are the same thing.
- **What does not change:** no precedence change (every row keeps its rung under a new name, so no deployment
  resolves differently), no new rows (the migration renames a value, inserting nothing and adding no column),
  no reordering of the segment ranks or the comparison key, and no capability removed.
- **Breaking change, accepted deliberately:** `secretAAD` binds a sealed field to its owner arc,
  `ownerKind|ownerID|name|field`. A secret sealed at the tier **before** this rename authenticates against
  `global|global|...`; after it, the derivation yields `platform|platform|...`, the AEAD check fails, and
  **that ciphertext never opens again**. Only the renamed tier is affected: a scoped secret carries a real
  owner id and is untouched. Accepted because no deployment holds tier secrets yet, and each alternative
  (freezing the AAD at the legacy string, a Go-side re-seal backfill, or a reveal-time fallback) buys
  compatibility nothing currently needs at the price of a permanent legacy branch. Recorded here rather than
  discovered later by a reader.
- **Amends:** [ADR-0033](#adr-0033-settings-persist-only-the-override-level-base-layers-are-recomputed-in-memory),
  [ADR-0034](#adr-0034-the-settings-gateway-is-unscoped-only-the-permission-gates-it),
  [ADR-0035](#adr-0035-settings-resolve-as-a-cascade-over-principals-with-a-broader-wins-lock), and
  [ADR-0042](#adr-0042-field-cascade-and-the-type-default-floor), and
  [ADR-0047](#adr-0047-the-fields-fold-product_property-and-property_value). Each keeps its model; only the
  level names and the default's place in the vocabulary move.
- **Closes:** issue [#316](https://github.com/hyperscaleav/omniglass/issues/316).

### ADR-0058: A run mode is a verb under its noun, and no command may be shadowed

- **Date:** 2026-07-22 | **Status:** Accepted | **Pages:** [CLI guide](/guides/cli/)
- **Decision:** the edge run mode becomes **`omniglass node run`**, a leaf under the generated
  `node` group, rather than a top-level `node`. A **guard test walks the assembled command tree
  and fails on any duplicate name**, so the hand-written and generated command sets can no longer
  collide silently.
- **Context:** the hand-written run mode and the generated API group both registered as `node`.
  Cobra does not treat that as an error: both are added and lookup returns the first, so **every
  generated node command was unreachable**. `omniglass node list` resolved to the daemon and
  failed asking for `--token`, while the CLI guide documented it as working.
- **Why a guard rather than a rename alone.** This is the third instance: `members` under the
  principal groups (#326), `type list` (#319), and now `node`. Each was found by a person typing
  it. The two command sets compose on one root, so no single file owns the namespace and no review
  of either set can catch it. The tree is the only place they meet, so it is the only place the
  check can live. The guard was written first and **found two more nobody had reported**: `grant
  create` and `grant delete`, where the principal-group variants shadow the principal ones, so
  granting a role to a principal has no CLI path at all (#357).
- **The known collisions are an explicit list that may only shrink.** The guard fails on any name
  **not** on it, so a new collision cannot land, and it also fails on an entry that has **stopped**
  colliding, so a fix must delete its entry rather than leave that name unwatched. It is a ratchet,
  not an allow-list.
- **Root cause, left for #357:** `commandWords` derives the group from a single path segment, so
  `/principals/{id}/grants` and `/principal-groups/{id}/grants` both become `grant`. Fixing that
  renames documented commands and is a naming decision, not a mechanical one.
- **Cost accepted:** `omniglass node` is a documented invocation and it changes. At v0.0.0 that is
  a docs edit, and a mode reads as a verb anyway, beside `node list` and `node enroll`.
- **Tracked as** [#354](https://github.com/hyperscaleav/omniglass/issues/354).

### ADR-0059: Every collection segment is a command level

- **Date:** 2026-07-22 | **Status:** Accepted | **Pages:** [api-first](/contributing/api-first/), [CLI guide](/guides/cli/)
- **Decision:** the CLI command path is derived from the **whole route**: every collection segment
  contributes a level and the verb is last, so a subresource is always addressed under the resource
  that owns it. `/components/{name}/properties` is `component property list`;
  `/principals/{id}/grants` and `/principal-groups/{id}/grants` are `principal grant create` and
  `principal-group grant create`.
- **Context:** the old rule used only the collection **nearest the leaf**, so it could not tell two
  parents apart. Across 195 operations it produced **24 collisions**, `property list` seven ways.
  Cobra does not treat a duplicate name as an error: it registers both and returns the first, so the
  second was unreachable and the only symptom was a command that ran the wrong thing. Granting a role
  to a principal had no CLI path at all ([#357](https://github.com/hyperscaleav/omniglass/issues/357)).
- **`nameOverride` was the rule, written out by hand fifty times.** It had grown to 53 entries, and
  the comment on nearly every one said the same thing: "the leaf-noun heuristic would collapse both
  into one group." Each was added after somebody typed a broken command. It is now **14 entries**,
  all of them the genuinely non-AIP `/auth` family, and none of them about a collision.
- **A name depends only on its own route.** This is the property worth having: under a
  disambiguate-only-when-ambiguous rule, adding a route could rename an existing command. Here it
  cannot, so the naming is stable as the API grows.
- **The grouping had to become a tree.** Fixing the derivation alone was not enough: the generator
  bucketed commands by their first word and used only the **last** word as the leaf name, so a
  three-word path rendered as two and collided again. It now builds an N-level tree, which is also
  what makes `node run` and `type secret list` render as written.
- **`omniglass statu` shipped.** The depluralizer took `-s` off `status`. A small irregular set is
  declared instead; the route vocabulary is ours, so this is a known list, not an English problem.
- **Cost accepted:** 67 of 202 commands are renamed, 135 unchanged. At v0.0.0 that is a docs edit,
  and the guides are corrected mechanically from the route map in the same change.
- **Two guards, because regeneration does not fix prose.** `TestNoCommandNameCollisions` fails on any
  duplicate name (its known-collision list is now **empty**, and its second half forced those entries
  out once fixed). `TestDocsOnlyNameRealCommands` walks the guides and fails on a documented command
  that does not resolve; it immediately found `omniglass secret-type list`, which had never existed in
  any build, and two commands with no API route behind them
  ([#359](https://github.com/hyperscaleav/omniglass/issues/359)).
- **Supersedes** the naming half of [ADR-0058](#adr-0058-a-run-mode-is-a-verb-under-its-noun-and-no-command-may-be-shadowed),
  whose guard this keeps and whose exception list this empties.
- **Tracked as** [#357](https://github.com/hyperscaleav/omniglass/issues/357).

### ADR-0060: A resource is one kebab-case noun; nesting means ownership

- **Date:** 2026-07-22 | **Status:** Accepted | **Pages:** [api-first](/contributing/api-first/), [API](/architecture/api/), [types](/guides/admin/types/)
- **Decision:** a resource is addressed by **one kebab-case noun**, and a nested path segment means the
  nested thing is **owned** by it. The `/types` umbrella is retired: `GET/POST /location-types`,
  `PATCH/DELETE /location-types/{id}`, `GET /secret-types`.
- **Context:** the location type registry was addressed **two ways**. Its CRUD lived under
  `/types/location` while its property contract lived on a flat `/location-types/{id}/properties`, so
  one entity had two command groups (`type location update` and `location-type property list`) and an
  operator had to know both. `/types/secret` had no flat form at all.
- **The umbrella misused nesting.** A nested segment says the child belongs to the parent
  (`/principal-groups/{id}/members`). `location` is not owned by `types`; it **is** a registry that
  happens to be one of several. Grouping by category is a documentation concern, not an addressing one.
- **Two mechanisms, now unambiguous.** A hyphen joins a noun that happens to be two words
  (`principal-group`, `location-type`, `audit-log`, `effective-tag`); a space means the thing beneath it
  (`location-type property`, `principal-group member`). Before this, the same registry used both, which
  is what made the rule unstateable.
- **Found by asking what the rule was**, not by a failure. The CLI naming fix
  ([ADR-0059](#adr-0059-every-collection-segment-is-a-command-level)) made the two spellings sit next to
  each other in one command tree, where the contradiction was obvious. The generator was correct
  throughout; the routes disagreed with themselves.
- **Addressing only.** Same handlers, same `<resource>:<action>` gates, same scope injection, no storage
  change. The `type` command group disappears and `nameOverride` needs no entry for any of it.
- **Breaking:** three route shapes change. At v0.0.0 that is a regeneration plus a docs pass, and
  `TestDocsOnlyNameRealCommands` fails on any guide left teaching the old names.
- **Tracked as** [#361](https://github.com/hyperscaleav/omniglass/issues/361).

### ADR-0061: A calculated series is current at its highest id, not its newest timestamp

- **Date:** 2026-07-22 | **Status:** Accepted | **Pages:** [datapoints](/architecture/properties/)
- **Decision:** for a **calculated** series (health, and anything else the engine derives), the current
  value is the row with the **highest id**. `ts` records when the value was computed and is for display
  and history; it does not decide which row is current. For an **observed** series, `ts` still orders,
  because it is the observation time and a late arrival must not displace a newer reading, but `id`
  breaks a tie.
- **Context:** `recordHealth` writes `select clock_timestamp(), ...`, so the timestamp is evaluated in
  the SELECT list while the id comes from the identity sequence applied when the row is inserted: the
  clock is read **before** the id is assigned. Two concurrent inserts can therefore commit with `ts`
  inverted relative to `id`, and a reader ordering by `ts` then disagrees with the writer about which
  row is current.
- **Production was already right, the test was not.** Every production reader of a recorded verdict
  (`recordHealth`'s own transition check, `subtreeSystemHealth`) orders by `id`, so the writer and the
  readers agreed. The health test helper ordered by `ts`, which is why it reported verdicts the engine
  never produced. The intermittent failure was in the harness, not the product.
- **`LatestState` is a real exposure and is fixed here too.** It backs the ingest transition guard and
  ordered by `ts` alone, so a poll cycle stamping several rows in one instant resolved to an arbitrary
  one and the guard could compare against a row that is not current. It now tie-breaks on `id`.
- **Reproduced deliberately rather than waited for.** The failure needs contention: it never appeared in
  nine consecutive full-suite runs on an idle machine, and appeared within one or two attempts when six
  copies of the storage package ran at once. Under that same load the fix held for 24 runs.
- **The regression test writes the inversion directly** rather than racing it into existence, and asserts
  the two orderings genuinely disagree before asserting the outcome, so it cannot pass vacuously. It
  reads through `LocationHealth`, which reports the **recorded** verdict; `SystemHealth` recomputes live
  and cannot witness the defect.
- **Tracked as** [#356](https://github.com/hyperscaleav/omniglass/issues/356).

### ADR-0062: A registry takes a uuid primary key and a renameable handle

- **Date:** 2026-07-22 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/), [api-first](/contributing/api-first/)
- **Decision:** a registry has a **uuid `id`** and a **unique, renameable `name`**, the shape `tag` and every
  estate entity already have. `product` and `vendor` convert first; the remaining seven follow, slice by
  slice, tracked as [#262](https://github.com/hyperscaleav/omniglass/issues/262).
- **Context:** [ADR-0056](#adr-0056-every-foreign-key-stores-a-primary-key) says every foreign key stores
  its target's primary key, and epic #343 made that true everywhere **except** the slug-keyed registries,
  where the name *is* the key. That exception was the last place a foreign key referenced a mutable,
  human-authored string. A product id was a typo or a rebrand away from being wrong forever, and two
  device packs both defining `cisco-room-kit-pro` collide on the primary key itself.
- **`name`, not `slug` or `key`.** Six tables and every estate entity already call the human handle `name`,
  and the API bodies already say `name`. A third word for the same concept would be worse than the
  inconsistency it fixed. Renaming the family to `slug` later is a separate, mechanical decision.
- **The registries already disagreed with each other**, which is worth recording: `property` and
  `interface_type` call their slug `name`, while `capability`, `driver`, `location_type`, `secret_type`,
  and `standard` call theirs `id`. So the later slices are a **rename** for five of them and an addition
  for two, not one uniform change.
- **`node` stays the exception.** Its primary key is `principal_id`, because a node is the detail row of a
  principal and its key IS that foreign key. It is deliberate and it is not changing.
- **The API carries both and accepts either**, as the estate entities do: `id` (uuid) and `name` (handle)
  on every body, and a path or reference resolves whichever form it is given. A name can never
  look like a uuid, so the two cannot collide.
- **The rename test is written first, each slice.** It renames a handle and asserts every reference still
  resolves and now reads the new one. That is the capability the epic buys, so it is what the slice proves.
- **The exception is retired, not just the tables.** With all nine registries converted, a closing slice
  removes the slug-keyed carve-out from the doctrine: the [api-first](/contributing/api-first/) rule now
  states every foreign key stores a uuid with no exception, [ADR-0056](#adr-0056-every-foreign-key-stores-a-primary-key)'s
  carve-out is marked retired, and the `TestReferencesCarryBothForms` guard drops its slug-keyed
  allow-list (the registry references move into the both-forms rule, and the one that surfaced a real
  gap, a component response that carried `product_id` without the product's name, is fixed). The
  storage helper collapses too: the per-registry `productRefCol` / `vendorRefCol` and the
  `registryHandles` set fold into one `registryRefCol(ref)`, since every registry now behaves the same.
- **Extended by [ADR-0089](#adr-0089-a-uuid-is-the-address-a-dotted-path-is-a-positional-lookup):**
  this decision's dual-accept clause already said a reference "resolves whichever form it is given";
  ADR-0089 makes a third form, a dotted path, a real one for `location`, `system`, and `component`,
  resolved structurally to a uuid before the ordinary name-or-id lookup runs.

### ADR-0063: The telemetry model is typed registries over bare-noun data tables

- **Date:** 2026-07-23 | **Status:** Accepted | **Pages:** [datapoints](/architecture/properties/), [events](/architecture/events/), [variables](/architecture/variables/), [storage](/architecture/storage/), [glossary](/architecture/glossary/)
- **Decision:** every component interaction normalizes to one of three **registries**, each suffixed
  `_type` (`property_type`, `event_type`, `command_type`), over bare-noun **data tables**
  (`metric`, `state`, `property`, `event`, `command`). A registry is a classification, so it takes the
  `_type` suffix; the bare noun holds the instances. This retires the last confusion left by the
  [datapoint_type to property rename](#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle):
  today's `property` registry becomes `property_type` (`kind` in `{metric, state}`), and today's
  `property_value` becomes `property`, the latest-value cache. `event` and `command` gain their own
  registries and are no longer modeled as a `property` kind.
- **Context:** using bare nouns for registries was the root inconsistency. `property` named the
  registry while `property_value` named the data; `event` had been folded into the property registry as
  `kind=log`, the "false unification" [datapoints](/architecture/properties/) and
  [events](/architecture/events/) explicitly warn against; and the code said `property` while the pages
  still said `datapoint_type`. Suffixing the registry `_type` and freeing the bare noun for the data
  fixes all three at once, and it **vindicates the two-registry separation the pages always wanted**:
  `property_type` and `event_type` stay distinct catalogs, they were never one universal registry.
- **The reusable pattern.** A **registry** (`<noun>_type`) defines canonical entries. A **realization**
  (the bare `<noun>`) records data referencing one registry entry by FK, over the same exclusive **owner
  arc** (component / system / location / node) the estate already uses, tagged with a **provenance** or
  **origin**. `metric`/`state`/`property` reference `property_type`; `event` references `event_type`;
  `command` references `command_type`. One rule, four tables, no bare-noun registries.
- **A log is a collection of events, so there is no `log` table.** The row is an `event` (one
  occurrence); a log is the *stream* of them, the way a registry is a collection of keys. So the earlier
  plan to rename the occurrence table `event` to `log` is **reversed**: the table stays `event`, and "a
  component's log" is a **query** over its events (observed origin). Component-observed versus
  platform-derived is an `origin` on the row, not a separate table; promoting a raw line into a typed
  event is enrichment of the row, not a move between tables.
- **The owner arc stays; owner-prefixed tables (`component_metric`, `system_metric`, ...) are
  rejected.** The exclusive arc already carries a component's and a system's metrics in one table, and it
  is the estate's established primitive (`property`, `tag_binding`, `secret`, `variable` all use it).
  Splitting by owner would multiply the firehose tables fourfold, fragment the hot path, and force a
  UNION for the query that matters most: a system's health rolling up its components' metrics wants one
  table. The only gain is a non-null single FK, which the arc's check already enforces logically.
- **`property` is a latest-value cache; the firehose stays `metric`/`state`.** `property` holds the
  newest value per **series**, `(owner, property_type, instance, provenance)`, the same series identity
  the firehose uses, **upserted on intake**. `metric` and `state` remain the append-only samples. The
  cache exists to answer "what is it now" and "what did we last tell it" without scanning the firehose.
- **Provenance in the cache is rows, not columns.** Each provenance is its own series row (`observed`,
  `calculated`, `intended`), so `observed=45` and `intended=50` are two rows, not two columns. Columns
  would put device-intake, command, and config all updating the same row (lock contention, lost updates
  on the hot path) and bake the provenance set into the schema. The "want / told / is" one-liner is the
  right **read** shape, delivered as a **pivot view** over the rows: read-shape is not write-shape.
- **`declared` resolves on demand; `intended` is stored.** The config setpoint (`declared`) is resolved
  live from the cascade and never rows into the cache, because it is always current and needs no history.
  The last commanded value (`intended`) **is** stored, because settlement needs the fact plus its `ts`.
  So the cache's provenance set is `{observed, calculated, intended}`.
- **Two different drifts fall out of that split.** **Command settlement** compares `observed` to
  `intended`, is **windowed**, and is short-lived ("did my last command take?"). **Config drift**
  compares `observed` to `declared` (resolved live), is **ongoing** ("is it where config wants it?").
  This is exactly why `intended` is stored and `declared` is not.
- **The settle window is a driver fact, carried on `command_type`.** How long a command takes to
  actuate (an input switch is near-instant, a lamp warmup is tens of seconds) is device-physical, so it
  lives on the **driver**, on the `command_type` the driver populates (the driver as a declarative menu
  of canonical properties, events, and commands), not on the abstract `property_type`. Settlement is
  **computed**, never a stored flag: within `now - intended.ts < settle_window` the value is pending and
  drift is suppressed; past the window, `observed` matching `intended` is settled and a mismatch is a
  failed command.
- **Staging.** The **name foundation** is cheap and lands first: `property` to `property_type`,
  `property_value` to `property`, and the `metric`/`state` FK repoint, a wide but mechanical sweep with
  no behavior change. The **event family** (`event_type`, the `origin` and causation columns, pulling
  `kind=log` out of `property_type`) rides the calculation and promotion layer, still `Design`. The
  **command pillar** (`command_type`, `command`, the settle window, command settlement) is greenfield.
  Each architecture page is rewritten to this model in the slice that builds its part, per
  [docs with everything](/contributing/docs-with-everything/); until then the pages carry an inline
  note pointing here.

### ADR-0064: Placement and classification are mutable after create

- **Date:** 2026-07-23 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/), [api](/architecture/api/)
- **Decision:** a component's **product**, **location**, and **parent**, and a system's **location**
  and **parent**, are patchable after create, not fixed at creation. Each follows the house
  **three-state** convention on `PATCH`: an omitted field is unchanged, an explicit empty string
  **clears** (a productless one-off, an unplaced entity, a root), and a name **sets**. A re-parent is
  **cycle-guarded** (refused when the new parent is the entity itself or one of its own descendants,
  the same recursive walk `location` already uses) and **scope-injected** (the new parent must sit
  inside the caller's update scope). The existing per-transaction audit and health recompute rules are
  unchanged.
- **Context:** the create body accepted these fields, the update body did not, so a component
  classified wrong on import or a display that physically moved rooms had **no path back except delete
  and recreate**, which destroys its telemetry history. The gap was invisible from any single body; it
  only showed up as the set difference between the create and update schemas, which is what surfaced it
  ([#342](https://github.com/hyperscaleav/omniglass/issues/342)).
- **Why product is the sharp one:** [ADR-0047](#adr-0047-the-fields-fold-product_property-and-property_value)
  made `product` the carrier of the property contract, so a wrong product resolves the wrong property
  defaults. A swap therefore keeps every explicitly-set value (they key by component and property_type,
  independent of product) and lets only the unset defaults follow the new product, so re-classifying is
  never a silent data loss.
- **What is deliberately NOT carried over from the location pattern:** location's reparent also runs an
  **allowed-parent-type** placement check, because a `location_type` constrains its parents. Products
  and standards are not placement-typed, so a component and a system carry the **cycle guard and scope
  injection only**, no placement-type validation. A component and a system reparent also, unlike a
  location this slice, support clearing to a root, since a root component and a root system are ordinary.
- **Health does not move on a placement change.** The rollup runs component to systems-it-staffs to
  locations-over-those-systems, so a component's own location and parent, and a system's parent, sit
  outside the chain; only a product swap (capabilities) and a system relocate (which rung it rolls into)
  recompute, both already wired.
- **Storage was already half-built:** `ComponentPatch.ProductName` and `SystemPatch.LocationName` were
  wired but unexposed; this slice adds the two reparent paths and the component relocate, and opens all
  of it on the API. It unblocks the [CRUD form primitive](/contributing/design-system/), whose generated
  edit form reads mutability off the create-minus-update schema difference and would otherwise render
  these fields read-only.

### ADR-0065: Property, sample, and current value replace the datapoint

- **Date:** 2026-07-28 | **Status:** Accepted | **Pages:** [properties](/architecture/properties/), [storage](/architecture/storage/)

The one word "datapoint" conflated two things: the signal (what is measured) and the observation (a single reading of it). Splitting them removes the ambiguity that let "the datapoint's current value" and "a datapoint arrives" name different nouns.

- A **property** is the canonical signal on one owning entity (the key a sample observes and config declares); its registry is `property_type`.
- A **sample** is one timestamped observation of a property, a row in `metric` / `state` (the kind).
- The **current value** is the latest sample per series, held in the `property` cache.

No tables were renamed (`metric`, `state`, `event`, `property`, `property_type` stay); the shift is vocabulary across the code (the proto `Sample` message, the `*Sample` Go types, `deriveSamples`) and the docs (the value-model page is now [properties](/architecture/properties/), with a redirect from the old slug).

It also settles the built shape the earlier design left open: the **`property` cache is the architecture-of-record for current values**, a table upserted from the persistence sink with an out-of-order guard, not the "view over metric" once sketched on [storage](/architecture/storage/). The `intended` value a command opens is written in the command's Postgres transaction; the data-lane re-entry of intended and the adaptive-poll reconciliation described under [intended](/architecture/properties/#intended-the-declared-effect-of-a-command) are the deferred **actuation** evolution, not the built path. Refines [ADR-0063](#adr-0063-the-telemetry-model-is-typed-registries-over-bare-noun-data-tables).

### ADR-0066: Logs are a raw ingest lane, not events

- **Date:** 2026-07-28 | **Status:** Accepted

Pulling `log` out of `property_type` into `event_type` (#395) also promoted every raw log line, at
ingest, into a caught `log.line` event. Reviewing the seeded event types showed that conflates two
different things: a log line that **arrived** versus an occurrence that **happened**. It is the same
category slip as naming a type after its transport (the `syslog.line` to `log.line` fix): arrival is
not a happening.

**Three ingest shapes, not two.** A **property** (metric or state) is a sampled value with a current
value. An **event** is a semantic, typed happening. A **log line** is raw arrival: untyped text off a
firehose. A log line is neither of the other two, it is the substrate some events are derived from.

**Two lanes meet at `event`.** (1) The raw firehose lands in a `log_line` ingest table; derivation
rules read it and emit semantic `event` rows. (2) A component with a native event model (an xAPI
xEvent, an SNMP trap, a webhook) publishes straight to `event`. Neither lane is a subset of the other:
most log lines never become events (they are searched, retained, and aged out having produced
nothing), and native events never touch a log. That asymmetry is why `log_line` is its own table, not
a flag on `event`.

**`origin` names the producer.** `derived` (a rule produced it, from a log line or another event),
`caught` (a component published it natively), `scheduled`. The retired model's "a log line is a caught
event" collapses into: the log line is a `log_line` row, and if it matters a rule **derives** an event
from it.

**Lineage lives on the event row.** A derived event carries `source_event_id` (unified: the source is
the cause, so this replaces the separate `caused_by_event_id`), `source_log_line_id`, and
`derived_by_rule_id`, all null for a natively-caught event. The derivation engine keeps its own
execution history (which rule version fired, when, over what inputs), but that is a separate
observability lane and lineage must not depend on it: rotating the execution log cannot be allowed to
orphan an event's provenance. `derived_by_rule_id` is the bridge into that history when the full story
is wanted, without being the only home for the basic fact.

**A log line is untyped but classifiable by labels, not a registry.** There is deliberately no
`log_type` registry: the log lane's job is to swallow the firehose **without** pre-declaration, the
exact opposite of the reject-not-project contract that justifies the `property_type` and `event_type`
registries, and log classes (system, app, firmware, kernel) are device- and OS-specific and
open-ended. Classification is descriptive: a `source` channel plus freeform labels and `attributes`,
which an operator can extend without a schema change. The one exception is `severity` (and a coarse
`facility`), promoted to indexed columns because retention and routing policy keys on them (keep
firmware and error lines longer, drop debug faster). The shape is
`log_line { ts, owner, source, severity?, facility?, message, attributes, labels, correlation_id }`.

**Build gate.** The `log_line` table, retention, the derivation engine, the lineage columns, and
native-event producers are their own slice ([#410](https://github.com/hyperscaleav/omniglass/issues/410)),
worth building when logs are a real firehose to search and age out separately. This ADR records the
target; it is not built all at once.

**Consequence for #395 (the event_type family slice).** The bits that encode the retired model come
out now: the ingest log-to-event promotion and the seeded `log.line` event type. The slice keeps the
`event_type` registry, the richer `event` (origin, causation, correlation), `log` leaving
`property_type`, and a native caught example (`call.started`). The `caused_by_event_id` to
`source_event_id` rename and the new lineage columns land together in the log-lane slice, not
piecemeal here.

### ADR-0067: Bookings are exclusive-arc-owned schedules, reconciled against observed usage

- **Date:** 2026-07-28 | **Status:** Accepted

Tracking how systems and spaces are used needs the **scheduled** side, not only the observed telemetry.
A booking (a room reservation, an equipment checkout, a virtual-room reservation) is what a calendar
system holds, and it is a different shape from a telemetry event: an **interval** (a start and an end)
with a lifecycle (created, moved, cancelled), and it is **declarative**, someone reserved something.

**A `booking` is its own entity, owned through the exclusive arc.** It uses the same `owner_kind` plus
one-of (`component_id` / `system_id` / `location_id` / `node_id`) plus the CHECK that exactly one is set,
the ownership model the rest of the estate uses. The arc **is** the binding, so no separate resource
layer is needed: a room booking owns to a **location**, a mobile-equipment checkout owns to a
**component**, a virtual meeting room owns to a **system** (the bridge modeled as a virtual conferencing
system), and a bookable space with no AV owns to a **location** that has no systems. The heterogeneity
flagged during design review (not every reservation is a physical room) is exactly what the arc
already expresses.

**A booking is an observed intended schedule.** Its provenance is **observed**: the platform did not
declare it, it caught it from an external calendar. Its meaning is **intended**: a declaration of how the
owner is meant to be used. A booking therefore carries observed provenance of intended use, and it is the
**intended-schedule** side of usage. Shape: `booking { id, owner arc, source, external_id, subject,
organizer, start, end, status, attendee_count, ... }`, deduplicated and updated on `(source, external_id)`.

**Sourcing is a calendar driver on one platform-level integration, not a per-room mailbox fanout.** A
per-room-mailbox model fails operationally at scale (one subscription and one credential per room), and a
calendar is a **source**, not a device. Instead there is one platform-scoped integration component per
calendar tenant, holding the tenant application credential, exposing a `calendar` **interface whose
`interface_type` is a driver** (`graph-calendar`, `google-calendar`), consistent with the
interface-is-a-driver model. A server-side worker runs the driver, enumerates the tenant's bookable
resources, and fans out **internally**, upserting each booking with its owner bound through the arc. The
operator's only seam is mapping a resource to its owner (auto-matched by resource address, operator-confirmed).

**Bookings drive usage reconciliation and utilization SLIs.** The booking is the intended schedule;
telemetry (`call.started`, occupancy, an active input) is the observed actual. Reconciled per owner and
window: booked and observed is real utilization, booked and not observed is a no-show, observed and not
booked is ad-hoc use. These are **SLI slices** in the health layer (utilization percent, no-show rate,
ad-hoc rate, per location / system / component / window). An owner with no observability (a booked space
with no systems) degrades gracefully to a booked-only SLI.

**Deferred, each its own slice.** The resource-to-owner binding (auto-match versus operator-set);
**privacy**, since booking subjects and organizers are more sensitive than device telemetry, so store the
minimal busy/free plus booked-by by default and make the full subject opt-in; the platform order
(Microsoft 365 Graph first, Google Workspace second, Teams and Zoom Rooms scheduling as a later layer);
and recurrence (expand a series to instances versus store the series) with the sync window. Nothing here
is built. This ADR records the target so the booking slice ([#412](https://github.com/hyperscaleav/omniglass/issues/412)) inherits a decided model.

### ADR-0068: The API error model is the stock RFC 9457 shape

- **Date:** 2026-07-30 | **Status:** Accepted | **Pages:** [API](/architecture/api/)
- **Decision:** the API's error model is Huma's stock RFC 9457 `application/problem+json` shape: the
  `ErrorModel` (`title`, `status`, `detail`), carrying for validation an `errors` array of `ErrorDetail`
  entries, each `{location, message, value}`. The custom envelope sketched on the API page (a stable
  machine `code` plus a `violations` array of `{field, message}`) is retired.
- **Context:** the custom envelope was designed before any route existed. Today 141 routes serve the
  stock model, and the generated SPA client and the CLI already render it uniformly. A bespoke envelope
  would be cost without a driving consumer: no caller keys on an error `code`, and Huma's `ErrorDetail`
  already names the failing field through `location`.
- **Reversible:** additive. If a consumer ever needs a stable machine code, it can be added as an
  extension field on the stock model without breaking the shape.

### ADR-0069: Cycle safety is provenance-based, not topology-based

- **Date:** 2026-07-30 | **Status:** Accepted | **Pages:** [alarms and actions](/architecture/alarms-actions/), [health](/architecture/health/)
- **Decision:** the guarantee that automation cannot feed back into itself rests on **provenance**, not
  on a topological "alarms are terminal upstream and never write samples" rule. A consequence write
  carries `provenance='calculated'` with a `source_rule` naming its producer (today: the health
  rollup's `state` sample, `source_rule='health-rollup'`, written on alarm raise and clear), and rules
  must never route on their own consequences: the routing layer refuses to re-trigger a rule off a
  sample whose `source_rule` is that rule.
- **Context:** the alarms page argued cycle safety from the premise that alarms never write samples.
  The build falsified it: raising or clearing an alarm recomputes health in the same transaction
  (`internal/storage/alarms.go`), and the rollup records the verdict transition as a
  calculated-provenance `state` row (`internal/storage/health.go`). That write is correct, it is the
  recorded-transition model of
  [ADR-0050](#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain);
  the premise was too strong. The real invariant is lineage-based and survives future consequence
  writers (calculations, action side effects) that a topology rule would forbid.
- **Supersedes** the terminal-upstream cycle-safety argument on
  [alarms and actions](/architecture/alarms-actions/); the page now derives safety from provenance.

### ADR-0070: retire the standalone effective-secrets and effective-variables per-component panels; fields become the component value surface

- **Date:** 2026-07-16 | **Status:** Accepted | **Pages:** [config, secrets, and variables](/architecture/variables/), [identity and access](/architecture/identity-access/), [API](/architecture/api/)
- **Decision:** The standalone per-component **Effective secrets** and **Effective variables** panels are removed,
  along with their `GET /components/{name}/effective-secrets` and `GET /components/{name}/effective-variables` routes
  (and the generated `omniglass effective-secret list` / `effective-variable list` commands and the matching
  typed-client methods). A component's value surface is the **field** primitive: a component's values are its
  **fields**, each resolving override-versus-type-default and shown in the **Effective fields** panel. A secret or a
  variable reaches a component by being **sourced into a field** (the deferred field `sources` model) or **bound to a
  collection interface input**, not through a per-component cascade-browse panel. **Kept** unchanged: the storage
  cascade **resolvers** (`ResolveSecrets` / `ResolveVariables`) as the internal primitive the future `$sec:` /
  `$var:` interpolation consumer will call, and the **Secrets** and **Variables** directories (browse, create, edit,
  reveal) with all their routes and CLI.
- **Context:** The per-component effective-* panels predated the field primitive and listed **every**
  cascade-resolving cell that reached a component, which at any real depth is mostly inherited noise (a global SNMP
  community, a location poll interval) rather than anything set on that component. The
  [field](/architecture/variables/#property-one-typed-name-a-product-contract-a-stored-value) primitive
  ([#266](https://github.com/hyperscaleav/omniglass/issues/266)) is the schema-over-cells consumer the design always
  intended: a component carries a typed set of fields, each resolving to a set literal or its type default, and the
  intended `sources` model lets a field draw its value from a variable, a secret, a datapoint, or a file. Once fields
  are the value surface, a second per-component cascade browser over the raw cells is redundant and misleading (it
  reads as though the cells attach to the component when they only resolve onto it). Retiring the panels narrows the
  component detail to its fields and keeps the cells' own management on the Secrets and Variables directories, where
  the cascade is authored. The resolvers stay because the interpolation consumer (`$sec:` / `$var:`) still needs
  them; only the browse-panel surface retires.
- **Closes:** issue [#281](https://github.com/hyperscaleav/omniglass/issues/281) (retire the per-component
  effective-secrets / effective-variables panels), under the field epic
  [#266](https://github.com/hyperscaleav/omniglass/issues/266).

### ADR-0071: a template is a clonable example, not a versioned shape an instance pins

- **Date:** 2026-07-31 | **Status:** Accepted | **Pages:** [templates](/architecture/templates/), [core entities](/architecture/core-entities/), [cascade](/architecture/cascade/), [glossary](/architecture/glossary/), [collection](/architecture/collection/)
- **Decision:** A **template is an example configuration an operator clones**. Creating from one is a **one-time
  fork with no inheritance and no back-pointer**, so a template can be improved in any release without migrating
  anyone: **it is upgrade-safe precisely because nothing stays connected to it**. What an operator instantiates from
  a template is an ordinary row they then own, whether that is a `location_type`, a `standard`, or a whole system.
  The **versioned-shape model is retired**: `component_template`, `system_template`, their `*_version` rows, the
  `stable` / `beta` channels, the frozen BOM, and "an instance pins a version" or "tracks latest" are gone. A
  component's shape is its **`product`** (with the `product_property` contract), and a system's shape is its
  **`standard`**, to which a system **conforms with live inheritance**. Forking applies template to row; conformance
  applies row to instance. The word *template* survives; its meaning is inverted.
- **Context:** The decision log and the code had drifted into disagreement, and neither said so. ADR-0045 deferred
  "a product's own template or field-schema binding" and ADR-0049 stated "Templates and their frozen BOM stay
  `Design`; the two models are reconciled when template pinning is built", so the log asserted the pinned model was
  merely *deferred*. Meanwhile ADR-0047 retired `component_type`, ADR-0048 promoted `system_type` to `standard`, and
  the shipped schema pointed a component at `product_id` and a system at `standard_id`. A 2026-07-30 vocabulary
  audit found `templates.md` still teaching version pinning across 29 lines, with the retired nouns reaching search
  results through the page's own frontmatter description, and no denylist entry anywhere because the estate-model
  ADRs had each introduced a new noun without retiring the old one.
  The two models are not variants of one idea, they are opposites. The pin existed **so a template could not change
  under an instance**; the fork exists **so a template can change freely because no instance is watching**. Holding
  both was what made the docs unresolvable: every page had to hedge. This generalizes the fork-seed model ADR-0048
  already shipped for standards and location types (tracked for build in
  [#317](https://github.com/hyperscaleav/omniglass/issues/317)) and extends it to instantiating a **system** from a
  template, which is the operator-facing half. It also removes two rungs from the structural cascade: a template
  cannot be a resolution tier when nothing points at it.
  Rejected: keeping pinning for components while forking systems (two mental models for one word), and retiring the
  word *template* entirely (it is the right word for a clonable example, and the fork model is what an operator
  already expects from "start from a template").

### ADR-0072: an envelope is not named after its passengers, and an insert struct takes the Write suffix

- **Date:** 2026-07-31 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/), [glossary](/architecture/glossary/)
- **Decision:** Two naming rules, both general. **A carrier is named for what it carries, never for one of
  its passengers.** The telemetry wire message is a **`TelemetryBatch`** (`proto/og/v1/telemetry.proto`): it
  carries samples and raw log lines and will carry natively caught events, so naming it `Event` named it after
  one passenger. **And a storage insert struct takes the `Write` suffix**, paired with the bare read struct:
  `MetricSampleWrite` / `MetricSample`, `StateSampleWrite` / `StateSample`, `EventWrite` / `Event`,
  `LogLineWrite` / `LogLine`. A `Write` is the shape a caller hands the gateway; the bare noun is the row that
  comes back.
- **Context:** "Event" had come to mean four different things: the wire batch, the two sample insert structs
  (`MetricSampleEvent`, `StateSampleEvent`), the event insert struct (`EventOccurrence`), and the event read
  struct. Three of the four collided in a single signature, `deriveSamples(ev *ogv1.Event, ...)
  ([]MetricSampleEvent, []StateSampleEvent, []EventOccurrence)`, where only the last had anything to do with an
  event as the platform defines one (an `event_type`-registered occurrence carrying an `origin`, ADR-0066).
  The batch had already outgrown its name once when the log lane added raw log lines to it, and the push route
  ([#423](https://github.com/hyperscaleav/omniglass/issues/423)) would have made the collision visible on the
  wire as `Event.events[]`.
  The rename was **free**, which is why it happened before the payload grew rather than after: protobuf encodes
  field **numbers** and never message names, so the same payload marshals byte-identically before and after
  (verified by encoding one on each side and diffing the hex), and a node and a server of different vintages
  still interoperate. No migration, no dual-write, no deploy ordering.
  The `Write` half is not a new idea, only a newly stated one: the log lane set the pattern with `LogLineWrite`
  in [#414](https://github.com/hyperscaleav/omniglass/issues/414) and the older types simply predated it, so
  the codebase carried two spellings of the same concept. Recorded here because it is a rule for **every future
  insert struct**, and a convention that lives only in a commit message is one nobody applies.
  ADR-0037's title still reads "a protobuf Event", correctly: that is what was decided on 2026-07-07, the log
  is append-only, and its heading generates the anchor other entries cite. It carries a forward pointer to the
  rename instead.
### ADR-0073: A driver consumes transports; a transport is code, not a row

- **Date:** 2026-07-31 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [glossary](/architecture/glossary/)
- **Decision:** The collect layer is **driver-centric**, closing the question
  [ADR-0039](#adr-0039-an-interface-is-a-device-api-the-interface-type-is-its-transport-not-its-driver) left open.
  Three things follow. **A transport is code, not an operator-editable row.** `interface_type` is a low-level
  primitive (`icmp`, `tcp`, `ssh`, `http`, `snmp`, ...), one per package, discovered through a code registry the way
  collection primitives already are; the `interface_type` **table**, its FK from `interface.type`, and the
  hand-written dispatch switch in `internal/node/probe.go` all retire together in a later slice. An operator cannot
  author a transport, because a transport is a wire implementation, not configuration. **A driver consumes
  transports and is never one.** A driver declares which transports it can run over and the instance picks one; the
  driver owns the normalized catalog (what to fetch, how to parse), the transport owns only how bytes move.
  **The `interface_type` is a driver clause in
  [ADR-0067](#adr-0067-bookings-are-exclusive-arc-owned-schedules-reconciled-against-observed-usage) is retracted**:
  a calendar integration is an `https` **transport** plus a `graph-calendar` or `google-calendar` **driver**, the
  same shape as every other device, not an `interface_type` named after a vendor API.
- **Context:** ADR-0039 recorded the driver-centric split as *"the current-best direction, not a locked gate"* and
  named its own successor: *"driver-centric vs template-centric is re-examined, and this ADR revised or superseded,
  in a later ADR before the collect layer is built."* This is that ADR, and it confirms rather than reverses the
  direction, because the alternative has been tried in this tree and lost twice. Protocol handling in the template
  makes every operator a programmer, which the product cannot ask for; and a transport as a **row** produced exactly
  the drift it invites, an ADR sourcing calendars through an `interface_type` named for a vendor API, which reads as
  reasonable precisely because a table accepts any string. A code registry cannot be extended by an operator, so it
  cannot silently absorb the driver's job. The estate model above this line is built (a component points at a
  product, a product carries a capability set and a property contract, roles staff a standard), while below it
  `driver` holds a name and a version and nothing a node could act on, so nothing is being unbuilt here: the
  decision authorizes work that the scope gate has been blocking.
- **Deliberately not decided here**, each with a home so none of it rides on this entry: whether a product may bind
  **more than one** driver (`product.driver_id` is a single nullable uuid today, and role-addressed driver output is
  the recommended alternative to a join table, but it is a recommendation, not a ruling); whether a product is a
  **versioned artifact instances pin** or stays the live classifier it is today
  ([#491](https://github.com/hyperscaleav/omniglass/issues/491)); where **cadence** lives; and how a component
  resolves its effective driver set (the `EffectiveCapabilities` plus-minus pattern in `internal/storage/roles.go`
  is the obvious candidate to reuse rather than invent a second mechanism).
- **Supersedes:** the status note on
  [ADR-0039](#adr-0039-an-interface-is-a-device-api-the-interface-type-is-its-transport-not-its-driver) (the
  driver-centric split is no longer provisional) and, in part,
  [ADR-0067](#adr-0067-bookings-are-exclusive-arc-owned-schedules-reconciled-against-observed-usage) (its calendar
  sourcing clause only; the booking entity and its arc ownership stand).
- **Enables:** the [collect layer epic](https://github.com/hyperscaleav/omniglass/issues/489), whose slices were
  unauthorized without it. Nothing in this entry is built; it is a decision, and the code lands in that epic.

### ADR-0074: An approved definition rolls up to one PR; slices cascade on an integration branch

- **Date:** 2026-08-01 | **Status:** Accepted | **Pages:** [slice workflow](/contributing/slice-workflow/), [feature loops](/contributing/feature-loops/)
- **Decision:** For loop-executed work (a body of slices defined and approved as one Epic or Feature issue), the
  PR granularity moves from one-slice-per-PR to **one-PR-per-approved-definition**: sub-issue slices are built
  test-first on cascade branches (or serial commits) merged into one integration branch, each passing the
  per-slice gates before merging inward, and the definition ships as a single rollup PR whose ship-review covers
  the whole diff. The slice lifecycle inside each sub-issue is unchanged, and merge to `main` remains the
  architect's call. Hand-driven single slices keep the original one-slice-per-PR shape.
- **Context:** Long agent loops ship several related slices per body of work. Per-slice PRs would put the
  architect back in the approval loop once per slice (the touchpoint cost the loop exists to remove), while
  letting unreviewed slices land on `main` would gut the ship gate. The cascade keeps every per-slice gate and
  gives the architect one reviewable boundary: approve the prose definition at the front, merge one rollup PR at
  the end. The [feature loops](/contributing/feature-loops/) page is the contract; built for the
  [AI-driven feature loops epic](https://github.com/hyperscaleav/omniglass/issues/488).

### ADR-0075: An alarm's condition identity is a raiser-supplied dedup key

- **Date:** 2026-08-01 | **Status:** Accepted | **Pages:** [workers](/architecture/workers/), [alarms and actions](/architecture/alarms-actions/)
- **Decision:** `alarm` gains a **`dedup_key`** column (the condition identity; it defaults to the message when the
  raiser supplies none) and the partial unique index `alarm_open_condition_key` on `(component_id, dedup_key)
  WHERE cleared_at IS NULL`, so **one open alarm per condition per component** is a database fact. `RaiseAlarm` is
  a guarded conditional insert: a losing raise returns the existing open incident instead of a duplicate, writing
  no audit row and recomputing nothing. The rule engine's `event_rule_id` keying joins alongside the dedup key
  with its own slice; a per-component unique index was refused (it would forbid two unrelated conditions on one
  component, contradicting the capability-degradation model).
- **Context:** workers.md reasoned from a "one-open index" that did not exist: `RaiseAlarm` was an unguarded
  insert, `alarm_active_idx` is non-unique, and the liveness sweep was idempotent only because it happens to run
  as a singleton. The table had no column naming WHICH condition was open, so the documented `(event_rule, owner)`
  key was unrepresentable before the rule table exists; the raiser-supplied key works today and does not block on
  the engine. Shipped by [#465](https://github.com/hyperscaleav/omniglass/issues/465) in the #431 loop.

### ADR-0076: A renameable, human-typed identifier stays in the URL, and the write returns the uuid

- **Date:** 2026-08-04 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/), [api](/architecture/api/), [storage](/architecture/storage/), [tags](/architecture/tags/), [config, secrets, and variables](/architecture/variables/)
- **Decision:** An entity is addressed by its **`name`**, a renameable identifier an operator types, as well as
  by its immutable **`id`**. A rename is an explicit custom method (`POST /<collection>/{ref}:rename`, gated by
  `<resource>:rename`) rather than a field write, and a write returns the `id` so a client can store the
  stable handle and stop depending on the name it used. References inside the platform store the `id` only
  (ADR-0056), and `audit_log.resource_id` keys on it, so a rename moves exactly one column. Two
  response bodies do not yet carry the id, `NodeBody` and `SystemRoleBody`, so a client of those two
  has the name and nothing else to diff; closing that is a follow-up, not a change of direction.
- **Decision (what the name rule became):** there is **one validator**, `storage.ValidateName(table, name)`,
  which picks the rule from the table's declared identity shape instead of from the call site; the platform's
  two other name rules are deleted, not renamed. Two rules survive rather than four, and they share one
  character set, differing only in the dot and the ceiling: an **entity name** is one segment of lowercase
  letters, digits, and hyphens, at most 100 characters (`hq-boardroom-dsp`); a **keyspace name** is a dot-joined
  path of those same segments, at most 128 (`icmp.rtt-avg`). Neither may be uuid-shaped. Only `property_type`,
  `event_type`, and `command_type` are keyspace. `tag`, `variable`, and `secret` had been declared keyspace on a
  claim nothing exercised, and move to the entity rule, since none of them ever carries a dot: a behaviour
  change on three shipped surfaces, so `cost_center` and `$var:crestron.ssh` stop being creatable and the docs
  that taught them say `cost-center` and `$var:crestron-ssh`.
- **Context:** this is a deliberate departure from prevailing practice, recorded so it is not mistaken for an
  oversight. Prior art runs the other way almost without exception: AIP-180 states that a resource must not
  change its name, Kubernetes `metadata.name` is documented "Cannot be updated", GCP `projectId` and Tag
  `shortName` are Immutable, and Azure's own guidance is that most resource names cannot change after creation
  and that details belong in tags instead. More pointedly, systems in this domain **retreated** from it after
  shipping: Grafana deprecated its title-derived dashboard slug in v5.0 and added `uid` because name-based
  references broke dashboards, and PagerDuty froze `CustomField.name` at creation and routes all renaming
  pressure to `display_name`. None migrated the other way. The case for keeping it is operator ergonomics in an
  estate whose entities are named after rooms and racks that genuinely get renamed, and the mitigation is
  Linear's: the typed identifier is renameable, the uuid is returned on every write, and a client that kept
  using the name can detect the move by diffing the id it holds. The cost is real and accepted: an external
  reference held as a name breaks on rename, and nothing on the server can repair it. Alias and redirect
  machinery is deliberately not built yet; it is the first thing to reach for if that cost shows up.
- **Amended by [ADR-0089](#adr-0089-a-uuid-is-the-address-a-dotted-path-is-a-positional-lookup)** in
  justification, not shape: `:rename` stays exactly this custom method, gains a second job as the
  operator's pen-taking act ([ADR-0090](#adr-0090-a-derived-value-is-a-default-that-tracks-until-touched)),
  and the accepted cost above is repaired rather than removed. Once the uuid is the address, an
  external reference held as a name no longer breaks on rename in the way this entry describes: an
  integration holding the id survives every rename, and one holding a dotted path is a positional
  lookup honestly reporting whatever occupies that position now. What remains, and stands on its own,
  is the permission split this entry already named.

### ADR-0077: A group name obeys the entity name rule, tightening a pattern the code had excused

- **Date:** 2026-08-04 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** `principal_group.name` obeys the ordinary entity name rule (`^[a-z0-9][a-z0-9-]*$`, 100
  characters, a uuid refused), enforced by the Storage Gateway on create and rename and carried in the OpenAPI
  contract as a `pattern`. The previous rule (`^[a-z0-9][a-z0-9._-]*$`, 200 characters, checked only by the
  request schema) is retired.
- **Context:** this reverses a position that was written down in code. `KeyProvedElsewhere` excused
  `principal_group` from the behavioural validation sweep with the reason that tightening it "is a behaviour
  change for existing groups, tracked with the rename work". This is that work, so the excuse is spent. The
  looser pattern admitted `.` and `_`, the two characters the address grammar reads as separators, and the
  gateway itself validated nothing at all, so a group could be created with a uuid-shaped name through any
  caller that was not the HTTP route. There are no releases and no operator data, so the tightening costs
  nothing now and would cost a migration later. Shipped by [#567](https://github.com/hyperscaleav/omniglass/issues/567) in the [#545](https://github.com/hyperscaleav/omniglass/issues/545) loop.

### ADR-0078: A read-only field renders as a fact, not as a box that refuses typing

- **Date:** 2026-08-04 | **Status:** Accepted | **Pages:** [UI and the design system](/contributing/design-system/)
- **Decision:** a field that is not being edited renders as a **fact** (`KVStacked`: an eyebrow label
  above a value), never as a bordered control. `BladeField` owns the read-or-edit switch, the
  read-only treatment, the free-text shape, and the identity label pairing, so each is decided once
  instead of at every field. A blade the operator cannot edit therefore contains no element shaped
  like a control.
- **Context:** the blade shell was a primitive and the blade contents were not. Eleven pages defined
  a byte-identical local `Field`, four more went through positional `ctx.field(...)` / `ctx.fact(...)`
  helpers, and the read-only rendering (`input input-bordered flex items-center`) was hand-rolled 24
  times, so every blade defect was an N-place defect: `display_name` was labelled "Name" on 11 blades
  at once, and a description that would not wrap was one bug in 24 fields
  ([#573](https://github.com/hyperscaleav/omniglass/issues/573)). The seed-owned vendor blade made
  the treatment question concrete: five fields rendered as bordered boxes on a panel with no pencil,
  two of them holding a placeholder glyph, directly below three plain facts. The same read-only state
  had two appearances on one panel, and the boxed one signalled an editability that did not exist.
  A box that rejects typing reads as broken; a fact reads as a fact. The rule also fixes the read half
  of #573 by construction, because text in a fact wraps. Shipped by
  [#575](https://github.com/hyperscaleav/omniglass/issues/575) in the
  [#574](https://github.com/hyperscaleav/omniglass/issues/574) loop.

### ADR-0079: Five telemetry lanes, and property stops being the genus

- **Date:** 2026-08-05 | **Status:** Accepted | **Pages:** [samples](/architecture/properties/),
  [storage](/architecture/storage/), [core entities](/architecture/core-entities/),
  [collection](/architecture/collection/), [commands](/architecture/commands/),
  [events](/architecture/events/), [data model](/architecture/data-model/),
  [health](/architecture/health/), [glossary](/architecture/glossary/)
- **Decision:** the telemetry model is **five lanes** with five names and no overlap: a **metric**
  is a quantity (numeric, aggregates, carries unit and precision), a **property** is a value (what
  something is, including a number used as a name; values have duration, not averages), an
  **event** is a typed happening, a **command** is an instruction with a target, and a **log line**
  is raw arrival. "Property" stops being the genus of every reading and becomes one of the five,
  and the word "state" retires as a table name: a health state is a concept, not a table. Shipped
  as epic [#584](https://github.com/hyperscaleav/omniglass/issues/584) in seven slices:
  - **The catalog splits on `data_type`, the lane key** (#587): `int` / `float` rows become
    `metric_type` (taking the numeric facts: unit, precision), `string` / `bool` / `json` rows stay
    `property_type` (keeping the value domain: validation), and the per-key `kind` column retires,
    since catalog membership routes a sample and partitions every row cleanly where `kind` did not
    (four kind-less declared names were properties by ruling, and `data_type='string'` already put
    them there). Each classifier contract gains a metric sibling (`product_metric`,
    `standard_metric`, `location_type_metric`). The three ingest catalogs share one resolution
    namespace: a create is refused when a sibling holds the name.
  - **The value store folds into the series** (#591): a declared value is an ordinary series row
    (`provenance='declared'`, no lineage), an edit appends, and the **unset is a tombstone** (an
    appended declared row whose value is JSON null, resolved as absence by every reader). The
    **current value of every provenance is derived**: the latest series row per
    `(type, owner arc, instance, provenance)`, never a maintained cache. The separate `property`
    value store and its upsert-on-intake cache retire outright.
  - **The `state` table becomes `property`** (#588): the record takes the bare noun and the catalog
    keeps the `_type` suffix, matching `event_type`/`event`; the two value columns converge into
    one `value jsonb NOT NULL`. The word "state" was overloaded (a health verdict is a state, a
    reachability verdict is a state, and neither was that table); the lane is a value over time,
    and its name now says so.
  - **A log line belongs to a component, and node self-logs split into `node_log`** (#589):
    `log_line`'s four-armed arc narrows to component-only, and a node's self-logs move to an
    origin-true `node_log` table keyed to the node, the amended ruling recorded on the slice issue.
  - **A command records its status, and an intended value names its command** (#590): `command`
    gains a recorded `status` (`issued` until a terminal `settled` / `failed` / `timed-out`, with
    `settled_at` stamping the terminal moment) beside the still-computed settlement verdict, and
    `command_type` gains the **two-armed target** (`target_property_type_id` or
    `target_metric_type_id`, never both), because a metric is commandable. An intended sample's
    lineage moves from the command's caused event to the **command itself** (`command_id`): the
    value points at its cause, not at a derivation of it.
  - **The push wire goes per-lane** (#594): `TelemetryBatch` carries `metrics`, `properties`,
    `events`, and `logs` arrays, each entry validated against its own catalog at ingest; the
    polymorphic samples array retires.
  - **One name rule, no dots** (#586): a name is a single kebab token, at most 100 characters, on
    every table; the dot-joined keyspace rule and its 128-character ceiling retire, and the seeded
    dotted names backfill to their hyphenated forms (`icmp.rtt-avg` to `icmp-rtt-avg`).
- **What this reverses, and why.** ADR-0063 and ADR-0065 recorded the previous taxonomy as the
  settled model: **property** as the canonical signal over every sample, one `property_type`
  catalog with a `kind` column spanning numeric and categorical names, and a maintained `property`
  latest-value cache as the architecture-of-record for current values. This ADR reverses that
  recorded call in part. The root defect was the **property-as-genus overreach**: one word carried
  the signal, the store, and the whole sample family, so "property" meant three things on one page,
  the catalog carried facts (unit, precision) that only half its rows could use, and the cache
  duplicated a fact the series already held. The registry-over-bare-noun pattern of ADR-0063
  survives intact; what changes is that property becomes one lane among five rather than the genus
  of all of them. Recorded as the current-best model on the evidence above, and like every entry in
  this log it is revisable if the evidence changes.
- **Context:** verified against the live schema before building, which produced one mid-loop stop:
  the epic was first drafted against the pre-July init-dump names, where `property` was the catalog
  and `property_value` held current values, and three of its claims pointed at tables that no
  longer existed under those names. The definition was corrected against the running database
  before any slice built on it, and two rulings rode the correction: the partition key is
  `data_type` rather than `kind`, and a current value is derived from its series rather than
  stored. The retired vocabulary (`state` as a sample table, `property_value`,
  `StateSampleWrite` and its siblings) joins the docslint denylist with this entry.

### ADR-0080: Retention is provenance-aware: never declared, never the latest row per series

- **Date:** 2026-08-05 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/),
  [samples](/architecture/properties/)
- **Decision:** any retention pass over the sample tables obeys two invariants: it **never deletes
  a `declared` row** (an operator's assertion is the whole truth however old, not a sample that
  ages out), and it **never deletes the latest row of any series** `(type, owner arc, instance,
  provenance)` (a prune must not erase a current value). The rule ships as the **`PruneSamples`**
  Storage Gateway primitive, tested for both invariants, **before any retention feature exists**;
  no caller wires it yet, and any future retention feature calls the primitive rather than writing
  its own delete.
- **Context:** a blanket "delete older than N days" is the obvious first retention feature, and it
  would silently erase a declared value set two years ago, because for declared provenance the
  single row is the record itself, not one sample among many. With current values now derived from
  the series ([ADR-0079](#adr-0079-five-telemetry-lanes-and-property-stops-being-the-genus)), a
  naive prune would also delete the newest row of a quiet series and blank a current reading. Both
  traps are cheap to close before the feature and expensive to discover after a purge, so the floor
  shipped ahead of the feature (#591).

### ADR-0081: The control-plane wire is one subject grammar, node-anchored and batch-granular

- **Date:** 2026-08-06 | **Status:** Accepted | **Pages:** [messaging](/architecture/messaging/),
  [scaling](/architecture/scaling/)
- **Decision:** every control-plane subject is `og.v1.<verb>.<node>`, the node name the last token
  and **exactly one token**, so the server subscribes per-verb single-token wildcards and a node's
  credential is an explicit allow-list of its own subjects plus its private `_INBOX.<node>` reply
  namespace. The verb family is `worklist`, `heartbeat`, and `telemetry`, with `worklist-changed`
  reserved for the re-pull nudge and `og.v1.command.<node>` the committed future per-node command
  queue. The trusted push lane is **`og.v1.api.telemetry`, its own segment**; the rejected
  alternative was a reserved node name under `og.v1.telemetry.*`, where the single-token wildcard
  would hand a node named `api` the trusted subject, so its own segment makes the forgery
  structurally impossible rather than dependent on nobody choosing an awkward name. Addressing is
  **node-anchored and batch-granular**: a record's name is payload, never topic, and per-record
  subjects (the MQTT-style topic tree) are rejected. One recorded correction rides the entry: the
  one-token name rule (#586) was never justified by names as topic tokens; it stands on its own
  grounds. One consequence is named and deferred: the core-NATS verbs' server-side consumers
  (worklist, heartbeat) are singletons by construction, and their HA fork (queue groups versus
  worklist reassignment) is a scaling decision for the day a second server exists. Telemetry does
  not face that fork: its ingest is a named durable JetStream consumer a second server joins.
- **Context:** the topics conversation (recorded until now only in #584's issue body) weighed a
  user-facing MQTT-style topic tree against the internal contract and split the two concerns: the
  internal bus follows KISS (NATS subjects carrying batches between the node and the server), and
  any future user-facing subscription surface is its own design with its own grammar, reachable
  over an MQTT bridge if wanted. Wire constants live in `internal/collection/wire.go`; the grammar
  section on [messaging](/architecture/messaging/) is the page-of-record.

### ADR-0082: The type resource renames to location_type

- **Date:** 2026-08-06 | **Status:** Accepted | **Pages:**
  [identity and access](/architecture/identity-access/), [API](/architecture/api/)
- **Decision:** the permission resource `type` renames to `location_type` on every surface: the
  route stamps, the roles seed, the console gate strings, and the authz guard fixtures. The
  location-type property contract routes follow the same resource (`location_type:update` declares,
  `location_type:delete` withdraws). The generic word `type` retires from the permission
  vocabulary; no route stamps it and no role grants it.
- **Context:** one word hid two registries: the Types console page held location types and secret
  types behind one nav word while `type:*` gated only the location registry, and the contract
  routes gating `type:*` beside `product` and `standard` gating their own nouns was a live
  asymmetry. The same one-word-hides-two-things shape the catalog arc exists to end, renamed
  pre-release while the rename is cheap; the Types page split carried it (#598, the epic #601).

### ADR-0083: The Catalog rail is sectioned by the estate noun each registry serves

- **Date:** 2026-08-06 | **Status:** Superseded by [ADR-0084](#adr-0084-the-catalog-shell-and-five-signal-lanes) | **Pages:** [UI](/architecture/ui/)
- **Decision:** the Catalog nav cluster renders non-folding section headers under one naming rule:
  a **section is named for the estate noun it serves** and an **entry keeps the registry's own
  word**, collapsing to plain **Types** where the registry has no other word (Locations > Types,
  Secrets > Types). The sections, mirroring Inventory's order: Components (products, vendors,
  drivers, capabilities), Systems (standards), Locations, Secrets, Telemetry (metrics, properties,
  events, and the future log catalog as a soon entry), Action (rules, commands, and the future
  notifications), General (tags). The organizing line: **Telemetry is what gets recorded, Action
  is what the platform does**; Commands left Telemetry on the observed-versus-issued split, and
  Events stays in Telemetry because the shipped lane records happenings (caught from the estate,
  caused by the platform) and never sends them. Headers render from the permission-filtered entry
  list, so a fully gated section disappears with its entries; the palette tags sectioned entries
  `Catalog · <section>`; a visible Overview entry opens the `/catalog` hub, one card per visible
  section with live registry counts. Templates leaves the rail until the registry is real. Routes,
  tables, resources, and API surfaces are untouched: the rule governs presentation only.
- **Context:** fourteen flat entries hid five real clusters: products, vendors, drivers, and
  capabilities serve components with nothing saying so, and standards served systems invisibly.
  Rejected along the way: a flat rail with hub-only teaching (daily wayfinding regresses to the
  soup), hover flyouts (hiding is the disease being treated, hostile to touch and keyboard, and a
  "Types" flyout bucket recreates the one-word-hides-many shape ADR-0082 retired), and a uniform
  Types suffix (reverses the lane-noun labels the five-lane epic settled). The nav word for the
  rule registry is Rules; the vocabulary beneath it (table, API, resource words for rules and the
  alarm rows they raise) is unsettled and tracked in #606, out of this decision's scope.

### ADR-0084: The catalog shell, and five signal lanes

- **Date:** 2026-08-07 | **Status:** Accepted | **Pages:** [UI](/architecture/ui/),
  [glossary](/architecture/glossary/)
- **Decision:** Catalog is a single rail entry opening a **shell**: a grouped subrail whose
  entries navigate to the real per-registry pages, rendered in the pane at their canonical flat
  URLs, with an **Overview** landing of teaching cards; the subrail and the Overview derive from
  one group table, judged through the same permission filter the rail uses. The groups, ordered:
  **Telemetry** (metrics, properties, events), **Actions** (commands, with rules and
  notifications as tracked stubs), **Components** (vendors, products, drivers, capabilities, a
  templates stub), **Systems** (standards, a templates stub), **Locations** (types, a templates
  stub), **Metadata** (tags). The organizing axis is **direction, not genus**: Telemetry is what
  you receive, Actions what you send or run; a command targeting a property or metric is a form
  dependency, not a menu adjacency. Consequences ruled with it: **secret types loses its nav
  slot** (the URL stays reachable and gated; the table's retirement is the schema phase's call);
  **Logs stays out** of Telemetry until a log_type exists; the Systems entry reads **Standards**
  until the system_blueprint rename lands with the schema phase, because splitting operator
  vocabulary across surfaces to ship a label early is drift by construction; and the blade model
  holds on every field (read facts until the pencil, per the epic's approval caveat). The lane
  collective noun becomes **five signal lanes**, four inbound and one outbound: a command is an
  instruction you issue, not a telemetry reading, so "five telemetry lanes" retires as prose
  while ADR-0079's structure (a command is a genuine peer lane: type table, instance table,
  registry) stands unrevised.
- **Context:** the sectioned rail (ADR-0083) shipped its sections as rail geography and a hub
  that restated the rail; four design rounds against the live console replaced it: a
  single-surface browse table (identity-first rows, rejected for parallel-table drift against
  the real pages), a filter subrail (rejected because facets that filter one merged table
  cannot host each page's own search and create flows), and finally the shell, which keeps the
  IA as wayfinding and the pages as the single surfaces. Grouping by subject entity rather than
  artifact class survived every round: an operator arrives knowing the entity, not the taxonomy,
  and a component template and a location template share only a word. ADR-0083 is superseded;
  its estate-noun instinct survives in the group names. The genus-naming discussion that
  produced the direction axis also queued the schema phase (system_blueprint, location contract
  removal, secret_type retirement, the four-class taxonomy), deliberately sequenced ahead of the
  #379 migration collapse and deferred from this decision.

### ADR-0085: The `component_type` registry returns as the device-class genus

- **Date:** 2026-08-07 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [storage](/architecture/storage/), [Products guide](/guides/admin/products/)
- **Decision:** The **`component_type`** registry returns: a seeded-plus-custom device-class taxonomy
  (`display`, `projector`, `screen`, `presentation-switcher`, `video-bar`, `dsp`, `amplifier`, `mic`,
  `camera`, `codec`, `control-processor`, `touch-panel`, ...) that **nests by `parent_id`** (`mic` over
  `wireless-mic`, `ceiling-mic`, `boundary-mic`), on the same official-and-custom pattern as the other
  classification catalogs, operator-graftable at any node. It classifies the **product**
  (`product.component_type_id`, required), so a component inherits its type through the product it is;
  it is not a second classifier on the component. The row carries exactly the identity facts that
  genuinely span products, **inheriting down the tree with override at any node**: `name`, the naming
  **stem** (a subtype names components by its inherited stem unless it overrides), `display_name`,
  `icon` (the console glyph, replacing the too-coarse derivation from `product.kind`), `abbrev` (the
  two-to-three character hostname stem), and default tags. The seed discipline: a subtype exists only
  where a standard's slot would name it; a fact like panel technology stays on the product. It is
  **not** a shape-definer: contracts, declared properties, and drivers stay on the product. The
  division of labour: the type says what a component **is** (one, via its product); the role says what
  a system **needs** (a typed slot, a separate decision the roles epic owns); the capability axis is
  untouched by this decision.
- **Context:** ADR-0047 retired the component-level `component_type` when the fields folded into the
  product contract, and this partially reverses it, deliberately differently shaped: above the product
  rather than beside the component. What forced the return was naming and rendering. A generated
  component name needs a stem in the device-class vocabulary (`display-1`, the `fp1` hostname
  convention), and the platform had no table that speaks it: the role is positional
  (`display-front` says where it sits), the product is a SKU (`qm55`), `product.kind` is three values
  wide, and capabilities are many-valued. Every identity fact the console kept reaching for (icon,
  abbreviation, base tags, name stem) turned out to live at the same missing level, which is the tell
  that the level is real. The economics confirm it: a thousand components, a few dozen products, a
  couple dozen types; each identity fact is authored once at the level it spans.
- **Tracked under** epic [#614](https://github.com/hyperscaleav/omniglass/issues/614).

### ADR-0086: The product classification floor, and the kind split

- **Date:** 2026-08-07 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [storage](/architecture/storage/), [Products guide](/guides/admin/products/)
- **Decision:** Every **component** is required to name a **product**: `component.product_id` is
  `NOT NULL`. Three seeded generics (`generic-device`, `generic-app`, `generic-service`, each pointed
  at the matching generic `component_type`) cover anything not yet modeled as a real SKU, so the floor
  is total from the first migrated database onward: no component ever exists with no kind, no declared
  contract, and no driver path. **`product.kind`** narrows to `device | app | service`, drops its
  column default, and is required at create: an operator states a product's class explicitly rather
  than reading a silent fallback to `device` that let a mislabeled cloud service pass as correct
  forever. **`vm` retires**, folded into `app`, because nothing forks on a virtual machine that does
  not fork the same way on any other app: a virtual appliance is a different SKU, not a different kind.
  The three that remain are the who-owns-what split: `device` (the box is yours), `app` (the runtime is
  yours), `service` (only the account is yours).
- **Context:** ADR-0049's own Context named the optional product as the forcing function behind
  capability-gated staffing's resolved-capability layering: a role could not check a product's
  capabilities directly because a component might not have one. Making product required closes that
  gap at the root rather than compensating for it one layer up, and does the same for the naming
  epic ([ADR-0085](#adr-0085-the-component_type-registry-returns-as-the-device-class-genus)): a
  generated component name needs a `component_type`, which needs a product, so "product required" is
  what makes the naming generator total rather than a fallback-to-hand-authoring special case. The
  `vm` retirement follows the same audit that found the kind default silently absorbing a mistake: a
  fixed four-value enum with a default reads as validated when it is merely unset, and the fourth value
  had no code path that branched on it the other three did not already cover.
- **Tracked under** epic [#614](https://github.com/hyperscaleav/omniglass/issues/614).

### ADR-0087: Capability-gated staffing retires; an alarm impairs its component, not a named capability

- **Date:** 2026-08-07 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [health](/architecture/health/), [API](/architecture/api/)
- **Decision:** The **alarm -> alarm_capability -> degraded-capabilities -> role** chain
  [ADR-0049](#adr-0049-the-system-role-capability-gated-staffing-and-the-resolved-capability-set) and
  [ADR-0050](#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain)
  built retires. The replacement is three calls shorter: an **alarm impairs its component's own
  verdict wholesale**, no longer routed through a named capability; a role's occupant **satisfies**
  it whenever the occupant's own verdict is not `outage` (`Occupies()` is `Verdict != Outage`, so a
  merely degraded occupant still occupies its slot, since severity is how loudly to page somebody,
  not a second staffing threshold); and the **typed-slot guard** (`system_role_type`,
  `system_role_product`: a filling component's product must be classified within an accepted
  `component_type`, and, if pinned, be one of the named products) is now the **only** assignment-time
  check and plays no further part in health. `impact`, `quorum`, the worst-wins fold, and health as a
  recorded transition recomputed at the write are all unchanged; only the routing key changed, from a
  capability set to a component's own verdict.

  Two refusal rulings that fell out of the same rebuild, generalized here because nothing else names
  them: a role-write refusal is **409** when it depends on rows other than the one being written
  (double-staffing a component across roles, a capacity below the currently assigned count: "the
  declaration or assignment request is not invalid on its own, it conflicts with the estate's current
  state"), and **422** when the declaration alone is invalid regardless of other rows (capacity below
  quorum, an unresolvable typed-slot reference). And a boot-seed carve-out: `choice_alternate`
  (not `role_choice`, which keeps the ordinary insert-if-absent rule and no set-level reconcile;
  a choice row itself is never deleted by a reseed) reconciles to its declared YAML set on
  **every** boot, deleting a stored alternate that dropped out of the set (refusing instead, with
  `ChoiceInUseShortfall`, if a role still points at it) rather than leaving it in place. This is a
  **deliberate departure** from the
  platform's usual boot-seed rule (insert-if-absent, `ON CONFLICT DO UPDATE`, an operator's row never
  touched by a reseed): it is safe here only because `choice_alternate` has **no operator write
  path** (nothing but the seed ever writes one) and its **`position` is a packed 1..n sequence**
  within a choice, where a leftover orphan does not sit inert, it **collides** with the position a
  renamed or reordered entry now wants. It is a narrow exception for a position-ordered seeded child
  of a table nothing else writes, not a precedent: a seeded entity with an operator write path, or
  without a packed ordering an orphan can collide on, keeps the ordinary insert-if-absent rule.
- **Context:** Task 5 of the identity-model epic ([#626](https://github.com/hyperscaleav/omniglass/issues/626))
  shipped the retirement (`ca78bd3`, `dbfa284`) without filing this entry at the time; it is recorded
  now, against the gap, by Task 9 of the same epic. The forcing function was the typed-slot guard the
  prior slice had just landed (`c006c62`): once a component fills a role because its **product**
  classifies within an accepted **`component_type`**, gating the same assignment on a **second**,
  independent capability set was two guards asking the same question in different vocabularies, and
  the capability registry (`capability`, `alarm_capability`, `component_capability`,
  `product_capability`, `system_role_capability`, five tables) added a maintenance surface, an
  editing UI, and a resolved-set computation (`EffectiveCapabilities`) that nothing left standing
  needed. Health's own reasoning for routing through capability
  ([ADR-0050](#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain),
  "capability is the only vocabulary shared by the thing that breaks and the thing that cares") no
  longer holds once assignment routes through type instead: the component **itself**, not a named
  fact about it, is what a role now cares whether is up. The 409/422 line and the boot-seed carve-out
  are recorded here rather than left as implicit code comments because both are the kind of local call
  a later slice reads out of context and either contradicts by accident or copies somewhere it does
  not fit; the boot-seed carve-out in particular must not be read as license to delete a shipped row
  for any other seeded entity, which is why its two preconditions are stated explicitly.
- **Supersedes:** [ADR-0049](#adr-0049-the-system-role-capability-gated-staffing-and-the-resolved-capability-set)
  in full: `role_capability`, `component_capability`, and `EffectiveCapabilities` are gone, not
  merely superseded in wording, and the typed-slot guard is what a role now requires. **Amends**
  [ADR-0050](#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain):
  its chain item 1 (capability as the routing key) retires; items 2 through 5 (the pure judgement
  package, the transition-only record, recompute-at-the-write, and a report computing what it
  serves) are undisturbed and this entry changes nothing about them.
- **Tracked under** epic [#626](https://github.com/hyperscaleav/omniglass/issues/626).

### ADR-0088: A placement change is an authorization act, so a move is its own verb

- **Date:** 2026-08-08 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [API](/architecture/api/), [identity and access](/architecture/identity-access/)
- **Decision:** `parent` and `location` leave the `PATCH` body of `component`, `system`, and `location`
  entirely (both the API input structs and the storage `Patch` structs) and become a new `POST
  /<collection>/{ref}:move` custom method, carrying `{location?, parent?}` on component and system
  (at least one required, 422 otherwise) and `{parent}` only on location. `:move` is gated by a new,
  single-word permission token, `<resource>:move`, distinct from `<resource>:update` the same way
  `:rename` is distinct from it, seeded in `internal/seed/roles.yaml` beside `rename` in the same
  slice its route lands (Operator and Deploy get `component:move`; Deploy also gets `system:move` and
  `location:move`; Administrator's `system`/`location` grants gain `move`). `:move` writes a DISTINCT
  audit verb, `move`, not the generic `update` a PATCH wrote (a loosely worded "the move is
  auditable" acceptance criterion was already satisfied by the old row, whose JSON happened to
  contain `parent_id`, without forcing anything). `:move` never calls `RecomputeHealth`: a component's
  or a system's own reparent never has, and a component's relocate never has either (a component's
  verdict is purely its own active alarms, unaffected by placement). The one exception, stated so it
  is not mistaken for an oversight: a **system's relocate** (its `location` field, not `parent`)
  keeps recomputing health at both the location it left and the one it arrived at, exactly as
  `UpdateSystem`'s combined patch already did (`TestHealthMovesOnRelocation` is the load-bearing
  proof); the health rollup runs system -> location, and a system's own location is a direct input to
  that rollup the way a component's placement no longer is post-#626, so "a placement move never
  recomputes health" holds for every reparent and every component relocate but not for this one field.
  This is a correctness statement, not a coverage claim: a **second**, known-and-tracked gap sits
  beside it. `locationVerdict` also rolls up recursively through the location tree (a system's
  location resolves upward to every ancestor, and a location's own verdict folds every system in its
  subtree downward), so a location with placed descendants that moves to a new parent leaves both the
  old and new ancestors' recorded verdicts stale, exactly the shape a system relocate closes but
  nothing closes for a location move. This is not new here: `UpdateLocation`'s old reparent branch
  never recomputed health either, so `MoveLocation` carries the gap forward rather than introducing or
  closing it, per the same "`:move` does not add new recompute calls" ruling. Tracked as
  [#642](https://github.com/hyperscaleav/omniglass/issues/642), not fixed in this task.
- **Decision (the gap this closes):** `UpdateComponent`'s and `UpdateSystem`'s old reparent branches
  guarded a rejected reparent only on the non-empty case (`if patch.ParentName != nil &&
  *patch.ParentName != ""`); an explicit empty string skipped the guard entirely and set `parent_id`
  to `NULL` with no scope check at all, while `CreateComponent`/`CreateSystem` already refused a root
  placement (`create.All` required) to a caller without an all-scoped grant. A component- or
  system-scoped (not all-scoped) principal, who could already write anything inside its own subtree,
  could clear a row's parent and walk it out of every subtree scope it had ever been placed under,
  with no check the create path itself would have refused. `MoveComponent` and `MoveSystem` now
  require `action.All` on the same branch, closing it; `TestScopedPrincipalCannotLiftToRoot` and its
  system twin are written from scratch, since every existing lift-to-root test ran with the all scope
  and none would have caught the gap or would turn red from the fix.

  The argument is written on `parent_id`, not `location_id`, because it is the true, checkable one:
  component and system scope is **own-tier only** today (a component's scope tree is its own
  ancestor chain, unrelated to a location's), so a `location_id` clear cannot lift a row out of a
  scope that never covered it in the first place, and the cross-tier cascade that would make a
  location-scope argument meaningful is a tracked later slice ([#10](https://github.com/hyperscaleav/omniglass/issues/10)).
  The durable framing that also covers that latent hole once it lands: a placement change is an
  authorization act, whatever tier it crosses, not merely a field write that happens to touch two
  more columns than a rename touches one.
- **Decision (the deliberate asymmetry):** `MoveLocation` does **not** gain a clear-to-root capability.
  `UpdateLocation`'s reparent branch never had one either (an explicit empty `ParentName` resolved
  nothing and was already a 422, `ErrParentNotFound`, before this split), so there was no gap to
  close there, and adding clear-to-root now would be a new product capability nobody asked for, not a
  security fix riding along with this task. The asymmetry with component and system, which DO gain a
  guarded clear-to-root, is intentional and stated here so a future reader does not read it as a
  missed spot.
- **Decision (the transaction split):** `MoveComponent` and `MoveSystem` are separate gateway
  functions with their own transaction and their own audit row, not a shared statement with
  `UpdateComponent`/`UpdateSystem`. `UpdateComponent`'s old UPDATE carried a three-state `CASE`
  across four columns (`display_name`, `product_id`, `location_id`, `parent_id`) in one statement;
  splitting placement out splits that statement, so an operator gesture that used to change both a
  label and a placement in one PATCH now costs two requests and writes two audit rows if the caller
  wants both. This is the same tradeoff `RenameComponent`/`RenameSystem`/`RenameLocation` already
  established for name-plus-other-field edits (ADR-0076), chosen deliberately here for the same
  reason rename earned its own act: a placement change is an authorization act, not a label edit, so
  it deserves its own grant and its own audit trail entry rather than riding along with whatever else
  a PATCH happened to touch.
- **Amended by [ADR-0092](#adr-0092-a-location-move-recomputes-both-ancestor-chains):** the
  known-and-tracked second gap this entry records (a location move leaving both ancestor chains'
  recorded verdicts stale, [#642](https://github.com/hyperscaleav/omniglass/issues/642)) is closed.
  The ruling is unchanged in principle and gains a second member of the same exception class it
  already carved out for a system's relocate: `:move` recomputes health exactly where the rollup
  genuinely depends on the placement being changed, which is a system's `location` and a location's
  `parent`, and nowhere else.
- **Context:** Task 13 of the identity-model epic ([#627](https://github.com/hyperscaleav/omniglass/issues/627)).
  The two storage placement test files (`components_placement_test.go`, `systems_placement_test.go`)
  and the placement end-to-end tests moved to the new verb rather than being deleted; the compiler
  found every storage caller once the `Patch` struct fields were removed, the HTTP end-to-end callers
  (`map[string]any` bodies, resolved only at request time) did not, and were found by re-reading every
  `PATCH .../parent` and `PATCH .../location` call site by hand. `location:checkName` is unaffected:
  it is the advisory placement-availability precheck, unrelated to `:move`, and was not touched.
- **Tracked under** epic [#627](https://github.com/hyperscaleav/omniglass/issues/627).

### ADR-0089: A uuid is the address, a dotted path is a positional lookup

- **Date:** 2026-08-08 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [storage](/architecture/storage/), [identity and access](/architecture/identity-access/),
  [API](/architecture/api/), [glossary](/architecture/glossary/)
- **Decision:** An entity has two resolvable references, different guarantees, both legitimate and
  labelled as such. The **uuid is the address**: immutable, surviving rename and move, the only
  reference the platform itself ever generates or persists. The **dotted path is a positional
  lookup**: human-typed, resolving to whatever occupies that position now (`boi.17c.415a.$comp.display-1`
  after a panel swap resolves to the replacement, which is the point of a positional reference, not a
  defect of one). The containment rule: a reference the platform **generates** is always a uuid; a
  reference a human **types** may be a uuid, a bare name, or a dotted path. The console persists and
  addresses every read and write by `n().raw.id` (`web/src/pages/Components.tsx`, `Systems.tsx`,
  `Locations.tsx`), so a stale path can only ever exist where an operator typed one, into a CLI, a
  runbook, or a hand-built request. Because a platform-owned name recomputes at every `:move`
  ([ADR-0090](#adr-0090-a-derived-value-is-a-default-that-tracks-until-touched)), a path built
  entirely from platform-owned segments cannot itself go stale between the read that produced it and
  the write that consumes it; only an operator-owned segment can drift, and only because an operator
  chose to rename it.

  **This does not reopen [ADR-0079](#adr-0079-five-telemetry-lanes-and-property-stops-being-the-genus)'s
  one-name-rule collapse (#586).** That decision killed a NAME that could itself contain a dot
  (`icmp.rtt-avg`, a keyspace catalog key); today every name, keyspace or entity, is exactly one kebab
  segment, no dots, at most 100 characters, on the one rule `storage.ValidateName` applies everywhere
  (`internal/storage/name_rule.go`). What this decision adds is a **reference**, syntactically distinct
  from a name, that concatenates several individually-valid single-segment names with `.` and a
  `$accessor` (`$comp`, `$sys`, `$role`) into one positional path. `icmp.rtt-avg` was the last name a
  table's `name` column held with a dot in it, backfilled dot-free by #586 itself
  (`icmp.rtt-avg` to `icmp-rtt-avg`); no name column has held one since, and this epic adds none: a
  dotted value is never stored, only resolved.

  **The allowlist is the load-bearing fact.** The entity name rule, `^[a-z0-9][a-z0-9-]*$`
  (`internal/storage/name.go`), is an allowlist, not a denylist of characters someone remembered to
  exclude. A name can never contain a separator or wildcard from any protocol, chosen or not yet
  chosen: `.` (path segments), `$` (accessors), `*` and `>` (NATS), `/` and `#` (MQTT), `%` (URL
  escaping), or `:` (reserved by the router for a custom method's verb suffix, `POST
  /components/{ref}:rename`: a name admitting `:` would make `rm215a:rename` ambiguous between the
  entity `rm215a:rename` and the entity `rm215a` with the `:rename` verb, so the allowlist excludes it
  for the same reason it excludes everything else on this list). That single property is what lets one
  grammar render as a CLI argument, a REST path segment, or a NATS subject with no escaping anywhere,
  and lets most segments render as a DNS label or an email localpart, since the character set alone is
  a subset of both: what the allowlist does **not** guarantee is a segment's fit under DNS's own
  63-octet label ceiling (the 100-character entity limit is wider) or DNS's ban on a trailing hyphen
  (`^[a-z0-9][a-z0-9-]*$` legally admits `abc-`); both are a render-time concern for whichever segment
  eventually feeds a hostname, not a naming rule this decision enforces. An allowlist still composes
  across every namespace it has not met yet, where a denylist has to be re-audited for each new one.

  **A percent-encoded slash arrives already decoded.** The HTTP handler decodes a path parameter
  before the address parser ever sees it (verified against the router, not assumed, before Task 12
  built on it), so a caller cannot smuggle an extra path level past `ParseAddress` by encoding a `/` or
  a `.`. Every segment, in the location root, the plane tail, and the role name alike, passes the
  entity name rule inside `ParseAddress` itself (`internal/storage/path.go:74-77`) before any of it
  reaches a query: validation is structural, not a property of what happens to already be in the
  database.

  **A dash render and a bare render are display-only, and neither is a form the resolver ever treats
  as a path.** `RenderDash` (`boi-17c-216b-display-1`) strips the accessor; `RenderBare`
  (`boi17c216bfp1`) further compacts the final segment to the component type's `abbrev` plus its
  ordinal and drops every separator, hyphens included (`internal/storage/render.go`). Both exist for
  labelling only (a cable tag, an asset sticker, a compact row sub-line): accessor-stripping is lossy
  for the dash form and stem-compaction is lossier still for the bare form, so neither round-trips
  through `ParseAddress`/`resolvePath`. Both also still satisfy the entity name rule on their own
  (letters, digits, and hyphens, no dot or `$`), so a dash or bare string handed back to the resolver
  is not refused as malformed: `ParseAddress` reports it is not an address at all and it falls through
  to the ordinary bare-name path, almost always matching no row, an unremarkable 404 rather than a
  distinguishable error.

- **Decision (six edges, recorded rather than hidden):** the read path (`scopedByNameInScope`,
  `refPolicyHide`) and the write path (`resolveScopedRef`, `refPolicyForbid`) share one primitive,
  `resolveRef` (`internal/storage/scopedcrud.go:521-546`), but they do not share every guarantee, and
  the gaps below are shipped as stated limits, not silently left for the next reader to discover.

  1. **`resolveRef`'s write-path policy proves "writable here," not "readable here."**
     `resolveScopedRef` (`scopedcrud.go:633-649`) narrows candidates by the caller's create- or
     action-scope, not its read scope. A resolved reference is one the caller may place a binding
     against, which is a narrower claim than the scope ruling's own wording ("scope decides before
     ambiguity does") suggests on its own. **Amended (#700):** that claim was too weak for a
     CROSS-TIER placement reference, and the amendment narrows which references the edge covers
     rather than changing the policy itself. A create's or a move's location and system references
     (`CreateComponent`, `CreateSystem`, `MoveComponent`, `MoveSystem`) resolved existence-only,
     because the caller's create scope is resolved for the entity being written and can never match
     the referenced tier's own ancestor chain. Once
     [ADR-0100](#adr-0100-a-label-cascades-where-the-blast-radius-is-a-placement-and-waits-for-the-verb-where-it-is-the-estate)
     put placement into the label data map, the label those writes stamp and hand back is rendered
     from the referenced row's label, so "writable here" stopped being the right question and
     "readable here" became it. They now go through `resolvePlacementRef`, which applies the READ
     path's policy (`scopedByNameInScope`, `refPolicyHide`) using the caller's scope on the
     REFERENCED tier, matching what `:renderLabel` already did for the identical references
     ([ADR-0104](#adr-0104-a-create-form-shows-the-name-it-can-know-and-never-mints-one-to-preview-it))
     so a preview and the create it previews cannot disagree. `resolveScopedRef` keeps every
     same-tier parent and owner reference, where this edge still reads as written.
  2. **A bare-name `forbidden` is a name-existence oracle.** A name matching at least one row, none of
     them in the caller's action scope, is `cfg.forbidden` (403), not the read path's non-disclosing
     404 (`scopedcrud.go:469-475`). A caller can learn a name exists somewhere in the estate from the
     status code alone. This predates the epic and is tested (`interfaces_scope_test.go:82`), and it
     is now asymmetric with the read path this branch tightened (`scopedByNameInScope` folds the same
     two cases into one 404), by design: several routes' contracts already depend on the 403/404 split
     a caller supplied its own reference into, which is not the new disclosure a read's uuid would be.
  3. **An ambiguous `?system=` on a component's tags read is a new, narrower oracle, preferred over
     what it replaced.** `ResolveTags` (`internal/storage/tags.go:449-459`) now 409s a `component:read`
     caller whose `forSystem` filter matches more than one system, redacting candidates
     (`withoutCandidates`). A caller with no `system:read` grant learns two systems share that name.
     This is a deliberate improvement: the alternative silently seeded the tag cascade from whichever
     system a bare-name lookup happened to resolve to, returning a wrong answer with no signal at all.
  4. **The `$role` accessor parses but resolves nowhere.** `AddressRole`
     (`internal/storage/path.go:35-42`) is syntactically real: `boi.17c.$sys.av.$role.primary-dsp`
     parses to a well-formed `Address`. `addressKindTable` (`scopedcrud.go:247-263`) has no table for
     it, so `resolvePath` reports the same `ErrPathNotFound` it would for any other plane mismatch, a
     non-disclosing 404 that does not distinguish "role addressing is not built" from "this role does
     not exist." Reserving the grammar ahead of its resolution is a stated decision here, not a gap
     found later: the day a system-role read exists, it inherits a grammar it never has to renegotiate.
  5. **The boot-seed reconcile-and-delete carve-out is a separate pattern, cross-referenced, not
     restated.** `choice_alternate`'s every-boot reconciliation
     ([ADR-0087](#adr-0087-capability-gated-staffing-retires-an-alarm-impairs-its-component-not-a-named-capability),
     [storage](/architecture/storage/#migrations-three-buckets-kept-separate)) is about which seeded
     rows a reboot may remove, not about how a reference resolves or who holds the pen on a value; it
     shares no mechanism with this decision or with [ADR-0090](#adr-0090-a-derived-value-is-a-default-that-tracks-until-touched)
     and is named here only so a reader does not conflate the two governance patterns.
  6. **The tier guard inside `resolveRef` is forward insurance, not proof.** `resolveRef` panics if the
     caller's scope was resolved for a resource that does not `Cover` the config being checked
     (`scopedcrud.go:521-524`). Every one of the 29 non-test call sites of `scopedByNameInScope` and
     `resolveScopedRef` today passes a `resource` label already derived from the same config it is
     checked against, so the guard **cannot fire on any
     input the current code produces**, and a green suite running with it live proves nothing beyond
     that (`scopedcrud.go:507-517`, in the code, says so explicitly). It is insurance for the next call
     site that copies a pattern without updating the label. It also inherits a blind spot from
     `scope.Covers`: for `secret`, `variable`, `field`, and `telemetry`, every tier is admissible, so
     the guard tells a right family from a wrong one but not a right tier from a wrong one within that
     family, exactly the shape a real cross-tier regression took in this epic's own review before a
     scoped test, not this guard, caught it.

- **Extends [ADR-0062](#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle):** that
  decision's dual-accept clause already said a reference "resolves whichever form it is given"; this
  decision makes the third form literal rather than aspirational. `loadByRef`
  (`internal/storage/scopedcrud.go:158-202`) tries a uuid, then a parsed dotted address, then a bare
  name, in that order, for every scoped tree entity.
- **Amends [ADR-0076](#adr-0076-a-renameable-human-typed-identifier-stays-in-the-url-and-the-write-returns-the-uuid)**
  in justification, not shape: `:rename` stays exactly the gated custom method ADR-0076 built, and it
  earns a second job under [ADR-0090](#adr-0090-a-derived-value-is-a-default-that-tracks-until-touched):
  it is now precisely the boundary where an operator takes the pen from the platform's name-tracking.
  What changes is why the ceremony still earns its keep. ADR-0076 accepted "an external reference held
  as a name breaks on rename, and nothing on the server can repair it" as the renameable identifier's
  cost. Once the uuid is the address that argument is repaired: an integration holding the id survives
  every rename, and one holding a path is a positional lookup honestly reporting whatever occupies that
  position now, not a broken link. What remains, and is sufficient on its own, is the permission split
  (an operator trusted to edit `display_name` is not thereby trusted to rewrite the identifier
  colleagues type) and the pen-taking act itself.
- **Context:** every estate name was globally unique before this epic, so operators hand-encoded the
  building into every name and the encoding went stale on every move, twice per room (location and
  system). ADR-0076 already made the id the durable reference and the name the renameable display key;
  this decision (identity-model epic [#627](https://github.com/hyperscaleav/omniglass/issues/627),
  Tasks 10 through 12 and 15) makes the id literally an address a caller composes, types, and resolves,
  and gives a human a second, positional way to reach the same row without inventing a third identity
  field. The path grammar was reserved, syntax-only, in `internal/storage/name.go` since ADR-0076;
  Task 10 made every internal owner-resolve id-based ahead of the placement-scoped uniqueness DDL
  (Task 11, `db/migrations/20260808090000_names_scope_to_placement.sql`), Task 12 built the parser and resolver
  (`internal/storage/path.go`, `resolvePath`), and Task 15 put the resolved path, its segments, and
  both renders on the wire and pointed the console at uuids exclusively. Task 13's `:move` verb
  ([ADR-0088](#adr-0088-a-placement-change-is-an-authorization-act-so-a-move-is-its-own-verb)) and
  Task 14's name generation ([ADR-0090](#adr-0090-a-derived-value-is-a-default-that-tracks-until-touched))
  are what make a platform-owned segment of a path trustworthy rather than merely typeable.
- **Tracked under** epic [#627](https://github.com/hyperscaleav/omniglass/issues/627).

### ADR-0090: A derived value is a default that tracks until touched

- **Date:** 2026-08-08 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [storage](/architecture/storage/)
- **Decision:** when the platform can compute a value an operator would otherwise type, it follows one
  rule with four binding clauses, and none of the four make the value a constraint: a derived value can
  prefill a field but it can never cause a write elsewhere to be refused.

  1. **The platform fills.** A component's `name` is minted at create when an operator leaves it
     blank: `<component_type stem>-<n>`, the ordinal the smallest positive integer no sibling matching
     that stem already claims in the same placement scope, a sibling on a different stem never blocking
     it (`generateComponentName`, `internal/storage/namegen.go:129-168`).
  2. **A platform-owned value tracks its facts.** While `component.name_generated = true`, the name
     recomputes inside the same transaction as whatever changed its inputs: a `:move` to a new
     placement (`internal/storage/components.go:632-655`) or an ordinary product `PATCH` that
     reclassifies the component to a new type (`components.go:470-496`). Both ride the causing write's
     own audit event, old and new name in its payload; a platform-driven recompute is never itself a
     `:rename`.
  3. **The operator owns it on first touch.** `:rename` (`components.go:706-727`) clears
     `name_generated` to `false` unconditionally, whether or not the row was already operator-owned,
     and from that write the platform never recomputes the name again no matter how the facts move
     afterward.
  4. **The operator can hand it back.** `:resetName` (`components.go:744-774`, gated by the same
     `component:rename` token `:rename` uses, since both change the identifier) regenerates the name
     from the component's **current** type and placement and sets `name_generated = true` again,
     whether or not it already was.

  The contrapositive is the other half of the rule: a value the platform must own unconditionally (a
  health verdict, a resolved effective-tags set) is computed on every read, never stored as a default
  an operator could edit into a lie. The boundary between the two is whether the operator is allowed to
  disagree with the platform's answer.

- **Decision (the migration default deviates from the epic's own wording):**
  `component.name_generated boolean not null default false`
  (`db/migrations/20260808090000_names_scope_to_placement.sql:39-46`) ships `DEFAULT false`, not the
  `DEFAULT true` the epic issue specified. This is deliberate: every component row that exists before
  this column lands was operator-typed under the pre-#627 model, where no generator existed to have
  picked a name for it. `DEFAULT true` would hand the platform a pen it never earned over real,
  pre-existing operator data and let the very first `:move` against one of those rows silently rename
  it. The gateway writes the flag explicitly on every insert from Task 14 onward, `true` on a generated
  create and `false` on an operator-typed one; the column default describes only a row this migration
  found already sitting in the table, never a row created after it.

- **Context:** the same shape resolved independently twice on this branch before it was named as
  one principle, which is what promotes it from a per-feature habit to a rule to check new work
  against. A component's name (Task 14) is the built case above. `createIdentity`
  (`web/src/lib/entities.ts`) already ran clauses 2 and 3 client-side for a catalog entity, deriving a
  slug from a display name live and freezing it on first edit, before this decision gave the
  server-side pattern a name. The product classification floor
  ([ADR-0086](#adr-0086-the-product-classification-floor-and-the-kind-split)) is the contrapositive's
  own evidence: a silent schema default on `product.kind` let a mislabeled cloud service read as
  correct forever, exactly the failure clause 1's explicit fill and clause 3's no-silent-default guard
  against, which is why `kind` is required at create rather than defaulted. A system's own
  `location_id` (`internal/storage/systems.go`) is a live counterexample to clause 2, worth naming
  because a reader could otherwise assume every platform-computable field tracks live: it is authored
  at create and changed only through `:move` ([ADR-0088](#adr-0088-a-placement-change-is-an-authorization-act-so-a-move-is-its-own-verb)),
  never derived from `system_member` rows, so clause 2's live tracking is confirmed here as a per-field
  policy choice this decision's own build makes differently for a system's placement and a component's
  name, not a mandate every derived field must take. The boot-seed reconcile-and-delete carve-out
  (`choice_alternate`,
  [ADR-0087](#adr-0087-capability-gated-staffing-retires-an-alarm-impairs-its-component-not-a-named-capability),
  [storage](/architecture/storage/#migrations-three-buckets-kept-separate)) is a related but distinct
  pattern, about which seeded rows a reboot may remove rather than who holds the pen on an
  operator-facing value; it is cross-referenced here, not restated.
- **Tracked under** epic [#627](https://github.com/hyperscaleav/omniglass/issues/627).

### ADR-0091: An update_mask says which fields a PATCH writes

- **Date:** 2026-08-09 | **Status:** Accepted | **Pages:** [api](/architecture/api/)
- **Decision:** a `PATCH` body may carry an optional `update_mask`, with AIP-134's semantics exactly:
  an **absent** mask is the implied mask of the fields the body populated (a non-empty value), which
  is what every `PATCH` in the tree already did; a **present** mask writes exactly the fields it
  names, populated or not, so a named field carrying its zero value **clears**; **`["*"]`** is full
  replacement, the equivalent of a `PUT`, and cannot be combined with named fields; and a mask naming
  a field the resource does not patch is a **422 naming the field**, never a silent no-op. Top-level
  field names only, spelled as the wire body spells them. It rides in the **request body**, not the
  query string. It is built as a primitive (`internal/updatemask`, a pure `Resolve` over three lists)
  and consumed by one caller, the role declarations, which are converted from `PUT` to `PATCH` in the
  same change.
- **Context:** the API could set a field and could narrow one, but for anything that is not a string
  it could never unset one. `system_role.capacity` is an integer with no empty-string sentinel
  available, so once an operator set a cap, raw SQL was the only way back to unbounded
  ([#638](https://github.com/hyperscaleav/omniglass/issues/638)). That is not a `capacity` quirk, it
  is a property of every non-string optional field the API will ever have, and AIP-134 has no answer
  for it through the implied mask, whose whole definition is "fields that are populated". The
  mechanism it gives is the explicit mask.

  **Why the body, not the query.** Google puts the mask in the query because gRPC transcoding binds
  the request body to the resource itself, leaving nowhere else for it. There is no gRPC here, Huma
  models a body as a typed struct, and a body field generates cleanly into the OpenAPI document, the
  typed SPA client, and a CLI flag with no hand editing. Recorded because it is expensive to reverse
  once clients exist.

  **The three-state string sentinel stays.** An omitted field unchanged, an explicit `""` clears, a
  value sets (`emptyPtrToNil`, `internal/api/products.go`) is already the house convention for an
  optional string reference, taught by the CLI reference on `:move` and the system patch. The mask
  generalizes clearing to everything that is not a string; it does not retire the sentinel, which is a
  larger ripple across the docs and the CLI reference and should not ride along with the mechanism
  that would eventually replace it. Where the two meet, the sentinel wins the definition of
  "populated": a pointer to `""` counts as populated (the caller said something, and what they said
  was "clear it"), which is what keeps the role alternate's explicit detach path working.

  **No retrofit.** The other 108 `PATCH` registrations accept no mask and are not changed. They stay
  correct for free: an absent mask IS the implied mask, so their behavior is byte-identical, proven by
  the suite staying green with no expectation edited. Retrofitting is a per-route decision about which
  fields are patchable, not a mechanical sweep, and doing it under one slice would have made the
  primitive impossible to review.

  **The roles conversion is the proof, and it changes wire behavior.** The role declarations were a
  `PUT` carrying two semantics at once: `capacity` and `alternate` preserved on omit while
  `display_name`, `quorum`, `impact`, `position_labels`, `accepted_types` and `pinned_products`
  replaced wholesale, so a write carrying only `display_name` silently reset the role's impact to
  `degraded` and dropped its labels and its typed slot
  ([#639](https://github.com/hyperscaleav/omniglass/issues/639)). Under the implied mask the whole
  body reads one way. Two consequences are deliberate rather than incidental: an **empty list is not a
  populated field**, so `[]` now means "unchanged" where it used to clear, and clearing a list means
  naming it in the mask (the console's role editor therefore names every field it owns, and nothing it
  does not); and the declaration routes become `PATCH`, which AIP-134 requires and which matters
  because `PUT` "becomes a backwards-incompatible change to add fields to the resource", and fields are
  about to be added. The `PATCH` still CREATES the role when it is absent, which AIP-134 would gate
  behind `allow_missing`: there is no other create path for a declaration (the role is addressed by
  name within its owner, so declaring and revising are the same idempotent write), and a flag whose
  only legal value is `true` teaches nothing. The binding-style `PUT` routes (`members`, the
  `*_properties` and `*_metrics` association routes) are untouched: they set an association wholesale
  rather than updating a resource.
- **Tracked under** [#666](https://github.com/hyperscaleav/omniglass/issues/666), with
  [#638](https://github.com/hyperscaleav/omniglass/issues/638),
  [#639](https://github.com/hyperscaleav/omniglass/issues/639) and
  [#640](https://github.com/hyperscaleav/omniglass/issues/640).


### ADR-0092: A location move recomputes both ancestor chains

- **Date:** 2026-08-09 | **Status:** Accepted | **Pages:** [health](/architecture/health/),
  [API](/architecture/api/)
- **Decision:** `MoveLocation` recomputes health when it changes a location's `parent`, inside the
  transaction the move already opens, over BOTH ancestor chains: the one the location joined and the
  one it left. This is the second and last member of the exception class
  [ADR-0088](#adr-0088-a-placement-change-is-an-authorization-act-so-a-move-is-its-own-verb) carved
  out for a system's relocate, not a reversal of its "a placement move never recomputes health"
  ruling: `:move` recomputes exactly where the rollup genuinely depends on the placement being
  changed, which is a system's `location` and a location's `parent`, and nowhere else (a component's
  or a system's `parent` stays health-inert, and so does a component's `location`, since a
  component's own verdict is purely its active alarms per
  [ADR-0087](#adr-0087-capability-gated-staffing-retires-an-alarm-impairs-its-component-not-a-named-capability)).
  The trigger names ONE row per side, the moved location and the parent it left, not a walked chain:
  `locationsOver` already takes its named locations as the seed of a recursive ancestry CTE, so each
  named row carries every ancestor above it, and walking either chain in Go first would reimplement
  in a second place the walk the query performs. Resolving the old chain after the write is equally
  safe, because the only parent edge the write touches is the moved row's own, so the old parent's
  ancestry reads the same before and after. A `:move` that changes no parent (the documented no-op)
  recomputes nothing, the same guard the system relocate applies.
- **Context:** [#642](https://github.com/hyperscaleav/omniglass/issues/642), filed by ADR-0088's own
  review round and closed here. The gap predates the `:move` split: `UpdateLocation`'s old reparent
  branch never recomputed either, so nothing has ever recorded this edge. It matters because a
  location's verdict is the one rollup that genuinely depends on where the row sits: `locationVerdict`
  folds every system in the location's own subtree, walked downward by a recursive CTE, so a location
  with placed descendants moving to a new parent really does change what its old and new ancestors
  should read. Left open, an operator reorganizing a campus would see the abandoned branch frozen at
  the verdict of a room that is no longer in it, and the new branch reading healthy over a room that
  is. The reads never lied (both health reads compute the verdict they serve), so the cost was
  confined to the recorded history, which is exactly the "a missing trigger is a hole in the history"
  cost the health page enumerates its trigger list to avoid.

### ADR-0093: The tag cascade follows the component it resolves for

- **Date:** 2026-08-09 | **Status:** Accepted | **Pages:** [tags](/architecture/tags/),
  [identity and access](/architecture/identity-access/)
- **Decision:** Effective tags are authorized by the **component the cascade resolves for**, and by
  nothing else. A caller who can read that component sees every value that cascades onto it,
  including values owned by a system or a location the caller could not have listed directly. The
  `?system=` parameter is a **filter over that answer**, choosing which membership seeds the system
  band, and never a widening of it: `seed_sys` already requires the named system to be one the
  component belongs to. Per-band scope checks are deliberately NOT applied.
- **Context:** [#641](https://github.com/hyperscaleav/omniglass/issues/641) proposed resolving
  `?system=` through the caller's read scope. Three findings closed it as the posture we already
  hold rather than a defect. First, the obvious implementation is the exact change
  [#627](https://github.com/hyperscaleav/omniglass/issues/627) made and reverted as a critical:
  `ResolveTags` resolves its scope for **component** (the route is gated on `component:read`), and
  `inScopeTree` walks the target table's own chain, so a component-tier id can never appear in a
  system's ancestor chain and every non-all caller's band silently vanished, a wrong answer with no
  error rather than a refusal. `TestResolveTagsSystemBandSurvivesScopedCaller` stands as that
  regression and asserts the band IS present for a scoped caller. Second, the check would not close
  the disclosure it named: the same values reach the same caller through the primary-membership
  default seed, through `sys_chain`'s ancestor walk above the seeded system, and through the
  deliberately scopeless batch resolver behind the directory's Tags column. Third, and decisively,
  the cascade is a property OF the component: a value that resolves onto a component the caller may
  read is part of that component's configuration, and withholding it would report a resolved
  configuration that no longer resolves to what the platform actually applies. The alternative, a
  per-band scope-filtered cascade, is a coherent design and a much larger one (per-tier scopes
  threaded from the API, a rule for the default seed and the ancestor walk, redaction of shadowed
  candidates, a batch contract, and that regression rewritten). It is not ruled out forever; it is
  ruled out as a patch, and it would need a threat model that wants it.
### ADR-0094: Benchmarks are the second performance instrument, and they gate nothing

- **Date:** 2026-08-09 | **Status:** Accepted | **Pages:** [test-driven](/contributing/test-driven/)
- **Decision:** Performance has two instruments, and only one of them gates. **Round-trip counting**
  (`internal/storage/storagetest/querycount`, asserted in `list_cost_test.go`) stays the deterministic
  net that runs in `make test` and blocks a merge. **Wall-clock benchmarks** (`make bench`, ten
  benchmarks over the real Gateway at two estate sizes) are diagnostic only: run deliberately before
  and after a change that claims a performance effect, and compared with `benchstat`. Counting catches
  the N+1 exactly; benchmarks catch what counting is blind to, which is everything inside a single
  statement (a dropped index, a plan flip to a sequential scan, a recursive CTE that stops being
  bounded, a predicate that stops being sargable). Every benchmark runs at a small estate and a larger
  one, because one size cannot tell a constant apart from a linear cost, and every fixture is built
  outside the timed section, because a benchmark that provisions inside its own loop measures the
  harness.
- **Not chosen, and why each was refused rather than deferred by accident:**
  - **No CI job and no merge gate on a duration.** A wall-clock threshold on a shared runner either
    sits so loose it catches nothing or it flakes and gets muted. A perf job everybody ignores is
    worse than none, because it reads as coverage. A Go benchmark is inert without `-bench`, so this
    is the natural shape rather than a compromise.
  - **No stored baseline artifact.** `benchstat` comparison needs a baseline per commit or per
    release plus a policy for when a regression blocks a merge, which is real infrastructure that
    should be built because it is needed, not because benchmarks felt like diligence. A number
    committed to the tree is a number from a different machine on a different day.
  - **No `EXPLAIN` plan assertions,** deliberately deferred rather than dropped. They are the sharpest
    tool for the dropped-index class and cost no timing at all, but a plan depends on table
    statistics, so a fixture too small plans differently from production and the assertion pins the
    wrong shape. Revisit when a fixture is realistic enough for the planner to agree with production,
    or when a specific plan regression proves the benchmarks insufficient.

    **Amended ([#725](https://github.com/hyperscaleav/omniglass/issues/725)):** the deferral stands for
    the plan a planner **prefers** and is lifted for an access path a query can **reach**, which is a
    different question with a different dependence on statistics. Neither revisit condition above is
    met (no fixture here is production-sized, and nothing regressed a plan) and neither is claimed:
    what ships is narrower than what was deferred. Planned with `set enable_seqscan = off`, which
    prices the sequential path out rather than forbidding it, `EXPLAIN` answers "which index is this
    relation reachable by", and that answer does not move with the fixture's size: measured over the
    health reads on an empty database, on a 45-row fixture with no statistics at all, and on the same
    fixture analyzed, the join shapes around the scan differed in all three (hash join to merge join, a
    sort appearing and disappearing) while the scan of `property` was the same index scan carrying the
    same index condition in every one. The instrument is
    `internal/storage/storagetest/accesspath`, it gates in `make test` as counting does, and three
    rules bound it. Assert the access path of **one relation**, never the plan's shape. Assert the
    **index condition** as well as the index name, because a coerced predicate or a leading column
    dropped from the filter leaves the index NAMED in the plan and walked rather than searched, which
    reads as a pass. And never assert a duration or a preference, which is this entry's standing
    invariant, untouched. It earns its place on the class no other instrument here can see: a partial
    index can sit in `pg_indexes` and be unreachable by the read it was built for, because its
    predicate stopped being provable from the query's own clauses, and the statement count is identical
    either way.
  - **No timing assertion anywhere,** in a benchmark or a test. This is a standing invariant, not a
    property of this slice.
- **What the first run measured, because a benchmark set is only as honest as its floor.** One pool
  acquire and one empty statement costs about 265us on the dev box, and every other number contains
  one copy of that per statement the call issues. The reads land between 0.7ms and 5.7ms with a
  run-to-run spread near 5%, so a plan regression in them is visible. The health RECOMPUTE chain does
  not: a raise-and-clear pair costs about 22ms across 58 statements, so roughly three quarters of it
  is transport that no planner decision can move, and its spread is three times the reads'. It was
  built, measured, and deliberately not shipped, because a benchmark that cannot detect the regression
  it appears to watch is worse than none. A path that round-trip-bound is counted, not timed, which is
  the same reasoning that made counting the first instrument.
- **Context:** [#651](https://github.com/hyperscaleav/omniglass/issues/651), filed out of Fred's
  question during [#643](https://github.com/hyperscaleav/omniglass/issues/643): "how do we know if
  performance decreased or increased". [#650](https://github.com/hyperscaleav/omniglass/issues/650)
  answered the counting half. This is the half counting cannot answer, and it was deliberately
  sequenced after [#649](https://github.com/hyperscaleav/omniglass/issues/649) replaced per-test
  migration with a template-database copy: while provisioning was about 90% of a storage run, timing
  anything on top of it measured the harness.
### ADR-0095: An operator forks a shipped registry row instead of the platform writing it

- **Date:** 2026-08-09 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/),
  [core entities](/architecture/core-entities/), [API](/architecture/api/)
- **Decision:** An operator's edit of a shipped (`official: true`) registry row never writes that
  row. It writes a **shadow**: the operator's version of the row's mutable columns, stored in one
  registry-agnostic `registry_shadow` table keyed `(registry, row_id)` where `row_id` is the shipped
  row's **own uuid**. Every read resolves the shadow over the official row, and restore-to-defaults
  (`POST /component-types/{id}:restore` for the first adopter) is deleting the shadow. The official row is neither updated nor
  deleted by any operator action, so a release can improve it without stomping anyone, and a delete
  of a shipped row stays refused whether or not it is forked: a fork is an overlay, not ownership.
  `component_type` is the first adopter; the other registries carrying `official` adopt as their
  slices land, needing no schema of their own.

  Three sub-decisions carry the shape, and an adopter must keep all three.

  **1. The shadow is keyed on the shipped row's uuid, in one generic table, not by a `namespace`
  column on the registry.** The rejected alternative was `namespace text` with the unique relaxed
  from `(name)` to `(namespace, name)`. It fails on addressing, which is the thing this repo has
  already settled twice. Relaxing the unique makes every name-keyed lookup on the registry return
  two rows, and `QueryRow` takes an arbitrary one; that is eight call sites on `component_type`
  alone, several of them in helpers **shared with twelve other registries**
  (`guardTypeMutable`, `registryAuditImage`, `deleteTypeRow`, `requireRegistryRow`), so a single
  missed site is a silent wrong-row bug rather than a failure. Worse, a same-table shadow needs its
  own uuid, giving one logical row two, while `product.component_type_id` and
  `role_component_type.component_type_id` are foreign keys to `component_type(id)` that would keep
  naming the official one: the fork would not take effect for the rows that matter, and the `id` a
  write echoes back would change on fork, which is the namespace leaking into the URL that
  [#655](https://github.com/hyperscaleav/omniglass/issues/655) forbids. The literal `(namespace, id)`
  composite primary key the issue sketched makes `id` non-unique and forces dropping all three
  inbound foreign keys, including the `ON DELETE RESTRICT` bought by
  [#507](https://github.com/hyperscaleav/omniglass/issues/507). Keying the shadow on the shipped
  row's uuid keeps `component_type_name_key` a global unique, keeps every foreign key and every
  existing lookup matching exactly one row, and reduces the change to *what a read resolves to*
  rather than *what anything addresses*. Generic rather than one shadow table per registry because
  twelve more registries adopt later and the per-type rule columns of
  [#657](https://github.com/hyperscaleav/omniglass/issues/657) add columns to them: a typed shadow
  table would double the DDL cost of every future column on every adopter, and this costs none. The
  polymorphic `(registry, row_id)` reference is the shape `audit_log`'s `(resource, resource_id)`
  already uses.

  **2. A fork captures the WHOLE mutable row, not only the edited column.** So a later release's
  improvement to a column the operator did not touch does **not** reach a forked row: the operator
  took the row over, and restore is how they hand it back. The alternative, a sparse overlay where
  unedited columns keep tracking the release, is not merely a different trade here, it is
  unrepresentable: `stem`, `icon` and `abbrev` are nullable and **null already means "inherit from
  the nearest ancestor"**, so a sparse overlay would need null to mean both "not overridden" and
  "inherit", i.e. a third state and a separate overridden-column set. That is the merge and
  three-way-diff machinery the issue's thin cuts rule out. The image is therefore stored as a
  whole-row jsonb of the mutable columns, each nullable column written as an explicit null rather
  than dropped, and the resolve overlays **the keys the image carries**. That last distinction is
  what makes a column added to a registry *after* a fork resolve to its official value instead of to
  nothing, which is the only sane answer for a column the fork could not have had an opinion about.

  **3. Inheritance across the shadow boundary resolves PER NODE, in official structure space.** The
  split that makes it decidable is **structure versus facts**. Structure is the uuid, the name, the
  `official` flag and `parent_id`; a shadow never carries any of them, so a fork restates what a
  node says and never moves, renames, or re-provenances it. Facts (`display_name`, `stem`, `icon`,
  `abbrev`, `default_tags`) resolve shadow-over-official at each node independently. The walk
  therefore visits the same node sequence it always did, and at each node reads that node's
  effective row before applying the existing first-non-null-wins rule. Forking an ancestor reaches
  every descendant that does not override the field; forking a leaf does not cut it off from the
  ancestors it inherits from; a chain with some nodes forked and some not is not a special case,
  because **there is no such thing as a forked chain, only forked nodes**. The rejected alternative,
  per chain ("a forked leaf walks only forked ancestors"), would make forking a leaf silently drop
  every inherited fact, and it names an object the schema does not have: `parent_id` is per row. The
  ruling survives `parent_id` becoming patchable later, which it is not today
  (`ComponentTypePatch` has no parent field and there is no reparent leg): the walk follows the
  *effective* row's parent, so a reparenting shadow would diverge the chain from that node up, which
  is still per node.

  **Amended ([#709](https://github.com/hyperscaleav/omniglass/issues/709)): a FOURTH fact, and the one
  an adopter is most likely to get subtly wrong. Every path that reads a row in order to write its
  shadow takes the registry row's lock in a statement of its OWN, before the read that resolves the
  shadow over it.** Sub-decision 2 is what makes the lock necessary: a fork writes the whole mutable
  row back, so two operators editing different fields of one shipped row at once both compute an image
  from the same starting point, and the second silently discards the first's field with no error and no
  audit anomaly, both writes in the log. What the amendment adds is that `for update` ON the resolving
  read does not do the job. At READ COMMITTED a statement's snapshot is taken when the statement
  begins, which is before it blocks; the waiter then unblocks on a row that was **locked** rather than
  updated, so there is no EvalPlanQual recheck, and the left join it had already evaluated still
  reports the shadow as its stale snapshot saw it. That serialises the two transactions without making
  the second read what the first wrote. It is not a hypothetical: `location_type` shipped exactly that
  lock in [#703](https://github.com/hyperscaleav/omniglass/issues/703), and a paired-fork test still
  lost an edit against it, which is how the shape was found. Locking FIRST makes the resolving read a
  new statement taking a new snapshot, after the lock is held, so it reads the shadow the previous
  holder committed. Cost: one round trip on the patch and restore paths of an adopting registry, the
  same trade [ADR-0108](#adr-0108-settlement-reads-one-clock-and-a-zero-window-is-a-statement-of-intent)
  states for the settle paths.
- **Context:** [#655](https://github.com/hyperscaleav/omniglass/issues/655), the first prerequisite
  of [#657](https://github.com/hyperscaleav/omniglass/issues/657). Thirteen tables carry `official`
  and enforcement was a flat refusal (`ErrTypeOfficial` on any patch), which is correct as far as it
  goes and leaves an operator nowhere to go. It also produced an inversion #657 runs into:
  `component_type` and `product` seed `official: true` and are closed, while `location_type` and
  `standard` seed `official: false` (example content an estate owns) and are open, so a per-type
  rule column would be writable on two registries and unwritable on the other two, backwards from
  where the machinery lives. The alternative on the table was a per-column carve-out from the
  official lock, which trades one inconsistency for a worse one: some columns of an official row
  writable and others not, with the rule living in code rather than in the model. `component_type`
  is the first adopter precisely because it is nested, so it forces sub-decision 3 rather than
  deferring it.
- **Open:** what happens to a shadow when a later release removes the official row it shadows. The
  emergent behaviour today is "orphan it, inertly": resolution is driven by the official row left
  joining the shadow, so an orphan is invisible rather than corrupting, and the polymorphic key
  carries no foreign key that would have blocked or cascaded the removal. Surfacing it as a conflict
  an operator resolves is the alternative, unbuilt.
### ADR-0096: The `system_type` name returns as the coarse space taxonomy

- **Date:** 2026-08-09 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [storage](/architecture/storage/), [API](/architecture/api/), [glossary](/architecture/glossary/)
- **Decision:** A new **`system_type`** table lands: a nested, universally seeded registry saying what
  **kind of space** a system is (`av / room / {board, class, meeting, training, conference, huddle}`,
  `av / sign / {video-wall, interactive-sign}`), exactly parallel to the way `component_type`
  classifies a product. It carries `name`, `display_name`, `stem`, `abbrev`, `icon`, `parent_id`, and
  `official`; `stem`, `abbrev`, and `icon` are nullable and **inherited from the nearest ancestor that
  sets one**, resolved by walking `parent_id` in Go. `system.system_type_id` points at it, **nullable
  for now**; a floor waits until the shipped tree has proven out. Both foreign keys are `ON DELETE
  RESTRICT`, and the delete path pre-counts **both** sides, so a type that still parents another type
  or still classifies a system is refused with the registry's own in-use error rather than a raw
  constraint failure.

  It does **not** carry `default_tags`, the one column `component_type` has that this does not: a
  product's instances start from its type's tag set, while a system's effective tags come from the
  platform, its location, and its own system tree, so the column would have no reader.

  `standard` is untouched. The two answer different questions and both stay: the **type** is what a
  system **is**, the **standard** is the blueprint it is **built to**.
- **Decision (the identifier reuse):** `system_type` was the **old column name** for what
  [ADR-0048](#adr-0048-the-standard-blueprint-and-the-template-fork-seed-model) promoted to
  `system.standard_id`, and it sat on the docs-lint denylist. A **table** by that name, with a
  `system.system_type_id` pointing at it, is a different object from the retired column, so the reuse
  is safe at the schema level; the collision is in prose, where a reader who greps finds both. The
  denylist entry is therefore **removed**, not exempted, on exactly the precedent
  [ADR-0085](#adr-0085-the-component_type-registry-returns-as-the-device-class-genus) set for
  `component_type`: an entry cannot express "banned, except in its own reintroduced meaning", and
  every sentence teaching the new registry would otherwise need an escape. The retired sense survives
  in ADR-0048's own prose under the standing retirement-marker exemption. One fossil is left in place
  deliberately and now labelled: `internal/storage/systems.go` maps the constraint name
  `system_system_type_fkey` (the pre-rename name of the **standard** foreign key) alongside
  `system_standard_id_fkey`, and the new key is `system_system_type_id_fkey`, a distinct string that
  maps to its own error.
- **Context:** `standard` is already nested (`parent_standard_id`), so using standard inheritance as
  the taxonomy was on the table and is the wrong axis. Standard inheritance expresses **design forks**,
  two ways to build the same kind of room; the coarse classifier is a different question, it wants a
  universal shipped seed, and one estate can hold ten signage standards and six classroom standards
  under a single coarse type. That makes the system side exactly parallel to the component side, where
  `component_type` is the coarse nested taxonomy with a universal seed and `product` is the specific
  artifact.

  The forcing function is naming. A generated system name needs a stem in the space vocabulary
  (`boardroom-2`) and a label render needs a compact form (`br`), and nothing in the estate model
  spoke either: a standard is a blueprint name (`meeting-room-v2`), a location type is where the room
  sits rather than what it is for, and neither is universally seeded. Every identity fact the console
  reached for turned out to live at the same missing level, the same tell that made ADR-0085's case.
  The shipped tree is deliberately not filler: its display names, stems, and abbrevs are the strings
  the whole naming arc will render from.
- **Tracked under** [#656](https://github.com/hyperscaleav/omniglass/issues/656), the second
  prerequisite of epic [#657](https://github.com/hyperscaleav/omniglass/issues/657).

### ADR-0097: Allocation tests the name it would mint, rather than reading the ordinal it stored

- **Date:** 2026-08-10 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [storage](/architecture/storage/), [glossary](/architecture/glossary/)
- **Decision:** The number a generated name is minted from becomes a **stored, nullable**
  `component.ordinal`, written in the same advisory-locked transaction that already computed it.
  Nullable is the load-bearing part: a name an operator typed, and a name an operator renamed
  (`:rename` clears the column in the same statement it clears `name_generated`), have no ordinal the
  platform owns, and absent is how that is written down.

  **Sibling allocation does not read that column.** It reads sibling **names** and returns the lowest
  ordinal whose **minted** name no sibling in the placement bucket already holds. One pure function
  (`mintName`) owns the name's shape, and the allocator asks it for candidates instead of picking
  siblings apart.
- **Decision (no scoped-unique index):** The ordinal gets no unique constraint of its own, only a
  `>= 1` domain check. A "one ordinal per (bucket, stem)" index cannot be expressed, since the stem is
  resolved from the `component_type` chain rather than stored, and it would be redundant if it could:
  for a platform-named row the name **is** the stem and the ordinal formatted together, so two rows
  colliding on both already collide on the name, which the scoped-name unique indexes
  ([ADR-0088](#adr-0088-a-placement-change-is-an-authorization-act-so-a-move-is-its-own-verb)'s
  placement buckets) refuse. A redundant constraint would add a second `23505` to map and a second way
  for a race to surface as a 500.
- **Context:** [#657](https://github.com/hyperscaleav/omniglass/issues/657) proposed that storing the
  ordinal would let sibling allocation "stop being a string-prefix scan and become a lookup keyed on
  type and placement". Building it exposed two problems with reading the column, and one thing the
  column was being credited with that it does not actually do.

  **An ordinal-only allocator is unsound.** The rows holding a NAME are not the rows holding an
  ordinal. An operator can type `display-1` by hand: that row owns the name and, correctly, no
  ordinal. An allocator consulting stored ordinals sees an empty set, picks 1, mints `display-1`, and
  hits the scoped-name unique index. The generator deliberately takes an advisory lock rather than
  retrying a `23505` (retry was rejected when the generator landed, since the abort takes the whole
  transaction with it), so that collision is a 500 on an ordinary create. Mutating the allocator to
  read only rows with a stored ordinal reproduces it exactly, and the test that catches it is now
  part of the suite.

  **Keying on the type would be worse than keying on the stem.** A stem is inherited, so two sibling
  component types can resolve the same one; giving each its own ordinal space would have both mint
  `display-1`. The uniqueness that matters is the name's, so the space that matters is the name's.

  **The column is not what unblocks a stem-less name.** The prefix scan could not count a stem-less
  sibling because its filter was a bare `-` that no name matched, which is a property of *parsing*,
  not of *storage*. Inverting the loop fixes that on its own: `mintName("", 1)` is `1`, and the
  allocator tests it like any other candidate. So a floor whose ordinal genuinely is its name
  ([#657](https://github.com/hyperscaleav/omniglass/issues/657) slice 7) is now expressible, and it is
  asserted at the storage layer against a real database ahead of its first caller.

  What the stored ordinal does buy is everything **downstream** of allocation, which is what the rest
  of the epic needs: the bare render reads a fact instead of re-deriving one from the string it is
  about to replace (finishing what
  [#654](https://github.com/hyperscaleav/omniglass/issues/654) left half-done, and making its
  never-restamp-an-operator's-name guarantee structural rather than defensive), a label rule can read
  `.Ordinal` without parsing it back out, and a recompute-and-compare invariant test can prove no
  stored ordinal has drifted from what allocation would produce. It also generalizes: when a name
  comes from an operator-editable rule rather than `<stem>-<n>`, the allocator changes its mint and
  nothing else, where a parser would have needed a matching second implementation.
- **Tracked under** [#681](https://github.com/hyperscaleav/omniglass/issues/681), the first slice of
  epic [#657](https://github.com/hyperscaleav/omniglass/issues/657).

### ADR-0098: A label rule reads what an entity IS, never where it sits

- **Date:** 2026-08-10 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [storage](/architecture/storage/), [glossary](/architecture/glossary/)
- **Decision:** A label rule is an operator-authored Go `text/template` evaluated over a **closed,
  flat `map[string]string`** and a **closed FuncMap** (`title`, `upper`, `lower`, `slug`). The map
  carries the entity's own columns and the facts resolved from its **classification** chain, and
  carries **nothing about its placement**: not its location, not its parent, not the system it
  staffs.

  | entity | keys |
  | --- | --- |
  | component | `Name`, `Ordinal`, `TypeName`, `TypeAbbrev`, `Stem`, `ProductName`, `VendorName` |
  | system | `Name`, `TypeName`, `TypeAbbrev`, `Stem`, `StandardName` |
  | location | `Name`, `TypeName` |

- **Decision (the sandbox is the data map AND the grammar):** Two allowlists, the same argument
  twice.

  The DATA half filters nothing. The value type is `string`, so field traversal and a method call
  fail at execution rather than reaching further; a key the map does not carry renders as the empty
  string (`missingkey=zero`, set explicitly rather than relied on); a secret, a credential and a
  token are absent **structurally**, never put in. That is why a general template engine beat a
  custom interpolation DSL: the security property a label rule needs is not a syntax that cannot
  express dangerous things, it is an environment that contains nothing dangerous, and the environment
  is ours entirely. Adding a key is the only way to widen what a rule can read, so the key set is
  pinned by a test.

  The GRAMMAR half is an allowlist over the **parsed tree**, checked at parse time, admitting a
  closed set of node types (text, action, pipeline, command, field, dot, identifier, literal,
  `{{if}}`, comment) and a closed set of function names (`title`, `upper`, `lower`, `slug`, plus
  `and`/`or`/`not` and the six comparisons). Everything else text/template offers is refused:
  `printf`, `print`, `println`, `call`, `index`, `slice`, `len`, `html`, `js`, `urlquery`, variable
  declarations, `{{range}}`, `{{with}}`, `{{template}}` and `{{define}}`.

  **A superseded claim, corrected here:** an earlier draft of this entry said `printf`'s width verb
  was "the one way a closed map of short strings still produces output far larger than its inputs"
  and that the ceiling therefore lived in a cap on rendered length. Both halves were wrong. The cap
  bounds bytes reaching the WRITER, and a value built inside a pipeline is materialized by `fmt` and
  never written, so the cap never sees it: 437 bytes of operator-authored rule allocates 85 MB and
  writes 8, with every further doubling 35 more bytes of rule for twice the memory, which OOMs the
  single binary and re-triggers on every later write to any entity of that type. Refusing `:=` does
  not fix it (the nested-pipeline form needs no assignment) and neither does removing a FuncMap entry
  (`printf` is a builtin the FuncMap never granted). A closed grammar does, and it does it at
  rule-edit time so nothing is ever stored. The length cap stays as a ceiling on OUTPUT, which is now
  reachable only by literal text an operator typed.

- **Context (why placement is excluded):** The label is **stored**, so a stored value must equal what
  its rule produces, and every input to a rule is therefore something a write path has to re-render
  on. The keys above change on exactly five acts, all of them the entity's own: create, rename, move
  (which re-mints the ordinal), reclassify, and reset. That closed set is what makes the
  recompute-and-compare invariant provable rather than hopeful.

  Adding the location's name would add a sixth, and it would not be the entity's own: renaming a
  **location** would silently stale every label under it, and the invariant would be right to fail.
  The epic's acceptance text ("a component created with a product and a location gets a label")
  reads either way, and is satisfied by the classification alone. The cascade that would make
  placement facts safe belongs with the slice that owns bulk recompute
  ([#685](https://github.com/hyperscaleav/omniglass/issues/685)), which can then add the keys and the
  cascade together. Ancestry is not lost meanwhile: it is already the entity's `path` and its two
  renders, beside the label rather than inside it.

- **Decision (the global tier is two columns, not one):** The global rule is one row per labelled
  entity kind in `label_rule`, carrying `default_template` (boot-seed space, rewritten
  authoritatively on every start) and `template` (operator space, nullable), resolved as
  `coalesce(template, default_template)`. Clearing `template` is restore-to-defaults.

  One column could not be both. An authoritative boot seed over a single column stomps an operator's
  rule on the next restart; a seed-if-absent freezes the shipped default at whatever the first boot
  wrote and no release can improve it. That is the problem
  [ADR-0095](#adr-0095-an-operator-forks-a-shipped-registry-row-instead-of-the-platform-writing-it)
  solves for a shipped registry row, and this is the same answer at the smallest scale that expresses
  it: shipped values and operator values live apart and reads resolve one over the other. It is two
  columns rather than a `registry_shadow` row because this table is keyed on an entity kind rather
  than a uuid, and three rows do not earn an overlay of their own.

- **Decision (a rule inherits per node, like every other type fact):** On a nested registry a
  `label_rule` is nullable and resolves by the first-non-null walk `stem`, `icon` and `abbrev`
  already follow, crossing the fork boundary **per node** with the chain always the official one
  (ADR-0095 decision 3). So forking a shipped ancestor's rule reaches every descendant that declares
  none, and a descendant that declares one overrides for itself alone. The analogous question
  ADR-0095 settled for facts has the same answer for rules, because a rule **is** a fact of the node
  that declares it.

- **Decision (an empty render stores NULL and keeps the pen):** A rule with nothing to say about a
  row stores SQL `NULL`, not a blank string, and `display_name_generated` stays true. Marking the row
  operator-owned because a rule once rendered nothing would exclude it forever from the recompute
  that exists to fix exactly that.

- **Decision (the pen is backfilled, claimed only where there is no label):** The migration adding
  `display_name_generated` defaults it false, and a one-time backfill then claims it on every
  existing row whose `display_name` is `NULL` or empty, leaving it false where the operator typed
  something. A row with no label has nothing to protect and is the majority case; left unclaimed it
  is inert forever, because the write-path stamp returns immediately when the pen is false and a bulk
  recompute only touches rows the platform already owns. The backfill writes the PEN and not the
  label, since rendering needs the rule engine and logic lives in Go, so those rows take their labels
  from the next write that touches them or from the recompute that can now see them.
- **Consequence:** `label_rule` columns land on `component_type`, `system_type`, `location_type`,
  `product` and `standard`, but only `component_type` carries the fork today, so a rule on a
  **shipped** row of the other four is not operator-editable until each adopts ADR-0095's primitive.
  That is the pre-existing state of those registries rather than a new restriction, and the tier that
  matters most for components (`component_type`) is forkable now.
- **Amended (#729):** the key table above is the map **as of this decision**, and it is a historical
  snapshot rather than the live set: [ADR-0100](#adr-0100-a-label-cascades-where-the-blast-radius-is-a-placement-and-waits-for-the-verb-where-it-is-the-estate)
  added the placement keys and #686 gave a system its `Ordinal`. The map is no longer typed out
  anywhere. Each kind's keys are declared once in `internal/storage/label_keys.go`, beside the
  accessor that produces each value, `labelData` builds the map by ranging that declaration, and
  `docs/src/generated/labeldata.json` (`make gen`, via `cmd/labelgen`) renders the per-kind tables
  [core entities](/architecture/core-entities/) publishes. So "the key set is pinned by a test" above
  is now pinned by CONSTRUCTION, and what the tests hold is the artifact against the declaration and
  the two `label_rule` API descriptions against it too. That last one was already wrong: both
  enumerated the map as it stood before ADR-0100, so the console's field help and the CLI reference
  taught seven keys where a rule could read nine.
- **Tracked under** [#682](https://github.com/hyperscaleav/omniglass/issues/682), the second slice of
  epic [#657](https://github.com/hyperscaleav/omniglass/issues/657), amended by
  [#729](https://github.com/hyperscaleav/omniglass/issues/729).

### ADR-0099: The acronym list is one replaceable setting, not a shipped list plus operator additions

- **Date:** 2026-08-10 | **Status:** Accepted | **Pages:** [settings](/architecture/settings/),
  [core entities](/architecture/core-entities/), [glossary](/architecture/glossary/)
- **Decision:** The acronym dictionary a label rule's `title` consults is **one key**,
  `label.acronyms`, in a new `platform,client` settings namespace. An operator's list **replaces**
  the shipped one; the two are told apart by the settings engine's existing **provenance**
  (`sources["label.acronyms"]` reads `default` or `platform`), not by keeping them as separate
  values and unioning them at read time.
- **Why not shipped-plus-additions:** The epic's scope text reads as a union, and a union is what an
  operator adding one word wants. It was rejected because of what it costs everywhere else. Merge in
  this engine is presence-based over generic maps and a non-map value overrides wholesale, so a
  union would be a merge rule this one key does not share with any other setting, and reading
  `label.acronyms` would no longer tell you the effective dictionary: the value would be a fragment
  and the engine would hold a list nothing on the wire reports. It also makes a shipped entry
  **unremovable**, which matters more than it sounds, since a word we ship that an operator's estate
  spells differently would then be uncorrectable. The union's one benefit, not retyping the list to
  add a word, is a console problem, and the console already read-modify-writes the effective value
  for every other setting.
- **Consequence, stated rather than discovered:** an operator who overrides the list stops receiving
  later releases' additions to it, exactly as overriding any other setting freezes it. The console
  badges the key as overridden, and restoring the namespace returns the shipped list.
- **Decision (the engine's lifecycle):** The dictionary is resolved from the settings cascade **at
  render time**, and the compiled engine is cached against the dictionary itself
  (`label.EngineCache`). Parsing binds a template's FuncMap, so a dictionary change produces a
  **new** engine rather than mutating one, and a rule compiled against the old engine keeps the old
  casing for as long as it lives; the storage path re-parses on every render, so the longest a stale
  dictionary survives is one render and an operator's edit needs no restart. A generation counter
  bumped by the settings write path was rejected for having a failure mode a content key cannot
  have: a second write path that forgets to bump it.
- **Consequence:** a render costs one read of the override table, on a write path already several
  round trips deep. A caller rendering many rows (the bulk recompute, [#685](https://github.com/hyperscaleav/omniglass/issues/685))
  resolves the engine once and passes it down, which is why the render functions take an engine
  rather than reaching for one.
- **Decision (the gateway reads that override in the caller's transaction, not off the pool):** A
  stamp runs inside a transaction, so it is already holding a connection; issuing the settings read
  against the pool acquires a second one, and a pool whose connections are all held by writers each
  waiting for a second connection is **deadlocked**, not slow. It needs no unusual code to reach (a
  bulk import of the estate sizes this epic exists for is enough) and it fails as a hang. So
  `Service.ResolveOverride` takes an override level the caller has already read, over the same level
  stack `Resolve` builds, and the gateway reads it on its own transaction; the dictionary and the row
  being stamped then come from one snapshot as well. A one-connection pool is the whole population of
  that race, which is how the regression test reaches it deterministically rather than under load.
- **Decision (parse and render need different engines):** Rule **validation** uses a
  dictionary-less engine, because `label.New` binds the same four function names whatever dictionary
  it is given and the grammar check walks a static allowlist: whether a rule parses is a fact about
  the rule alone. Only execution differs, so only the render path carries the current engine, and a
  template compiled by the validator is discarded rather than reused.
- **Tracked under** [#684](https://github.com/hyperscaleav/omniglass/issues/684), the fourth slice of
  epic [#657](https://github.com/hyperscaleav/omniglass/issues/657).

### ADR-0100: A label cascades where the blast radius is a placement, and waits for the verb where it is the estate

- **Date:** 2026-08-10 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [storage](/architecture/storage/), [identity and access](/architecture/identity-access/),
  [glossary](/architecture/glossary/)
- **Decision (placement is in the data map):** A component's label rule reads `LocationLabel` (the
  label of the location it sits at) and `SystemTypeLabel` (the label of its primary system's type); a
  system's reads `LocationLabel`. This **reverses the narrowing in
  [ADR-0098](#adr-0098-a-label-rule-reads-what-an-entity-is-never-where-it-sits)**, whose title is now
  false of the code and is left standing as the record of what was believed. The worked example is
  `{{.SystemTypeLabel}} {{.LocationLabel}} {{.TypeName}}` rendering `Boardroom 204B Display`.
- **The keys are flat, never dotted,** because the map is a closed `map[string]string` and that
  flatness is half the sandbox argument ADR-0098 rests on: a string cannot be traversed through, so
  `{{.Location.Label}}` would be a handle on another row where `{{.LocationLabel}}` is a fact copied
  out of one.
- **Which location, precisely:** the row's OWN `location_id`. Not an ancestor of it, and not the
  location its plane root sits at. A component's label says where that component is, so it reads the
  column that says so; a nested component with no location of its own reads placement as absent. The
  alternative would make a label depend on an ANCESTOR COMPONENT's placement, so moving a parent
  would stale every descendant, and it costs a recursive walk per row on a path that runs on every
  create. `LocationLabel` is the location's READ LADDER (the label an operator typed, else the
  location's own name), not the raw column, because a shipped estate has no location labels at all
  and reading the column alone would render placement as blank for every row in it.
- **Consequence, and the reason this is one slice rather than two: ADR-0098's completeness argument
  is void.** It could enumerate five write paths and call the set complete precisely because every
  fact its map held was the labelled row's own. That argument was already thinner than it read
  (`TypeName`, `ProductName`, `VendorName`, `Stem`, `TypeAbbrev` and the rule itself are columns on
  registry rows), and placement ends it: the set of acts that stale a label is no longer the set of
  acts on the labelled row. The write paths are therefore **derived from the map** and, more
  importantly, the invariant that guards them stopped being an enumeration at all: it is now an
  estate-wide question the gateway answers (`PreviewLabelRecompute` returning nothing), so a write
  path nobody thought of fails it.
- **Decision (the line is blast radius, not ownership):** An act whose blast radius is bounded by a
  PLACEMENT cascades eagerly, inside its own transaction, so the estate is never observably stale: a
  location's rename, relabel or reclassify restamps what is placed at it; a system's reclassify
  restamps its member components; every act that moves a component's primary membership (`AddMember`,
  `RemoveMember`, `SetPrimaryMember`, `AssignRole`'s implicit bind, a create naming a system, and a
  system's DELETE, which releases every membership it holds) restamps that component; a system's
  `:move` stamps its own label, which it never did before.
  An act whose blast radius is bounded only by the ESTATE restamps nothing and waits for the verb:
  a rule changing at any tier, a `component_type`'s or `product`'s or `vendor`'s `display_name`
  changing, and the acronym list changing. That is the epic's own argument applied consistently:
  editing a shared classification must not silently rewrite fifteen thousand rows any more than
  editing a rule may.
- **Consequence, added by the epic's review pass: a write path can be one the DATABASE performs.**
  Deriving the write paths from the map caught every explicit mover of a primary membership and
  missed the implicit one. `system_member_system_id_fkey` is `ON DELETE CASCADE`, so deleting a
  system has always taken its memberships with it, in the parent's own `DELETE` where the gateway
  can attach nothing to the loss. A component in a boardroom under `[{{.SystemTypeLabel}}]` went on
  reading `[Boardroom]` after the boardroom was gone, and the invariant this decision rests on
  (`PreviewLabelRecompute` returning nothing) said so, which is the property it was built for. The
  fix is not another restamp bolted to the delete: the memberships are now **released explicitly**,
  one step ahead of the row's own delete, so a delete is the same act `RemoveMember` performs and
  gets the same two consequences (the sole survivor is promoted to default, the released components
  restamp in one recompute rather than one apiece). The promotion half was a pre-existing divergence
  between the two doors into `system_member`, and it is fixed here rather than filed because the
  label the delete leaves behind depends on which membership answers afterwards. The generic
  scoped-CRUD delete grew a `beforeDelete` hook to hold it, since the rows a hook needs are
  unreadable by the time an `afterDelete` runs. The completeness lesson generalizes past labels:
  **an `ON DELETE CASCADE` is a write path with no Go on it**, and the estate's other two
  (`system_role_assignment` on both `system_id` and `role_id`) feed nothing into a rule today.
- **Consequence:** a location rename is bounded by what is placed AT that location, not by its
  subtree, because a component reads the label of the room it is in and never that room's building.
  A campus rename is free. That is a property of the data map, so if a later slice gives a location
  an ancestry fact (a positional type's generated name is the obvious candidate) the cascade owes it
  a subtree arm, and the test that pins the current answer is the one that fails first.
- **Decision (a preview is an apply that rolls back):** The read-only implementation of a preview
  cannot list exactly what the apply changes. Recomputing locations moves their labels, which stales
  the components and systems placed at them, so the honest answer includes rows of two other kinds
  that exist only as a consequence of writes a read-only pass never made. Simulating those writes
  would be a second implementation of the cascade for the two to drift apart on. So a preview runs
  the apply, collects the change set, and rolls back: exact by construction rather than by argument.
  The cost is stated rather than hidden: a preview is not a pure read. It takes the operation lock
  and `FOR UPDATE` on the rows it visits and produces WAL that is discarded, so it is an operator
  gesture, not something to put behind a keystroke. It still does not promise atomicity ACROSS the
  pair, and deliberately: holding a lock between two HTTP requests would let an operator who opened a
  preview and wandered off block every write on the tier. The apply's own returned set is what closes
  that gap.
- **Decision (epic D1, one audit row for the operation):** A bulk recompute writes **one** audit row,
  verb `recompute`, resource `label_rule`, `resource_id` the entity kind, carrying the affected count
  and its per-kind split. Per changed entity was the alternative and it loses on both halves of the
  question it claims to answer better: it writes fifteen thousand rows for one click, in one
  transaction, on a table every other write shares, and the per-entity trail it buys is a
  restatement, since a generated label is DERIVED and "why does this row read what it reads" is
  already answered by the rule and the row's own facts. "Who changed the estate's labels and when" is
  answered by exactly this row and by nothing in a per-entity trail. The nearest precedent agrees: a
  health recompute cascades across a whole ownership chain and audits nothing, because it is a
  consequence of an act that is itself audited. Which is also why a CASCADE writes no row of its own:
  the rename or reclassify that triggered it already has one. The row is keyed on the rule rather
  than on the entities because `label_rule`'s primary key genuinely is the entity kind, so it is the
  one key a rename cannot orphan.
- **Decision (a cascade is not scope-filtered; the verb is):** The verb selects on the caller's read
  scope AND their update scope, both injected into the one query, so an operator can neither preview
  nor apply outside what they may already see and change. A cascade is not scope-filtered, because it
  is not a query anyone asked for: it is the rest of a write the operator already made, and leaving a
  row stale because it sits outside the grant that let them rename the location would break the
  invariant the stored label rests on, silently. The health recompute crosses scope boundaries for
  the same reason. No new permission either way: a recompute is gated by the entity's own `:update`,
  and so is the preview, because a preview is half of an edit rather than a report.
- **Decision (lock order, written down rather than discovered):** membership, then label, then
  health. A bulk recompute takes one coarse advisory lock for the whole operation plus `FOR UPDATE`
  on the rows it visits; the single-row stamps take neither, because each already holds its row's
  lock from the `UPDATE ... RETURNING` it rides behind.
- **Consequence, measured:** the recompute and the location cascade are both flat in row count, held
  there by [#650](https://github.com/hyperscaleav/omniglass/issues/650)'s counting instrument rather
  than by intent, with the classification resolved once per distinct product or classifier pair and
  the placement facts, the global rule and the acronym dictionary each read once per operation. A
  placed, system-bound, generated component create costs nineteen statements, of which exactly one is
  this decision's.
- **Tracked under** [#685](https://github.com/hyperscaleav/omniglass/issues/685), the fifth slice of
  epic [#657](https://github.com/hyperscaleav/omniglass/issues/657).

### ADR-0101: The first of its stem in a bucket carries no ordinal, and the mint that says so is the one allocation tests

- **Date:** 2026-08-10 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [storage](/architecture/storage/), [glossary](/architecture/glossary/)
- **Decision (epic D3, ordinal suppression):** A generated **system** name suppresses the ordinal on
  the first of its stem in a placement bucket: a room's only boardroom is `boardroom` and the second
  is `boardroom-2`. The order dependence this buys is accepted rather than mitigated, and it is
  worth stating in full rather than in the abstract. Allocation is lowest-free
  ([ADR-0097](#adr-0097-allocation-tests-the-name-it-would-mint-rather-than-reading-the-ordinal-it-stored)
  preserved it), so **deleting `boardroom` while `boardroom-2` survives lets the next create take
  `boardroom` again**, and `boardroom-1` never exists at any point in that sequence. The same rule
  therefore yields different names for the same estate depending on the order it was built in. The
  operator-facing string is what this epic exists to improve, and a room called `boardroom-1` when
  there is only one of it is the defect the epic was filed about.
- **Decision (suppression is a property of the MINT, not of the shape function):** `mintName` became
  a `nameMint` value: a resolved stem plus a `bareFirst` flag, with the shape as a method on it. It
  is NOT a branch inside the shape function, and it is not a global change to the shape, because a
  component that suppressed at ordinal 1 would rename every generated component that already exists
  (`display-1` is the shape #681 has been minting) and would break the no-expected-value-changes
  rule this epic runs under. Components are counted things in a rack, where `display-1` beside
  `display-2` is what an operator writes on the label; a system is a room, and a room with one
  boardroom in it does not call it `boardroom-1`.
- **Decision (the allocator takes the mint, not the stem):** `pickOrdinal` takes the `nameMint` and
  tests `mint.name(n)` for each candidate. This is the load-bearing half. A mint that suppressed
  while the allocator still tested `<stem>-<n>` would disagree on exactly ordinal 1, so the first
  create would take `boardroom` and the second would test a free `boardroom-1`, mint `boardroom`,
  and hit the scoped-name unique index as a `23505` the transaction cannot recover from. Passing one
  value to both is what makes that disagreement unrepresentable rather than merely unlikely: there
  is no second spelling of the shape to keep in step.
- **Decision (where the choice comes from, and the seam for the next slice):** `bareFirst` is a
  field on the mint, resolved per entity kind today (`componentMint` false, `systemMint` true) and
  intended to be resolved per TYPE.
  [#687](https://github.com/hyperscaleav/omniglass/issues/687) gives `location_type` a nullable name
  rule, and a rule that produces a positional name fills this same field from a per-type fact: it
  consumes the seam rather than replacing it. A per-kind default is where the fact lives until a
  type carries one, not a competing mechanism. A stem-less mint ignores the flag outright, since
  suppressing the ordinal of a name that IS its ordinal (a floor called `1`) would leave nothing at
  all.
- **Decision (the allocation lock is keyed on the bucket, and the stem comes OUT of that key):**
  The advisory lock was keyed on the table, the stem and the bucket, which was sound only because
  the mint was always `<stem>-<n>`, making two stems' name spaces provably disjoint. Suppression
  ends that: stem `wall` at ordinal 2 and stem `wall-2` at ordinal 1 are the same name, and both
  stems pass the rule a stem is validated with. Keyed on the stem, those two concurrent creates in
  one bucket take different locks, read the same siblings, mint the same name, and the loser gets a
  `23505` on a create that supplied no name at all. So the lock now guards what the unique index
  guards, the **bucket**, because the bucket is the only partition of the name space a mint cannot
  cross. The cost is that two creates in one room serialize whatever they are classified as, which
  is the price of the shape being per-type rather than fixed.
- **Decision (a placement bucket is a value, not a pair of pointers):** The lock key and the sibling
  filter became one `nameScope` per entity kind, because the kinds do not agree on how many buckets
  there are. `component` and `system` have three (a parent, else a location, else unplaced) and
  `location` has TWO (a parent, else root), since a location carries no located-at column. The
  location constructor takes no location id, so pairing a location with the three-way shape is not a
  mistake to avoid at each call site, it is a value that cannot be constructed. The key carries the
  table too, so a system and a component sharing a parent uuid never serialize against each other.
- **Decision (the pen spreads to both trees, generation to one):** `name_generated` and both verbs
  (`:rename` freezes it false, `:resetName` returns it) land on **system and location**; only a
  system generates. `location_type` carries no stem to mint from, so `:resetName` on a location
  refuses with a typed error naming the missing fact (422) rather than reporting a reset that did
  not happen. The pen still ships on that tier ahead of its generator, and that is the point: a
  location an operator names today is already frozen when #687's rule arrives, where a pen added
  later would have to decide retroactively who had named every existing row.
- **Decision (no backfill, unlike the label pen):** The column defaults `false` and no one-time
  migration touches it, which is the argument the component column already records: every row that
  exists when the migration runs was named by an operator, so claiming the pen retroactively would
  let the first `:move` silently rename real estate. That is the opposite of
  [#682](https://github.com/hyperscaleav/omniglass/issues/682)'s label pen, which WAS backfilled,
  and the difference is the field rather than a change of mind: "no label" is a state the platform
  may safely claim, while a row always has a name and somebody typed it.
- **Decision (the system bare render stays unwired, and this is not an oversight):** Both halves of
  the compact render's substitution now exist for a system (`system_type.abbrev` from
  [ADR-0096](#adr-0096-the-system_type-name-returns-as-the-coarse-space-taxonomy), and a stored
  ordinal from this slice), and `attachSystemPaths` still passes neither. `RenderBare` substitutes
  whenever it is given both, with no shape check, which is what makes
  [#654](https://github.com/hyperscaleav/omniglass/issues/654)'s guarantee structural for a
  component. A suppressed name carries no digits while owning ordinal 1, so wiring it would print
  `brd1` for a room named `boardroom`: a number on a physical label that appears nowhere in the
  entity's name, the exact defect the ordinal column's own migration cites. Correcting it means
  teaching the render which mint produced the name, which is a rendering decision, not a naming one.
- **Consequence (the write paths were derived, not listed):** The acts that must re-mint a
  platform-owned system name are the ones that change an INPUT to the mint, and the mint reads
  exactly two things: the `system_type` chain's stem and the placement bucket. That yields create,
  `:resetName`, `:move` (both the relocate and the reparent arm, since a parent wins over a
  location) and the `system_type` half of a reclassify, and it also settles two cases a list would
  have missed. Un-classifying a platform-named system reaches the generator with no type and is
  REFUSED there rather than silently keeping the name and handing back the pen. And a `stem` edited
  on a shared `system_type` row is bounded by the estate rather than by a placement, so it does not
  cascade, which is
  [ADR-0100](#adr-0100-a-label-cascades-where-the-blast-radius-is-a-placement-and-waits-for-the-verb-where-it-is-the-estate)'s
  line applied to the name side. The database agrees: `system.location_id` and `system.parent_id`
  are both `ON DELETE RESTRICT`, so no placement changes without one of these acts running.

  The same derivation settles the shape of the reclassify guard, which review caught the code
  getting wrong. The trigger is the classification **changing**, not the `system_type` field being
  present in the patch: the console sends that key on every save so an unclassify can clear, so a
  presence test re-mints on an edit to the label alone, and with a lower ordinal freed by an earlier
  rename that re-mint MOVES the name (`boardroom-2` becomes `boardroom`) under `system:update`, with
  no rename requested and possibly no `system:rename` grant held. A patch that re-states the type
  changes neither input to the mint.

  **The epic's review pass found the same defect on `:move`, and it is fixed here rather than
  filed.** The move arm was gated on the row being platform-named alone, so a `:move` that re-stated
  the location the system already sat at, or supplied neither field (this verb's documented no-op),
  re-minted and moved the name by exactly the sequence above, under `system:move`. It was left wide
  deliberately, on the stated belief that narrowing it would move an existing expected value; that
  premise was false, since systems had only had generated names since this same unmerged branch. The
  guard is now the **bucket** changing, compared as a `nameScope` rather than as a pair of pointers,
  because a parent wins over a location: relocating a parented system leaves it in the same parent
  bucket and must not rename it either. No test expectation moved. `MoveComponent` has the identical
  wide shape, is reachable the same way, and is genuinely pre-existing, so it is left for its own
  issue rather than changed here.

  **Amended ([#696](https://github.com/hyperscaleav/omniglass/issues/696),
  [#691](https://github.com/hyperscaleav/omniglass/issues/691)):** both component-tier guards are
  now closed, and one of the two is spelled DIFFERENTLY here rather than copied. `MoveComponent`
  takes the identical `nameScope` bucket comparison, since the two kinds have the same three buckets
  and a parent wins over a location on both. The reclassify does not. A system reads its stem from a
  `system_type` chain, while a component reads it from a PRODUCT that points at a `component_type`
  chain, one hop further, so the value an operator picks is not the value the mint reads: two
  products classified under one type (or under two types inheriting one stem) mint identical names.
  Keying the component guard on the product id would therefore leave a real reclassify moving the
  name onto a freed lower ordinal, which is the defect this entry refused for the presence test
  wearing a third set of clothes. It compares the RESOLVED STEM instead, and `stemForProduct` is the
  first half of `generateNameForProduct` lifted out so a guard can ask what a name would be minted
  from without minting one. A destination with no stem at all is never equal to anything, so it
  still reaches the generator and is still refused there. The system tier keeps the type-id
  comparison for now and the same residual with it (two `system_type` rows inheriting one stem), a
  real case filed as [#706](https://github.com/hyperscaleav/omniglass/issues/706) rather than fixed
  inside a component-tier slice: the two tiers agree on the RULE (re-mint when an input to the mint
  moves) and disagree only on how far each has to walk to read one. The claim that the component
  reclassify was "not reachable the same way today" survived checking and is narrower than it
  sounds: the console never sends `product` on a save (`web/src/pages/Components.tsx` edits the name
  and the label only), but the API takes it and the generated CLI exposes it, so both defects were
  reachable by the paths the issues named.
- **Consequence:** `Ordinal` joins the system label data map, widening
  [ADR-0098](#adr-0098-a-label-rule-reads-what-an-entity-is-never-where-it-sits)'s closed map by one
  key, and the shipped global system rule deliberately does not use it: for a suppressed first name
  the number and the name disagree, so whether a label says "Boardroom" or "Boardroom 1" is an
  authoring choice an operator makes, not a platform default.

  **Amended ([#693](https://github.com/hyperscaleav/omniglass/issues/693)): the shipped system rule
  reads the ordinal, and the key it reads is the number the NAME carries.** The decline above is
  reversed, and the case that reverses it is the one AV estates are full of: a divisible boardroom is
  two `board` systems in one room, so both rendered "Boardroom", and the operator reading the console
  had less information than the platform holding `boardroom` and `boardroom-2`. The rule is now
  `{{.TypeName}}{{if .Ordinal}} {{.Ordinal}}{{end}}`, the component's verbatim.

  The reversal is TWO changes and only one of them was named when the ruling was made, which is worth
  recording because the other one is where the argument lives. Adding `{{if .Ordinal}}` to the rule
  alone renders **"Boardroom 1"** for the only boardroom in a room, because `{{if}}` on a string is
  false for the EMPTY string and a suppressed first name still owns the stored ordinal 1. That is the
  defect this entry's decline was protecting against, and a rule change on its own walks straight
  into it. So the map's value changes with it: `Ordinal` is now the ordinal the row's name SHOWS,
  which is empty for a suppressed first exactly as it is empty for a row an operator named. The two
  states read alike to a rule because they mean the same thing to a reader: this row's name carries
  no number.

  The suppression is asked of the MINT (`nameMint.suppresses`, the branch `name` itself takes) rather
  than read off the name string, which keeps this entry's central decision true on the label side: a
  name's shape has one implementation, and a second reading of it could disagree at exactly ordinal
  1. It also survives the seam above, where `bareFirst` becomes per-TYPE: a rule hardcoding
  `ne .Ordinal "1"` would have been a per-kind default baked into a template, wrong for the first
  type that chose differently, and unwritable by an operator who should never have to know which
  mint named a row.

  What the reversal costs is stated rather than buried: a rule can no longer render "Boardroom 1" for
  a system named `boardroom`, because the number is not reachable from the map any more. That was the
  authoring choice the decline preserved, and it is the one this entry already calls the defect the
  epic was filed about, so it is refused as a rule rather than offered and warned about. An operator
  who wants those exact words still types them, which takes the pen (#682) and is the honest way to
  say a label is not derived from anything.

  A key was NOT added beside `Ordinal` to keep both readings available. Two spellings of one number
  in a closed map is a difference for a rule author to misinterpret, and the wrong pick reintroduces
  the defect silently, which is the same argument `NameRule.normalized` makes about two spellings of
  one rule.

  The component tier reads its ordinal through the same helper, and the value there is unchanged for
  every row: `componentMint` does not suppress (a rack's `display-1` beside `display-2` is what an
  operator writes on the label), so the helper's answer is the stored number. One meaning of the key
  on both tiers, rather than two tiers that happen to agree today.

  **The write paths were re-derived and grew by none.** Every act that moves a system's ordinal is an
  act that re-runs the mint or hands the pen back: create, `:rename` (which clears it), `:resetName`,
  `:move` where the bucket changes, and the `system_type` half of a reclassify, which is the same set
  this entry derived for the NAME. Each already restamps the label unconditionally in its own
  transaction, and unconditionally is what matters rather than the set being the same: a reclassify
  between two stems can leave the name identical while the ordinal moves (`wall-2` at ordinal 2 is
  the same string as the suppressed first of stem `wall-2`), so a stamp gated on the name having
  changed would have missed it. A delete frees a lower ordinal and re-mints nothing, by this entry's
  own allocation rule, so no surviving row's label goes stale behind it. The completeness invariant
  (`TestNoActLeavesALabelStaleAnywhere`) now runs a system rule that READS the ordinal, where the one
  it ran before could not have seen a hole in any of this.
- **Tracked under** [#686](https://github.com/hyperscaleav/omniglass/issues/686), the sixth slice of
  epic [#657](https://github.com/hyperscaleav/omniglass/issues/657), and amended by
  [#693](https://github.com/hyperscaleav/omniglass/issues/693).

### ADR-0102: A name rule is a declaration a type opts in with, and a rule change renames nothing

- **Date:** 2026-08-10 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [storage](/architecture/storage/), [glossary](/architecture/glossary/)
- **Decision (the opt-in):** `location_type` gains a **nullable `name_rule`**, and its presence IS
  the opt-in: null means an operator names every location of that type. There is no boolean beside it,
  so "this type generates" and "this is what it generates" cannot disagree. A campus, a building and a
  room each have a real-world name an operator holds and the platform does not, so generating one
  would be guessing. `17c` is ground truth, not a default.

  This entry shipped saying "of the shipped place vocabulary only **floor** is genuinely
  auto-nameable, and it ships positional", so three of the four shipped types stayed on the opt-out.
  **Amended (#657): that is now false in both halves.**
  [ADR-0103](#adr-0103-a-positional-name-is-allocation-order-and-the-real-world-designation-is-a-label)
  was reversed for `floor` and the rule was dropped from the seed, on the argument that a floor's
  designation (B2, LG, G, M, 12A) is not an integer at all, so **all four** shipped types are on the
  opt-out and no shipped type is auto-nameable. The opt-in itself is unchanged, and it is reached by
  an operator declaring a positional type of their own. What that costs, and why no fifth type was
  seeded to replace it, is recorded on ADR-0103 rather than repeated here.
- **Decision (a declaration, not a template):** The rule is `{"stem": "...", "bare_first": <bool>}`,
  decoded straight into the `nameMint`
  [ADR-0101](#adr-0101-the-first-of-its-stem-in-a-bucket-carries-no-ordinal-and-the-mint-that-says-so-is-the-one-allocation-tests)
  already made the one place a generated name's shape lives. An empty stem is a **positional** type,
  whose ordinal genuinely is its name (a floor called `1`).

  The alternative, and it was the more consistent-looking one, was the `label_rule` template beside
  it on the same table: the same `text/template` engine, the same allowlist
  ([ADR-0098](#adr-0098-a-label-rule-reads-what-an-entity-is-never-where-it-sits)), reusing a parser
  rather than growing a second dialect. It was rejected on three counts, and the first is decisive.

  **A name has no safe failure.** A label rule that fails to render degrades: the row keeps the pen,
  the read ladder falls through to the entity's name, and nothing is lost. A name has no next rung.
  It is `NOT NULL`, it is in a scoped-unique index, it is the address an operator types, and it is
  what a runbook outside this system stores. A template that renders `Floor-1` for a type whose
  display name is `Floor` produces an illegal name, and the honest options at that point are a failed
  create or a silently different name, neither of which is a degradation.

  **The edit-time refusal is provable for a declaration and only sampleable for a template.**
  [#686](https://github.com/hyperscaleav/omniglass/issues/686)'s acceptance already promised that a
  rule which would render an illegal name is refused when the RULE is edited, not when a location is
  created, and nothing had built it. A declaration's whole output space is `{stem}` and
  `{stem-n : n >= 1}`, so `validateNameRule` mints ordinal 1 and a nine-digit ceiling and checks
  both: every candidate between them shares a character class and has a length between the two, so a
  rule legal at both ends is legal everywhere in it. The ceiling has to exist rather than being
  waved at, because `<stem>-<n>` grows with `n` and the name rule has a length cap: a 97-character
  stem mints legally at 1 and illegally at 100. A template could only have been probed against a
  sample row, which is a different and weaker promise.

  **The expressiveness would have bought nothing here.** A location's data map
  ([ADR-0098](#adr-0098-a-label-rule-reads-what-an-entity-is-never-where-it-sits)) carries `Name` and
  `TypeName`. `Name` is what a name rule is producing, so it is circular; `TypeName` is the type's
  display name, whose slug IS the stem. The one useful template a location could write is the
  declaration, spelled longer. The FuncMap would have diverged too (`slug` and `lower` matter,
  `title` and the acronym dictionary are actively wrong for a kebab name), so "reuse the engine"
  would have meant a second configuration of it, not a second use of it.

  Where a template earns its place is a rule reading facts a declaration cannot name, which is the
  shape a future component or system name rule might have (a vendor, a product, a role). That is a
  reversal to make when there is a fact to read, on the tier that has one, with an argument about
  failure that this tier cannot make.
- **Decision (the rule IS the mint, so nothing restates the shape):** `NameRule.mint()` returns a
  `nameMint` with no reshaping, which carries ADR-0101's guarantee onto this tier for free: the
  allocator tests `mint.name(n)`, the caller mints from the same value, and there is no second
  spelling of the shape to keep in step. Validation mints from it too, so the check and the generator
  cannot disagree about what a rule produces. A stem-less rule normalizes `bare_first` to false,
  since suppressing the ordinal of a name that IS its ordinal would leave nothing: two spellings of
  one rule would compare unequal while minting identically.
- **Decision (a rule change renames nothing, and there is no name-side recompute):**
  [#687](https://github.com/hyperscaleav/omniglass/issues/687)'s own acceptance said a rule change
  renames nothing "except through the explicit recompute" of
  [#685](https://github.com/hyperscaleav/omniglass/issues/685). That verb is the **label** cascade,
  and there is no name-side equivalent. Editing a rule changes no stored fact about any existing row:
  the name, the ordinal and the pen all stand, and the new rule decides how the NEXT nameless create,
  `:resetName`, move or reclassify names a row.

  That is ADR-0100's blast-radius line, which ADR-0101 already applied to a `system_type`'s stem, and
  it is stronger on the name side rather than merely the same. A bulk relabel is recoverable: a label
  is display, the pen says who owns it, and the preview shows the blast radius before the apply. A
  bulk rename is not: it breaks bookmarks, runbooks and integration config outside this system,
  which is the entire reason a rename is its own verb under its own permission
  ([ADR-0088](#adr-0088-a-placement-change-is-an-authorization-act-so-a-move-is-its-own-verb)).
  `:resetName` is the deliberate, one-row-at-a-time way to bring a row onto a new rule, and it is
  gated by `location:rename` because it IS a rename.
- **Decision (a positional type at root is legal):** `allowed_parent_types` can permit a positional
  type at root, where the bucket is every parentless location, so it allocates `1`, `2` across the
  whole estate. That is allowed, and the argument is that the root bucket is not a special case, it
  is a large one. The bucket is the PLACEMENT, never the placement and the type, so two positional
  types under one parent already share an ordinal space; refusing at root would refuse the
  estate-sized instance of a rule that holds everywhere else, and bucket size is not a property the
  platform can police (a building with two hundred floors is the same shape). An operator asks for it
  by making one type both positional and root-placeable.

  The real consequence is not allocation, it is ADDRESSING: a positional type makes a bare name
  ambiguous by design, since every building has a floor `1`. That is answered by machinery this epic
  did not have to build, the dotted address
  ([ADR-0062](#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle)'s addressing and
  `#627`'s placement-scoped uniqueness): `boi.17c.1` resolves where `1` cannot, and an ambiguous bare
  name is already a named refusal listing its candidates rather than a silent first match.
- **Consequence (two limits recorded rather than papered over):** A rule cannot be CLEARED on the
  wire, only replaced, because an omitted JSON key and an explicit `null` both decode to a nil
  pointer and an object field has no third state to read. That is the limit
  `component_type.stem`'s patch already carries (its wire field is `minLength: 1`), and closing it
  means either an untyped `json.RawMessage` field, which costs the generated client its shape, or a
  custom method for one field; both are wire-contract choices rather than this slice's. And the
  shipped `floor` rule reaches NEW estates only: `SeedLocationType` is insert-when-absent, the
  forked-template half of the seed model, so an upgraded estate opts its own `floor` in with one
  `PATCH`. Deliberately no backfill migration, since the rule it would install could not then be
  removed, and a `location_type` row is operator space rather than platform space. Both limits are
  written into the operator guide with the exact call.

  **Amended (#657):** the two limits compose worse than either does alone, which is only visible when
  a shipped rule has to be WITHDRAWN, as ADR-0103's reversal withdraws this one. Removing the line
  reaches new estates only, by the same insert-when-absent argument read backwards, and the estate
  that already has the rule cannot drop it with a `PATCH` either, because the wire has no spelling for
  clearing one. So a `floor` opted in by an earlier release stays opted in until someone writes the
  column directly. The `PATCH` above is still the whole of the opt-IN story; there is no opt-out
  story, and the operator guide now says that rather than leaving it to be discovered.
- **Consequence (a location label rule still cannot read `.Ordinal`, and that is not an oversight):**
  ADR-0098's closed map gains no key here, although this slice gives a location an ordinal. It would
  be redundant where it helps and wrong where it does not: a positional location's name IS its
  ordinal, so `{{.TypeName}} {{.Name}}` already renders `Floor 3`, and the only case a separate key
  serves is a stemmed type with the first ordinal suppressed, where printing `Wing 1` for the only
  wing is the defect the epic was filed about. ADR-0101 declines `.Ordinal` in the shipped SYSTEM
  rule for the same reason.
- **Consequence (the fork primitive does not reach this registry, and does not need to):**
  [#655](https://github.com/hyperscaleav/omniglass/issues/655)'s `registry_shadow`
  ([ADR-0095](#adr-0095-an-operator-forks-a-shipped-registry-row-instead-of-the-platform-writing-it))
  has exactly one adopter, `component_type`, so an operator cannot fork a rule onto an official
  `location_type` row. They do not have to: the shipped location types are seeded
  `official: false` and inserted only when absent, so they are ordinary operator-owned rows the
  registry's own `PATCH` edits, and a restart never reverts the edit. That is the forked-template
  half of the seed model rather than the canonical-catalog half, and it is the same reason this
  registry has needed no fork for `label_rule` either.
- **Consequence (the write paths, derived again):** A location's mint reads exactly two things, the
  type's rule and the parent bucket, which yields create, `:resetName`, the `location_type` half of a
  reclassify, and `:move`. Two of those are worth stating. The reclassify guard is the classification
  **changing**, not the field being present, because `web/src/pages/Locations.tsx` sends
  `location_type` on every save: the defect review caught on the system tier one slice ago
  (ADR-0101) is refused entry here rather than repeated. And `:move` re-mints only when the parent
  actually moves. That was written as NARROWER than the system tier's, where the same re-mint ran on
  a move that changed no bucket, and the reason given for leaving the system arm wide was that
  narrowing it would move an existing expected value. The epic's review pass established that the
  premise was false: systems gained generated names one slice earlier on the same unmerged branch,
  so every expected value at risk had been written by that branch. **Both `:move` arms now re-mint
  only on a bucket change**, and no test expectation moved. The two are spelled differently because
  the kinds do not have the same number of buckets: a location has two and the parent is the whole
  of the distinction, so comparing the parent IS comparing the bucket, while a system has three with
  a parent winning over a location, so its arm compares the `nameScope` bucket the two placements
  resolve to and a parented system relocated to another room keeps its name.
- **Consequence:** `location` gains the nullable `ordinal` the previous slice deliberately withheld
  from it ("a column no writer can fill is a fact waiting to be read wrongly"), and the
  recompute-and-compare invariant now covers all three trees. The bare render stays unwired here for
  a reason of its own: `location_type` carries no `abbrev` at all, so there is no compact form to
  substitute into, and a positional location's name already is its ordinal.
- **Tracked under** [#687](https://github.com/hyperscaleav/omniglass/issues/687), the seventh slice of
  epic [#657](https://github.com/hyperscaleav/omniglass/issues/657).

### ADR-0103: A positional name is allocation order, and the real-world designation is a label

- **Date:** 2026-08-11 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [location types](/guides/admin/location-types/)
- **Context:** [ADR-0102](#adr-0102-a-name-rule-is-a-declaration-a-type-opts-in-with-and-a-rule-change-renames-nothing)
  shipped `floor` as the one auto-nameable location type, positional and stem-less, on the argument
  that "a floor has a name the platform can know". The dev estate is the first real data to meet
  that argument, and it falsifies the sentence as written: the West Building's only seeded floor is
  the building's **Level 2**, and the generator calls it `1`, because a positional ordinal is the
  lowest free number in the parent's bucket and nothing else.
- **Decision:** `floor` stays positional, and the divergence is **kept rather than removed**, because
  the two fields are answering two different questions. A **name** is an address: it has to be
  unique under its parent, legal, and typeable, and the platform owning it means an operator did not
  have to think of one. A **label** is what a human reads, and a building's own designation for a
  floor ("Level 2", "B1", "Mezzanine", the skipped 13) is signage, a real-world fact the platform has
  no access to. So the platform allocates the name and the operator types the label, and the seeded
  estate ships both cases of the same type side by side: the floor under Innovation Hall is named `1`
  and labelled Level 1 (they coincide), and the floor under the West Building is named `1` and
  labelled Level 2 (they do not).
- **Decision (the sharper rule this generalises to):** a stem-less positional name is right where the
  position is an **arbitrary disambiguator** and wrong where the number is a real-world fact. It is
  never a claim about the world, so a type whose ordinal an operator will read as a designation
  (a rack unit, an output, a channel) wants its number **typed**, not allocated. `floor` sits on that
  line rather than safely inside it, and it stays shipped-positional on the strength of the workflow
  it serves: building out a tower floor by floor, where typing forty names is the cost and the
  designations arrive later as labels.
- **Rejected, and why each:**
  - **Make `floor` nominal and drop its rule** (the plain reversal of ADR-0102). It is the more
    honest reading of "a floor has a name the platform can know", and it was rejected on cost rather
    than principle: it leaves the shipped estate with **no** auto-nameable location type, so the
    feature is inert everywhere until an operator defines one, and it moves nine storage cases and an
    e2e that assert against the seeded `floor` rule. The reversal stays available and this entry is
    where it starts. **This is the option the amendment below takes.**
  - **Seed a Level 1 under the West Building so the numbers line up.** It makes the estate agree by
    construction and teaches the wrong lesson: correspondence would look like a guarantee, and it is
    a coincidence of enumerating from the bottom in order.
  - **Ship a `floor` label rule (`Floor {{.Name}}`).** It renders "Floor 1" for a floor that is
    Level 2, which is the same defect with a nicer typeface.
- **Consequence:** the shipped **location** label rule stays empty and the dev estate labels all
  thirteen of its locations by hand, which is not a gap in the demonstration but the demonstration:
  a location's label is operator space by design, and the two floors are where that stops being an
  abstraction. It also means a positional name is not safe to read as a designation anywhere in the
  product, so a console surface that shows a floor must show its label. (The empty half of this is
  superseded by [ADR-0105](#adr-0105-a-rule-reads-a-name-as-words-and-the-location-tier-ships-the-restatement-it-once-refused),
  which ships a global location rule; the operator-space half stands.)
- **Amended (#657), and REVERSED for `floor`:** the architect rejected the divergence on this type,
  taking the option listed as rejected-on-cost above. Two arguments, and the second is the one this
  entry did not have.

  **The allocator's answer misreads as a designation.** A building with floors 0, 1 and 2 is named
  `1`, `2` and `3`, in whatever order somebody seeds them. This entry called that friction worth
  keeping; the architect calls it an invitation to misread the estate, and the invitation is extended
  by the platform rather than by the operator.

  **A floor's designation is not an integer at all.** Real buildings sign B2, LG, G, M, 1, 12A, P3.
  Modelling that as an ordinal is a **category error**, not merely an imprecision, which is what makes
  the reversal obviously right rather than a matter of taste. It also dissolves the objection that
  looks hardest: a negative floor cannot be spelled under `^[a-z0-9][a-z0-9-]*$`, since a leading
  hyphen is refused, but nobody signs a floor `-1`, they sign it `B1`, which is already a legal name.
  A rule shaped like `.Ordinal` could never have reached that name whatever the sign handling.

  So `floor` becomes **nominal**: no `name_rule`, and an operator names it exactly as they name a
  campus, a building and a room. The dev estate's two floors are named `level-2` and `level-1` for the
  designations they actually carry, ADR-0105's shipped location rule renders "Level 2" and "Level 1"
  from those names, and the two pins this entry made load-bearing are **released**: name and label are
  one fact now instead of two that disagreed.
- **Consequence of the reversal (the cost, stated rather than hidden):** of the four seeded location
  types **none** carries a name rule, so **location name generation ships dormant**: correct, tested,
  and demonstrated by nothing in a shipped estate. Seeding a fifth positional type to keep the
  demonstration alive is refused for the reason seeding a Level 1 was, that a demonstration
  constructed to make the feature look used teaches the wrong thing. The generator keeps its coverage
  through a positional type the TESTS create (a parking deck: its number is an arbitrary
  disambiguator, which is exactly what a floor's is not), because a feature losing its tests when its
  last shipped user goes away is how one quietly stops working.
- **Consequence of the reversal (an existing estate keeps the rule, and cannot drop it on the wire):**
  `SeedLocationType` is insert-when-absent, the forked-template half of the seed model, so deleting
  the line un-ships nothing from an estate that already booted with it: its `floor` still carries
  `{"stem": ""}` and still names floors `1`. That is the mirror image of ADR-0102's own consequence
  (the rule reached new estates only), and it is not fixable by a backfill, because a `location_type`
  row is operator space. It is sharper than the shipping direction was, because ADR-0102's second
  recorded limit bites here: a rule **cannot be cleared on the wire**, only replaced, since an omitted
  key and an explicit `null` both decode to a nil pointer. Such an estate reaches the new default only
  by a direct write, and the operator guide says so rather than implying a release does it. Closing
  that needs the wire-contract change ADR-0102 costed and declined.
- **What survives for genuinely positional types:** everything except the placement of `floor`. A
  stem-less positional name is still right where the position is an **arbitrary disambiguator** (a
  parking deck, a rack row, a berth) and wrong where the number is a real-world fact an operator reads
  off a wall; `floor` sat on that line and is now placed firmly on the nominal side of it. The
  name-versus-label distinction stands untouched: a name is an address, a label is what a human reads,
  and the two coinciding on the dev estate's floors is a property of naming them well, not a
  guarantee. A console surface that shows a location still shows its label.
- **Tracked under** [#689](https://github.com/hyperscaleav/omniglass/issues/689), the ninth slice of
  epic [#657](https://github.com/hyperscaleav/omniglass/issues/657); the amendment folded into
  [#698](https://github.com/hyperscaleav/omniglass/pull/698) after the rollup review.

### ADR-0104: A create form shows the name it can know, and never mints one to preview it

- **Date:** 2026-08-11 | **Status:** Accepted | **Pages:** [work with an entity](/guides/operator/entities/),
  [UI](/architecture/ui/)
- **Context:** eight slices of epic [#657](https://github.com/hyperscaleav/omniglass/issues/657) built a
  generator that names a component, a system and a location from what it is and where it sits, and the
  console was the last surface that could not reach it: two of the three create forms derived the name
  from the display name and refused to submit without one, so every console-created row arrived
  operator-named whether the operator meant that or not. The sub-issue asked the form to "produce both
  values without the operator typing either", and the epic's own mechanics say that only half of one of
  them is knowable. A generated name is `<stem>-<n>`: the **stem** falls out of the classification the
  operator has just chosen, and the **ordinal** is the lowest free number among the LIVE siblings in the
  placement bucket, allocated under an advisory lock inside the create's own transaction
  ([ADR-0097](#adr-0097-allocation-tests-the-name-it-would-mint-rather-than-reading-the-ordinal-it-stored)).
  The label is worse: its data map carries the row's own `Name` and `Ordinal`
  ([ADR-0098](#adr-0098-a-label-rule-reads-what-an-entity-is-never-where-it-sits)), so it cannot be
  rendered before the row it describes exists.
- **Decision:** the form shows the **shape** and marks the unknown half as unknown. The stem is resolved
  in the browser from the type registry the picker already loaded, and the ordinal is written as the
  literal token `n`, which is not a digit and cannot be misread as the value: `display-n`, `boardroom`
  for a mint that suppresses its first ordinal, `n` alone for a positional type. Each shape travels with
  the sentence that makes it honest, and for a suppressing mint that sentence is load-bearing rather
  than decorative, since the shown `boardroom` becomes `boardroom-2` for the second one in the room.
- **Decision (the placement is context, not a prefix):** the bucket the name has to be unique in is
  shown beside the field as a **path** (`Unique at Headquarters / West Building / Level 2 / Boardroom`),
  read-only. The sub-issue asked for the ancestry as an editable field's read-only prefix
  (`boi-17c-[editable]`), which is stale by two slices: names became scoped to placement, so a name no
  longer contains its ancestry at all, and rendering the path into the field would put back exactly the
  redundancy the scoping removed.
- **Rejected, and why each:**
  - **A draft-preview verb that mints and rolls back.** It matches the acceptance's wording and buys a
    number that is **provisional**: any other create in the same bucket can take it between the preview
    and the commit, so the form would show a value that silently turns out different, which is the one
    outcome this whole affordance exists to prevent. It is also not free to ask: a rolled-back mint
    takes the same `pg_advisory_xact_lock` on the bucket that real creates take, so a form previewing
    as the operator changes a picker would serialise the estate's creates behind a UI affordance.
    Refused on both counts, and the second is the one that would not have shown up in review.
  - **Re-rendering the label rule in TypeScript.** A second implementation of the rule engine in
    another language, which is the defect the epic's third slice swept 42 hand-rolled copies of onto a
    single primitive to end. The label is therefore not previewed at all: the form says a rule will
    render one and marks it Generated, and shows nothing it cannot know. (Amended by #699 below: the
    refusal stands, and the label is now shown, because the ONE engine renders it.)
  - **Fetching the resolved stem from the server per selection.** It removes the one thing that can
    drift (the browser's walk of the type chain versus the gateway's) at the cost of a round trip per
    picker change and a new route. Declined because the walk is not new here: the console already
    climbs both type registries for the icon, and #688 makes that walk a single primitive
    (`lib/typechain.ts`) with the stem as its third consumer, so there is one client-side climb rather
    than three. What closes the gap instead is a browser-tier e2e that reads the shape the console
    showed and asserts the row lands with that stem, which is the only tier that can see both answers.
    **Amended ([#695](https://github.com/hyperscaleav/omniglass/issues/695)):** this argument is
    spent, and it was always an argument about COUNT rather than about kind. The stem consumer went
    with #702 and the two icon consumers go here, so `lib/typechain.ts` is deleted: there is no
    client-side climb at all, one primitive or otherwise. The registry LISTING carries
    `resolved_icon` beside the raw `icon`, resolved in the gateway over rows the list already holds,
    so it costs no read the console did not already make. Both fields ship because they answer
    different questions: an edit blade posts the raw one back (blank means inherit) and a cell draws
    the resolved one.
- **Consequence:** a name is optional on all three estate create forms, and required only where nothing
  will generate one (an unclassified system, a `location_type` with no name rule, a `component_type`
  chain with no stem). Each of those states shows the missing fact by name rather than a disabled
  button with no explanation. The console can now produce a row the platform owns the name of, which
  every earlier slice of this epic could only be reached from the API or the CLI.
- **Amendment (#699, 2026-08-11): a render is not a mint, and the distinction is what this decision
  turned on without naming.** Both refusals above are about ALLOCATING. The provisional-answer
  argument is that a minted ordinal can be taken by another create before the commit; the
  serialisation argument is that a rolled-back mint takes the bucket's `pg_advisory_xact_lock`.
  Neither reaches an operation that allocates nothing: resolve the rule through the same tiers, build
  the same closed data map, execute it with the same one engine, and write the token `n` where the
  ordinal would go. No lock, no write transaction, no allocation, and no second implementation of
  anything. That operation is `POST /<collection>:renderLabel`, and the create form now shows both
  generated values in **locked fields** rather than showing a shape and a promise. The
  no-allocation claim is held by a test that reads back every SQL statement the render issued (#650's
  counting instrument) and by a create five renders later still taking ordinal 1, rather than by this
  paragraph.
  - The TypeScript refusal is **unchanged and is what makes this shape the only one available**: the
    label is shown because the Go engine rendered it, not because the browser learned to.
  - The NAME's shape stays client-side, so the two halves of the form now answer from different tiers.
    That is a deliberate trade rather than an oversight: a stem resolves synchronously from a registry
    the picker has already loaded, so the locked name appears the instant a picker moves, where a
    round trip would leave the form's primary affordance empty until it lands. The cost is the drift
    ADR-0104 already accepted, and the round-trip objection to resolving the stem server-side is now
    void, since the form makes a round trip per picker change regardless. Folding the name into the
    same answer is therefore available and cheap, and is deliberately not taken here.
  - **The gate is the entity's own `:create`,** the permission the create it precedes needs: the
    answer exists to be acted on, and an operator who cannot create has no use for it. The PLACEMENT
    refs are a separate question and resolve within the caller's `location:read` and `system:read`
    scopes, because the rendered string can carry a location's label and a system type's label, so
    without that the route is a disclosure channel. A location draft injects no scope at all, because
    a location's data map reads no other estate row (ADR-0098's exclusion survives on that tier).
    **Amended (#713):** the component's `system` ref resolves in `system:read` **and**
    `system:update`, and the route carries the create's conditional `system:update` permission,
    because the create binds that reference as a membership (ADR-0107). The rule is no longer "the
    placement resolves in a read scope" but "each placement reference resolves exactly where the
    create resolves it", which is the only version of it a preview and its create cannot disagree
    about.
  - **Three states per field, not two,** and the third is not a loading state. Generation is
    unavailable, permanently, for a component_type chain with no stem, a system with no system_type,
    a system_type chain with no stem, and a location_type with no name rule; the label has its own,
    where no rule resolves at any tier, and the field then shows the NAME (the read ladder's third
    rung) rather than sitting locked and empty. That state was every location in a fresh estate when
    this was written, because no location label rule shipped at any tier; a global one ships as of
    [ADR-0105](#adr-0105-a-rule-reads-a-name-as-words-and-the-location-tier-ships-the-restatement-it-once-refused),
    so it is now the state an operator reaches by clearing the rule rather than the state they start in.
- **Amendment (#657, 2026-08-11): the lock is an inline action, and a locked field is `readonly`
  rather than `disabled`.** The affordance moved INSIDE the field, a square icon button in the
  field's daisyUI join, which is where the console already puts an in-field action (a Variables row's
  set / revert, a secret's reveal, a setting's Restore to default). Both actions read as that
  Settings row does, on purpose: handing a field back to the platform IS restoring it to its default,
  and one idea should not carry two visual languages. The button has no text at all, so its tooltip
  ("Override", "Restore to default") is the visible copy and the accessible name says which field.
  - **`disabled` had to go, and that is the substance of this amendment.** A disabled input fires no
    click, so click-to-override cannot work on one, and it is out of the tab order, so the value the
    row is about to carry has no keyboard path. `readonly` is not editable, is still focusable, and
    still fires events. The consequence is that the locked LOOK is now drawn rather than inherited:
    daisyUI 5 ships **no `.input-disabled` class**, only `:disabled` / `[disabled]` selectors, so the
    class the markup carried was a no-op and the whole locked appearance came from the attribute.
    `.input-locked` in `app.css` carries it, and hover only warms the border.
  - **Focus does NOT take the pen**, although a click does. A locked field is a tab stop by the
    decision above, so claiming on focus would mean tabbing from the pickers to the Create button
    claimed both pens and blanked both fields on the way past, which is the state the locking exists
    to prevent. Clicking is a deliberate act; passing through is not. The click accelerator is
    one-way for the same reason: the way back discards what the operator typed and belongs on the
    button. The always-present button is what makes every state keyboard-reachable, and the section's
    tab order is asserted rather than inspected.
- **Amendment (#702, 2026-08-12): reading the lowest free ordinal is not minting one, so the form
  shows the real number and the create carries a precondition.** This decision's two refusals were
  both about ALLOCATING, and #699 already applied that distinction to the label render. The same
  distinction reaches the ordinal itself, which this decision missed: the number a create will take
  is `pickOrdinal` over the sibling names in the placement bucket, and running that computation is a
  read. It takes no `pg_advisory_xact_lock`, opens no write transaction and allocates nothing, so the
  serialisation argument, the one that "would not have shown up in review", does not reach it. The
  form therefore shows `display-3`, not `display-n`, and the token is retired from the codebase
  (`OrdinalToken`, `nameMint.shape` and the console's `ORDINAL_TOKEN` are all gone rather than left
  unused).
  - **The provisional-answer argument stands, and is answered rather than avoided.** Another create
    in the same bucket really can take the number in between, and hiding the number was one way to
    cope with that; refusing is the better one. The form posts the ordinal it was shown as
    `expected_ordinal` on the create, the create allocates under its own lock exactly as it always
    did, and `confirmOrdinal` compares the two inside that transaction before anything is written. A
    create that would land a different name is a **409** naming the number that moved and the name it
    mints; the form re-reads the draft, shows the new name, and the operator submits again. A refusal
    is honest where a silent difference is not, and a silent difference is precisely what the locked
    field exists to prevent.
  - **It is a number, never a name.** A locked field that posted a name would claim the pen and set
    `name_generated` false, inverting the whole affordance, so the precondition is the ordinal and the
    API refuses it (422) beside a supplied name, where nothing is allocated for it to be about. It is
    a pointer with `minimum: 1` so absent and zero cannot both spell "no expectation", and an
    operator-named draft answers with no ordinal at all, which is what makes "the field is locked" and
    "there is a precondition to post" one fact rather than two.
  - **The form has to tell this refusal from every other one,** because its recovery (re-read,
    re-show, resubmit) is specific to it and a create's OTHER 409 is a name collision the operator has
    to resolve themselves. Matching on the message would tie the console to server copy, so both
    refusals a form acts on carry a `huma.ErrorDetail` locating them on the request field the recovery
    touches: `body.expected_ordinal` for the conflict, carrying the ordinal that moved as its value,
    and `body.name` for the four "the platform will not name this row" refusals. That is the RFC 9457
    `errors` array the `ErrorModel` already publishes, so it costs no new wire shape.
  - **The name's shape stops being client-side, which #699 explicitly left available.** That amendment
    called folding the name into the same answer "available and cheap, and deliberately not taken
    here", on the grounds that a stem resolves synchronously from a loaded registry. It is taken now:
    the draft already resolves the stem, the mint and the bucket to render the label, so returning the
    NAME costs nothing, and the form makes a round trip per picker change regardless. The cost it
    removes is the drift this decision accepted, which is the naming half of
    [#695](https://github.com/hyperscaleav/omniglass/issues/695). The consequence is that "the platform
    will not name this row" is now an ANSWER from the server rather than a null the browser computed,
    so the field's three states are unchanged but their source is one tier instead of two, and the
    submit gate waits for that answer rather than flipping synchronously.
  - **The draft takes the same scopes its create takes, in the same order,** because it now resolves
    the same references: the parent decides which bucket the ordinal is read from, so it resolves in
    the caller's `<kind>:create` scope, the set the create resolves it in. A location's draft
    therefore takes a scope where it took none, and what that scope guards is the sibling read rather
    than the render (ADR-0098's exclusion of placement from a location's label data map is untouched).
    The draft does not rehearse the create's own all-scope gate on a ROOT create: it answers what a row
    would be called, not whether the create is permitted, and the permission is the route's gate.

    **Amended (#702 review), and that last sentence is REVERSED.** The draft refuses the parentless
    bucket in the same set the create refuses it, because the answer is not inert: it carries the
    lowest free ordinal, read from the bucket's sibling NAMES, so it reports which of that bucket's
    names are taken. On the root bucket the probe is chosen rather than fixed, since writing a stem
    into a forked type's name rule is ordinary
    ([#703](https://github.com/hyperscaleav/omniglass/issues/703)) and needs only
    `location_type:create`. Driven, the root draft answered `secret-region-2` to a principal whose
    create in that bucket is a 403. The gate is a SCOPE, so it sits beside the route's permission gate
    rather than contradicting it, and it is unconditional rather than only when a name is being
    drafted, for the same reason the create's gate is.
  - **Amended (#702 review): the precondition binds the NAME, not the ordinal.** The claim a locked
    field makes is "the row will be called what I am displaying", and the ordinal is one of three
    inputs to that rather than the claim itself: a name is the stem, the suppression rule and the
    number together, so a claim on the number alone passes unchanged while the other two move. Driven:
    a form drafts `display-1`, `PATCH /component-types/display {"stem":"monitor"}` lands, the create
    posts `expected_ordinal: 1`, and the row is created as `monitor-1` with the precondition MET,
    which is exactly the silent difference the locked field exists to prevent. Nothing races there,
    and forking a type and rewriting what it mints is an ordinary operator act since #703, so the
    console was shielded only by an accident (the stem is not in the draft body, so it is not in the
    query key that would have re-asked) and a CLI or API caller was not shielded at all. `expected_name`
    replaces `expected_ordinal`, and `confirmDraftedName` compares the string. Keeping the number and
    adding the stem and the bucket beside it was refused: it is three fields where one already carries
    all three by construction, and it would need extending again the next time a mint grows an input.
    **It is still a precondition and not the name field:** the create leaves `name` empty, so the row
    is still `name_generated` and the ordinal column is still the platform's, and posting it beside a
    supplied name is the same 422 as before. The 409's `ErrorDetail` moves to `body.expected_name` and
    carries the name the create WOULD have produced as its value, which is what the form shows next.
    The drafted `ordinal` stays on the render's answer as an informational fact, since a surface that
    shows where a value came from teaches the mechanism it operates.
- **Amendment (#693, 2026-08-12): the lock is the console's ONE vocabulary for the pen, so it reaches
  the edit blade and the list's chip retires.** This decision described a create form, and the pen's
  other half was left where it had grown: a full-text `Generated` chip in the identity cell, read by
  18 flat-list pages and every tree. Two things were wrong with that and only one is cosmetic. It
  charged the Name column the width of the word on every platform-labelled row, on the very column
  [#690](https://github.com/hyperscaleav/omniglass/issues/690) had just had to put a floor under. And
  it stated an ownership fact where the operator could not act on it, while the surface that CAN act
  on it, the edit blade, said nothing at all. So `PenToggle` moves out of the create form into its own
  module and the blade's display-name field consumes it, and the chip goes from both list renderers.
  - **The name's own chip stays, and that is the consistency rather than an exception.** The component
    blade marks a platform-picked NAME in its read state, beside the name fact. The rule both now
    follow is that a pen states itself beside the field it owns, on the surface where the operator can
    change it; a list is neither.
  - **The blade has a third state the create form does not,** and it is a locked field showing a value
    that is about to change: "Restore to default" on a row an operator labelled by hand. Nothing can
    know what the rule will render for it, because `:renderLabel` previews a row that does not exist
    and reads the lowest FREE ordinal in the bucket, which for an existing row is the NEXT sibling's
    number. Asking it would answer confidently and wrongly. The field therefore keeps showing the
    label that is there and the hint says the platform rewrites it on save. A lock over an EMPTY field
    was refused here for the same reason it was refused on the create form.
  - **It closes a silent pen theft, which is why this is a fix and not a restyle.** Every blade seeded
    a plain signal from `display_name` and posted `display() || undefined`, so opening the pencil on a
    platform-labelled row and saving ANY other field posted the platform's own rendering back as an
    override. `labelPen` read that as the operator claiming the label, the row stopped following its
    rule, and nothing on screen had said so. The field now posts the PEN's value, which is the empty
    string while it is locked, and the API reads that as "still the platform's". One expression covers
    the no-op and the hand-back, and the hand-back is the console's first way back at all.
  - **`labelGenerated` retires with the chip.** It asked "is there a rendered label here to mark",
    which is a marker's question and returns false for a row whose rule rendered nothing; a field asks
    "who holds the pen", which is true for that same row and must open it locked. One predicate
    answering two questions differently is how a surface marks one thing and does another.
- **Tracked under** [#688](https://github.com/hyperscaleav/omniglass/issues/688), the eighth slice of
  epic [#657](https://github.com/hyperscaleav/omniglass/issues/657), amended by
  [#699](https://github.com/hyperscaleav/omniglass/issues/699), its tenth, by the affordance pass
  folded into [#698](https://github.com/hyperscaleav/omniglass/pull/698), by
  [#702](https://github.com/hyperscaleav/omniglass/issues/702), and by
  [#693](https://github.com/hyperscaleav/omniglass/issues/693).

### ADR-0105: A rule reads a name as words, and the location tier ships the restatement it once refused

- **Date:** 2026-08-11 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [find things in your estate](/guides/operator/inventory/), [location types](/guides/admin/location-types/)
- **Context:** epic [#657](https://github.com/hyperscaleav/omniglass/issues/657) shipped a label engine
  ([ADR-0098](#adr-0098-a-label-rule-reads-what-an-entity-is-never-where-it-sits)) and an operator-owned
  acronym dictionary ([ADR-0099](#adr-0099-the-acronym-list-is-one-replaceable-setting-not-a-shipped-list-plus-operator-additions))
  that nothing could reach. `title` upper-cases each word and leaves the SEPARATOR standing, so
  `{{title .Name}}` renders `north-wing` as "North-Wing"; the closed FuncMap's other three run the
  other way (`slug`) or ignore word boundaries entirely (`upper`, `lower`). There was therefore no rule
  an operator could write that turned a kebab NAME into the words in it, and "HQ West" from `hq-west`
  was unreachable by any spelling. It bit hardest on locations, whose global rule shipped deliberately
  empty, so every location in a shipped estate fell to the read ladder's last rung and read its raw
  name.
- **Decision (the function):** `words` joins the closed FuncMap: a run of `-` or `_` becomes one space,
  a leading or trailing run is dropped rather than becoming an edge space, and every other character,
  including whitespace the fact already carried, is untouched. It is deliberately not a normalizer and
  deliberately not `wordRe`'s "anything that is not a letter or a digit": a catalog display name's
  parentheses and slashes are punctuation somebody chose, and only the two separators a NAME is built
  from are spent. Adding a function is a **three-place act** by design (the FuncMap, the AST allowlist,
  and `FuncNames`), and two of the three is silent in both directions, so the published set is now
  WALKED by a test that parses each name in a real rule rather than described by a comment.
  **Amended ([#701](https://github.com/hyperscaleav/omniglass/issues/701)):** the three-place act is
  ONE place, and the "by design" was wrong about which design. What the property needs is that a
  function be in the FuncMap AND the grammar, not that a person type it into both: the FuncMap,
  `FuncNames` and the allowlist now all derive from a single ordered declaration, so two-of-three is
  unwritable rather than merely tested for, and the fourth copy this entry did not count (the prose
  that teaches the language) becomes a table rendered from that same declaration. Adding a function
  stays a deliberate, test-visible act, because the closed-set test still names the members and the
  generated artifact still has to be regenerated and committed. The walking test stands and is joined
  by its converse, since a name admitted by the grammar with no implementation behind it was the
  direction nothing checked.
- **Decision (the location rule):** the global location rule ships as `{{title (words .Name)}}`, written
  to `default_template`, the boot-seed-owned column an operator's `template` sits beside untouched. The
  seed's own comment argued against exactly this and is rewritten rather than deleted: it said any rule
  at that tier is "either a constant or a restatement", and the constant half stands (labelling every
  room "Room" is worse than the name it would replace) while the restatement half was true only while a
  restatement could **echo**. This one re-cases the name and runs the operator's dictionary over it, so
  it produces a string the fallback cannot, which is the test a rule at this tier has to pass.
- **What this does NOT change:** the read ladder's last rung is still **verbatim**. A row with no stored
  label renders its `name` exactly, never a prettified version, and the difference is that this rule
  RENDERS AND STORES a label rather than teaching a read path to guess one. That distinction is
  [#613](https://github.com/hyperscaleav/omniglass/issues/613)'s, corrected the same day for having the
  same confusion in it.
- **Consequence (the upgrade is not automatic, and says so):** a shipped rule change restamps nothing by
  itself ([ADR-0100](#adr-0100-a-label-cascades-where-the-blast-radius-is-a-placement-and-waits-for-the-verb-where-it-is-the-estate),
  [ADR-0102](#adr-0102-a-name-rule-is-a-declaration-a-type-opts-in-with-and-a-rule-change-renames-nothing)):
  the blast radius is the whole estate, so it waits for the verb. A new estate gets a rendered label at
  create; an existing one keeps its raw kebab locations until an operator runs
  `/locations:recomputeLabels`, and the operator docs say that plainly rather than implying a release
  does it for them.
- **Consequence (the estate stops masking the rule):** [#689](https://github.com/hyperscaleav/omniglass/issues/689)
  pinned a `display_name` on all thirteen dev-estate locations, because nothing would have rendered one.
  Four of those pins now restate what the rule renders, so they are released and the RENDERED values
  pinned in their place; nine survive with a reason each, ADR-0103's two floors among them, since a
  positional name is allocation order and releasing those deletes that worked example. **Amended
  (#657):** ADR-0103 was then reversed for `floor`, so those two floors are named `level-2` and
  `level-1` for their designations, this rule renders "Level 2" and "Level 1" from those names, and
  their pins are released too. Six rendered values are pinned in place of released ones and **seven**
  survive. The media lab's
  name becomes `media-lab`: every other location name in the estate is one word, so without it nothing
  the console shows demonstrates a separator becoming a space.
- **Known gap, deliberately not closed here:** `HQ` is not in the shipped acronym list, so `hq-west`
  renders "Hq West" out of the box and needs one operator edit to read "HQ West". Adding it is a change
  to a settings DEFAULT rather than to this rule, it ripples through three generated artifacts, and
  ADR-0099's replace-not-merge semantics mean a shipped entry is not free. Left as a follow-up so this
  slice stays what it says it is.
- **Tracked under** epic [#657](https://github.com/hyperscaleav/omniglass/issues/657), folded into
  [#698](https://github.com/hyperscaleav/omniglass/pull/698) after the rollup review.

### ADR-0106: A location type is platform-owned, and a nullable object clears under the mask

- **Date:** 2026-08-12 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/),
  [core entities](/architecture/core-entities/), [API](/architecture/api/),
  [location types](/guides/admin/location-types/)
- **Context:** the boot seed's contract is documented as an authoritative upsert, and
  `SeedLocationType` was `on conflict (name) do nothing`. A shipped default was therefore a one-way
  ratchet: adding one reached every estate, removing one reached only new installs.
  [#657](https://github.com/hyperscaleav/omniglass/issues/657) hit it live, shipping a `name_rule` on
  `floor` and then reversing that in
  [ADR-0103](#adr-0103-a-positional-name-is-allocation-order-and-the-real-world-designation-is-a-label),
  at which point the withdrawn rule could not be taken back out of an estate that had booted in
  between, and no `PATCH` could clear one either. The do-nothing seed was not an oversight: shipped
  location types seeded `official: false`, so the rows belonged to the operator and an authoritative
  re-seed would have stomped their edits. The ratchet was the symptom; the ownership model was the
  cause.
- **Decision (ownership):** `location_type` adopts the registry-fork primitive
  ([ADR-0095](#adr-0095-an-operator-forks-a-shipped-registry-row-instead-of-the-platform-writing-it)),
  the second registry to do so after `component_type`, rather than growing a third mechanism beside
  it. Shipped rows seed `official: true` and the platform owns them, so the seed writes them with
  `ON CONFLICT DO UPDATE` and a withdrawal is just another write. An operator still shapes their own
  place vocabulary: an edit of a shipped type stores their whole version of its mutable columns as a
  shadow keyed on the row's own uuid, every read resolves the shadow over the row, and
  `POST /location-types/{id}:restore` discards it. One uuid and one name per logical row either way,
  so no foreign key, no URL and no audit row learns about the fork. The rejected alternative was the
  `default_template` / `template` column pair the global label rules use, which is right for a table
  of three rows with no operator-created siblings and wrong here: it doubles every future column on a
  registry that already has five mutable ones, and it answers only the columns somebody thought to
  split.
- **Decision (what the image carries):** the fork's whole-row rule is per registry, not per column
  name. `allowed_parent_types` is IN the image although it names other types, because a
  `location_type` is flat: it has no parent link, and the placement constraint is a fact the row
  states about itself, one an operator legitimately reshapes. Holding it official would leave a fork
  unable to say the thing the registry exists to say.
- **Decision (the migration's discriminator):** estates already carry operator edits on the shipped
  rows themselves, so a one-time backfill moves them into shadows before the flip. Telling an edit
  from a shipped value is the whole difficulty, and it is NOT done by comparing the row against what
  this release ships. That comparison cannot see the case this ADR exists for: a row holding a value a
  previous release shipped and this one withdrew differs from today's YAML and reads exactly like an
  edit, so preserving it would defeat the withdrawal, and a row edited to a value a later release
  happens to ship has the mirror problem. The **audit trail** decides instead, because it is a record
  rather than an inference: every operator write of a registry row writes its audit row in the same
  transaction, so a shipped row with no `create` and no `update` of its own has never been touched and
  whatever it holds is what some release shipped. `create` counts as well as `update` for a case that
  is reachable today, an operator deleting a shipped type and creating their own under the same name.
  The known imprecision is biased toward preserving: a contract write audits against `location_type`
  too, so one addressed by uuid looks like an edit here and leaves the row holding a shadow of the
  values it already had, which reads identically and costs a `:restore`. The reverse mistake,
  reverting an edit, is not recoverable from the console at all.

  **Amended (#703), and the discriminator is REMOVED.** Everything above answers "how do you tell an
  operator's edit from a shipped value", and the architect's ruling of 2026-08-12 is that there is no
  such edit to tell apart: Omniglass has cut no release and holds no operator data, so no upgraded
  estate exists and the capture was preserving rows that do not. A review then found the
  discriminator wrong in two ways that both lose data silently, and its `create` leg exercised by
  nothing, which is what a premise that never occurs does to the code written for it. The backfill is
  now one statement, `set official = true` on the four shipped names, and its test asserts the
  migration writes **no** shadow, so the machinery cannot grow back quietly as a fix for a problem
  that was ruled out of existence. **This is correct only while that premise holds.** The first real
  release creates estates carrying operator data, and from then on a change of ownership on a shipped
  row owes those estates an answer to the question this bullet asks, in a NEW migration reasoning
  about their rows: this one has already run, and it reasons about nothing. Whoever ships that
  release should read the paragraphs above as the standing argument, still sound; only the machinery
  went away.
- **Decision (the wire):** a nullable OBJECT field clears by being named in `update_mask` with no
  value, reusing the primitive from
  [ADR-0091](#adr-0091-an-update_mask-says-which-fields-a-patch-writes) rather than inventing a
  per-type spelling. The house three-state string sentinel does not generalize: `""` clears a string
  because a string has an empty value to overload, and an object has none (`{}` is a rule with default
  fields, not the absence of one). An explicit `null` cannot carry the intent either, since it decodes
  to the same nil as an omitted key. **This is the convention for every nullable object field that
  follows**, so it is recorded on the API page and not only in the handler.
- **Consequence (a capability that had to be defended):** flipping the shipped rows to `official`
  activates a guard that had been dormant on this registry, since no `location_type` row was official
  before. `SetLocationTypeProperty` and its three siblings would have started refusing the four types
  an estate actually uses, silently withdrawing the contract editor for them. They no longer consult
  the official flag: a contract line is a row in its own table rather than a column of the registry
  row the fork covers, and **nothing seeds one**, so every line in those tables is an operator's
  already. An ownership change may not take a working capability away as a side effect.
- **Consequence (an expectation inverts):** `internal/seed/seed_test.go` asserted "official
  location_types = 0, a shipped location type is operator-owned". It now asserts 4 and guards the
  opposite claim. The same inversion lands on the wire test and on the console, where origin reads
  three ways (shipped, yours, overridden) exactly as it does on component types, and the blade's
  destructive slot offers **Restore shipped** on a forked row.
- **What this does NOT change:** the `label_rule` table's `default_template` / `template` pair stays
  as it is ([ADR-0098](#adr-0098-a-label-rule-reads-what-an-entity-is-never-where-it-sits)). It is one
  row per labelled entity kind with no operator-created rows, where a per-column split is the natural
  fit and an overlay table would be more machinery than three rows are worth. A shipped row still
  cannot be DELETED, forked or not, and an operator-created location type is untouched by any of
  this: it is written in place, it never carries a shadow, and `:restore` has nothing to give it back.
- **Tracked under** [#703](https://github.com/hyperscaleav/omniglass/issues/703) and
  [#692](https://github.com/hyperscaleav/omniglass/issues/692).

### ADR-0107: A create that writes a membership costs what the membership route costs

- **Date:** 2026-08-12 | **Status:** Accepted | **Pages:**
  [identity and access](/architecture/identity-access/), [API](/architecture/api/),
  [core entities](/architecture/core-entities/)
- **Context:** `POST /components` accepts a `system` reference, and what it does with it is insert the
  component's **primary membership** into `system_member`. That is the same row
  `PUT /systems/{name}/members/{component}` writes, and that route is gated on the `system:update`
  permission and resolves its system in the `system:update` scope. The create asked for neither: the
  reference resolved existence-only until
  [#700](https://github.com/hyperscaleav/omniglass/issues/700) made it resolve in `system:read`, and
  the route required `component:create` alone. A principal holding `component:create` and no system
  permission at all could therefore write a membership through the create that the membership route
  refused it, which makes the gate on the membership route decorative rather than binding.
- **Decision:** the create's `system` reference resolves in the caller's **`system:update` scope**,
  and the route requires the **`system:update` permission** when the reference is present. Two paths
  that write one row cost one permission. The scope half is not implied by the permission half and is
  the one a test has to drive separately: a principal holding `system:update` on one system and the
  all-scoped viewer floor beside it may READ every system in the estate, and before this could bind
  any of them on a create.
- **Decision (where the check lives):** in the handler, not the middleware, because the condition is a
  **body field** and Huma's operation middleware cannot see the body. To keep the generated spec a
  faithful map of what a route enforces rather than of what it always enforces, the second permission
  is **published** as an `x-omniglass-conditional-permission` extension beside the primary
  `x-omniglass-permission` stamp, exactly as a platform-tier write publishes
  `x-omniglass-platform-permission`, and it joins the route-derived permission universe the roles view
  and the docs lint both read. Reusing the existing stamp mechanism was preferred to a hand-written
  note in prose, which is the drift the generate-first rule exists to prevent.
- **Consequence (accepted, and the reason it is not a bug):** `operator` holds
  `component:create,update,rename,move` and **no `system:*` permission at all**; `deploy` holds
  `system:create,update,rename,move` beside the same component set. So an operator can no longer
  create a component INTO a system. That is the role line the seed already draws rather than damage
  from this change: an operator maintains components, a deploy tech builds out systems and their
  membership. The alternative, granting `operator` the `system:update` permission, was refused as a
  much larger grant than "may bind a membership while creating a component": it would also let an
  operator edit systems generally, which is exactly the line the two roles are drawn along. The
  recovery needs no grant at all, since creating the component and binding it are separately
  authorized acts: the operator creates it, and whoever holds the permission adds it after.
- **Consequence (the narrowing has to be met before the form is filled in):** a live narrowing
  discovered as a 403 after an operator has filled in a create form is the outcome
  [#699](https://github.com/hyperscaleav/omniglass/issues/699)'s rule exists to prevent, so the
  console does not offer the system picker to a principal that cannot use it, and the slot explains
  itself and names the permission rather than vanishing. The API's refusal names `system:update` too,
  which is what the CLI prints, because a refusal an operator cannot act on sends them to an
  administrator with no ask.

  **Amended (#707 review): that claim was FALSE as shipped, on the layer this decision itself calls
  out.** The console gate read the `system:update` **permission** and nothing else, while
  authorization is two layers and this decision moved BOTH. A principal holding the permission over
  no system at all was offered the picker, filled the form in, and was refused on submit, which is
  the exact outcome the paragraph promised to prevent. That principal is not exotic: a
  location-scoped `deploy` grant resolves `system:update` to the empty set, because
  `applicableKinds("system")` is `{"system"}` alone and the cross-tier expansion is unbuilt
  ([#10](https://github.com/hyperscaleav/omniglass/issues/10)), and devseed ships one as
  `tech-east`. The gate now also requires a system carrying the `update` **action**, the server's own
  per-row answer computed from the same per-action scope the gateway enforces, offers only those
  systems, and names whichever of the two layers is missing. The console filters data rather than
  resolving a scope in the browser, which it must not do.

  **Amended (#707 review): the API's refusal denied the row's existence.** The bind resolved in the
  `system:update` scope alone, so a system outside it came back as the non-disclosing
  `ErrSystemNotFound` and the route answered "system not found" (422) for a row the same caller could
  `GET`. The bind now takes `system:read` as well: update still decides whether it happens, read
  decides what the refusal may say. Out of the read scope is the same non-disclosing 422 as before,
  so nothing new is disclosed; inside it, the refusal is a **403 naming the scope**, which discloses
  nothing either, since that caller can read the row.
- **What this does NOT change:** the LOCATION reference on the same create still resolves in
  `location:read`, because a location is read and rendered into the label rather than written
  ([ADR-0100](#adr-0100-a-label-cascades-where-the-blast-radius-is-a-placement-and-waits-for-the-verb-where-it-is-the-estate),
  #700). The two references are deliberately not symmetric: what decides the action a placement
  reference resolves for is what the write DOES with it.

  **Amended (#713): the residual this entry left open is closed.** As shipped, the `:renderLabel`
  draft kept `system:read` on the reasoning that it renders a preview and writes nothing, and the
  entry accepted that a preview could therefore be served for a create the platform would refuse,
  with the console's own gate as the thing that kept the two in step. That is the caller's good
  behaviour standing in for the platform's, and it is also wrong about what the preview contains: the
  rendered string carries the system's TYPE label, so the lenient resolve hands back a fact about a
  row the caller may not bind, one tier past the disclosure `location:read` closes on the other
  reference. The draft now resolves its `system` through the create's own resolver, in
  `system:read` **and** `system:update`, and carries the create's conditional permission, so both
  halves of the gate rehearse and the refusals are the same sentinels in the same order. The
  LOCATION reference is unchanged on both routes, and remains `location:read` on both, for the reason
  above: it is read and rendered, never written.
- **Tracked under** [#707](https://github.com/hyperscaleav/omniglass/issues/707),
  [#713](https://github.com/hyperscaleav/omniglass/issues/713).

### ADR-0108: Settlement reads one clock, and a zero window is a statement of intent

- **Date:** 2026-08-12 | **Status:** Accepted | **Pages:** [commands](/architecture/commands/)
- **Context:** `Settle` compares a sample's `ts` against a `now` its caller supplies. Every `ts` on
  the telemetry tables is `timestamp with time zone default now()`, so that end of the comparison is
  written by **Postgres**, while `now` was `time.Now()` in the **server** process. Two clocks, one
  comparison. At `settle_window_seconds: 0` the test reduces to `now.Sub(intended.TS) < 0`, true
  exactly when the sample is stamped ahead of the host, so the verdict for the setting that most
  wants to be decisive was decided by skew. `TestCommandIssueAPI` failed intermittently on precisely
  that branch, and the operator-facing version is worse than a flake: a deployment whose database sits
  on another host gets the same coin flip on every zero-window command, and a command that genuinely
  failed can be reported `pending` and never settle.
- **Decision (the clock):** the comparison takes `now` from the **database**, read with `select now()`
  inside the caller's transaction (`dbNow`), at both settle sites. `now()` is
  `transaction_timestamp`, which is the point rather than an incidental detail: a settle-check running
  in the transaction that just opened the intended value reads exactly the timestamp that row was
  stamped with, so the two ends of the comparison are one reading of one clock rather than two
  readings that happen to be close. A check in a later transaction reads a strictly later timestamp
  from the same server, so elapsed time is elapsed time and never skew.

  **Amended (#718): that last sentence over-claims, and the code comment repeating it has been
  corrected.** `now()` is `transaction_timestamp` and both settle paths run at READ COMMITTED, so an
  `IssueCommand` that BEGAN after a settle-check's transaction did can still commit before one of its
  statements takes a snapshot; the intended row it then reads carries a `ts` later than that
  transaction's `now()`, and the delta is negative. No verdict is wrong today, and not by luck: a
  negative delta is less than any positive window, which is `pending`, the right answer for a command
  issued a moment ago, and the zero case never consults a timestamp at all. What is corrected is the
  claim, not the behaviour, because an invariant an isolation level does not provide is one a future
  change can lean on. `settled_at` is stamped with
  the same value, which also puts it in the currency the insert path's own `now()` already uses.
- **Decision (the zero window):** a window of zero is **terminal by construction**, checked before any
  arithmetic. `settle_window_seconds: 0` is the documented way to say "settle immediately", which is a
  claim about **intent** rather than about elapsed time, so no timestamp of any provenance may make it
  `pending`. Taking the clock from the database is what makes every other window honest; this is what
  makes the zero case independent of the clock rather than merely agreeing with it, and it is the half
  that survives a future caller that supplies `now` from somewhere else.
- **What was refused:** **stamping the sample from Go** on the write path, which is the other way to
  get one clock. It inverts the ripple: `ts` defaults to `now()` across every telemetry table and other
  readers rely on database time ordering, so moving the authority to the process would touch far more
  than settlement to fix a defect that lives in settlement. Also refused: **widening the comparison
  with a tolerance**. A tolerance makes the test stop failing without making the behavior stop
  depending on skew, which is the failure this repo has paid for twice.
- **Consequence (cost, stated):** one extra round trip on each of the two settle paths, `IssueCommand`
  and `CommandSettlement`. Both already run several statements inside one transaction, so it is a small
  fraction of either, and it is the price of the comparison being about time rather than about which
  host answered. `Settle` stays **pure** and still takes `now` as an argument; what changed is who
  supplies it, which is the repo's own rule about the clock being an edge concern pushed out of the
  core.
- **Consequence (a shape that was already true becomes stated):** nothing in the catalog ships a zero
  window with a target today (`reboot` is fire-and-forget), so the change moves no seeded behavior. It
  moves what an operator's own zero-window `command_type` does, from "terminal if the clocks happen to
  cooperate" to "terminal".
- **Extended ([#719](https://github.com/hyperscaleav/omniglass/issues/719)): the same principle, a
  different mechanism, and no round trip.** The mixed comparison survived on the history reads: a
  system's and a location's health transitions, a component's property transitions, a component's
  events, and a component's and a node's logs each built `time.Now().UTC().Add(-window)` in the server
  process and filtered a `ts` the database stamped. Less urgent than settlement, because a
  multi-minute window absorbs ordinary skew where a zero window had no margin at all, and still a
  boundary decided by two clocks with nothing bounding the difference, which is why the same read
  answers differently against a database on another host.

  The round trip `dbNow` costs was weighed rather than assumed, and refused: these are READ paths, and
  the arc that precedes this one
  ([#653](https://github.com/hyperscaleav/omniglass/issues/653),
  [#725](https://github.com/hyperscaleav/omniglass/issues/725),
  [#726](https://github.com/hyperscaleav/omniglass/issues/726)) spent itself taking round trips OFF
  them. It did not have to be paid, because the dilemma the issue poses (pay a round trip, or document
  the mixed comparison) has a third answer the settle path did not have. `Settle` compares in Go, so
  it needs `now` as a VALUE and something has to fetch it. A history read only ever hands its boundary
  to a `where`, so it needs a BOUND, and a bound can be computed where the data is: the window travels
  as a **duration** and the query filters `ts >= now() - make_interval(secs => $n)`. One clock, the
  database's, and the instant is never named in Go at all. Six read paths, zero extra statements.
  Neither horn of the issue's choice was taken and nothing was left implicit.

  What this costs instead is that the boundary is now per statement rather than per request, so the
  reachability panel's per-interface strips are each bounded by their own statement's
  `transaction_timestamp` instead of by one instant shared across the loop. Sub-millisecond, against a
  24-hour window, and the instant they used to share was skewed against the data anyway.
- **Tracked under** [#667](https://github.com/hyperscaleav/omniglass/issues/667) and
  [#719](https://github.com/hyperscaleav/omniglass/issues/719).

### ADR-0109: An alarm carries an acknowledgement, and not a snooze or a resolve

- **Date:** 2026-08-13 | **Status:** Accepted | **Pages:** [alarms and actions](/architecture/alarms-actions/), [identity and access](/architecture/identity-access/), [API](/architecture/api/)
- **Context:** `alarm:ack,snooze,resolve` had appeared for most of a year in
  `internal/seed/seed_shrink_test.go`, in `internal/rbac/rbac_test.go`, and in the permission-format
  fence and the worked example on the identity-access page. Nothing seeded it, no route registered
  it, and no design document ever specified what snooze or resolve would do. It read as prior art,
  which is exactly the risk of borrowing a real resource's vocabulary for a fixture: three verbs
  looked decided, and only one of them had a meaning.
- **Decision (what is built):** the acknowledgement, alone. Two nullable columns on `alarm`,
  `acknowledged_at` and `acknowledged_by`, plus
  `POST /components/{name}/alarms/{id}:acknowledge`, the `alarm:acknowledge` permission, the seeded
  grant on `operator`, an `unacknowledged` filter on the list read, and the console affordance.
- **Decision (orthogonal, not a status):** an alarm's raised state belongs to its **condition**: the
  one-open invariant is per `(component, dedup_key)` and the raiser owns the key
  ([ADR-0075](#adr-0075-an-alarm-carries-its-condition-identity)). An acknowledgement is a fact about
  a **person**. Two independent facts, therefore two nullable columns and not a `status` enum, which
  would force them into one column and make acknowledged and cleared mutually exclusive. All four
  combinations are real and each one is tested: raised and unacknowledged (the queue), raised and
  acknowledged (somebody is on it), cleared having never been acknowledged (it came and went), and
  cleared after being acknowledged. A cleared alarm stays acknowledgeable, because the orthogonality
  runs in both directions and the record of who read an incident outlives the fix exactly as the row
  does.
- **Decision (no health recompute):** raising and clearing both recompute the health chain in their
  own transaction, and acknowledging deliberately does not. Acknowledging is not fixing: a transition
  recorded here would stamp a change at a moment when nothing about the estate moved, and the health
  history is meant to be edges and only edges.
- **Decision (the permission's name):** **`alarm:acknowledge`**. Every other seeded action spells its
  verb (`create`, `update`, `rename`, `move`, `reveal`, `issue`, `push`, `purge`,
  `revoke-session`), and a permission string is operator-facing: it is rendered in the Roles view, in
  the grant builder's tooltips, and in `/auth/me`. `alarm:ack` would have been the only abbreviation
  in the set, and its only claim was the fixture this ADR exists to disown.
- **Decision (idempotent, not refused):** a second acknowledgement of the same alarm keeps the first
  person and the first time, returns 200, and writes no second audit row. The recorded fact is
  monotonic, and the first sighting is the one that means something operationally (time to
  acknowledge). Refusing would turn a double click into an error an operator has to interpret, and
  would make two people opening the same queue race into a 409 that teaches nothing. It is the same
  shape the raise path already has: a re-raise of an open condition returns the existing incident
  and writes no audit row, because the no-op leg changed nothing. The database decides it, through a
  conditional `where acknowledged_at is null`, rather than a read-then-write that a concurrent
  acknowledgement could lose.
- **Decision (the scope, and why `deploy` does not get it):** the acknowledgement resolves
  `visible_set(P, alarm, acknowledge)` from **its own permission** on the **component** tier, not
  from `component:update` and not from `component:read`. A responder role may acknowledge without
  editing components, and a principal commonly holds an estate-wide read beside a narrow write, so
  binding the scope to either neighbour answers the wrong question in one direction or the other.
  Given that, `deploy` does **not** get the grant: a `deploy` grant is made at a **location** in
  practice (devseed's `tech-east` is exactly that shape), and a location-kind grant fills no
  component-tier scope at all ([#714](https://github.com/hyperscaleav/omniglass/issues/714)). The
  capability would resolve to nothing while the console's capability-only hint still offered the
  button: a grant that reads as given and refuses when used. It is revisitable when the cross-tier
  expansion ([#10](https://github.com/hyperscaleav/omniglass/issues/10)) lands, and
  `TestALocationGrantOfDeployReachesOneTier` now carries `alarm` in its matrix so the question
  resurfaces on its own.
- **What was refused:** **snooze**, because its purpose is to suppress **notification** and the
  outbound notification registry is unbuilt
  ([#618](https://github.com/hyperscaleav/omniglass/issues/618)); a snooze with nothing to suppress
  is a column that lies about what it does, and it belongs with that issue or after it. **Resolve**,
  because the platform already has `DELETE /components/{name}/alarms/{id}`, so a human "resolving"
  an alarm whose raised state belongs to its condition is either that clear under a second name or a
  different concept nobody has specified. **Un-acknowledge**, which is a second verb and a second
  decision rather than a nullable field somebody flips, and has no evidence behind it yet. **Bulk
  acknowledge**, until there is evidence the queue needs it. And **a denormalized acknowledger
  label** on the alarm row: `audit_log` already denormalizes the actor's name at write time for
  exactly the purge case, so the alarm keeps the foreign key (`ON DELETE SET NULL`) and resolves the
  current label on read, which reads honestly empty after a purge while the durable record survives
  where it already lived.
- **Divergence recorded:** the identity-access page's three-way status split says a target the
  caller can **read** but not act on is a **403**. Every scoped route in the platform answers
  **404** there, this one included, because the target is resolved through the gateway with the
  action's own scope and a miss is a miss. Making it three-way is one shared refusal primitive
  across every scoped route rather than a per-route special case, and doing it for this route alone
  would have made it the only route in the system that answers differently from its own siblings.
  Tracked in [#736](https://github.com/hyperscaleav/omniglass/issues/736); the page now marks the
  branch as design.
- **Tracked under** [#728](https://github.com/hyperscaleav/omniglass/issues/728),
  [#733](https://github.com/hyperscaleav/omniglass/issues/733),
  [#734](https://github.com/hyperscaleav/omniglass/issues/734) and
  [#735](https://github.com/hyperscaleav/omniglass/issues/735).

### ADR-0110: A principal's identifier is the gateway's answer, not a stored function's

- **Date:** 2026-08-13 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/), [audit](/architecture/audit/), [identity and access](/architecture/identity-access/)
- **Context:** `principal_label(uuid)` was a stored SQL function,
  `coalesce(human.username, service.label)`, and it was wrong on both counts. It put the platform's
  answer to "what names this principal" in the database, which is the one place this repository says
  logic never lives, and it called the answer a LABEL when both branches return an identifier. It
  was also unpinned: no test named it, so it could have returned anything and the suite would have
  agreed. The word matters beyond tidiness, because `label` is about to become the schema's name for
  the friendly string on eighteen tables ([#613](https://github.com/hyperscaleav/omniglass/issues/613)),
  and a word cannot be renamed onto a column while it still means two other things somewhere else.
- **Decision:** the function is dropped and the answer moves to
  `internal/storage/principal_ident.go`, which names the tables and columns ONCE
  (`principalIdentSources`) and renders every shape a statement can need from that one list, so a
  caller picks a shape and never a column.
- **Which shape, measured rather than assumed.** A read over many rows LEFT JOINs the sources and
  folds them in Go. Two positions cannot join, and both are bounded reads: `alarmCols` is read by an
  `UPDATE ... RETURNING` (`RETURNING` cannot left-join, and giving the `UPDATE` a `FROM` clause
  would change which rows it updates), and the audit insert runs inside the CALLER's transaction on
  every operator write, where a Go fold means a second round trip and the alarm write path pins its
  statement count as the exact equation `12 + 5*slots + 4*locations` (`alarm_cost_test.go`,
  ADR-0094) that counts that insert. Both render the sources as correlated sub-selects instead. The
  first draft of this decision used sub-selects everywhere, and a review measured it: on a
  500-member group roster, projecting them AND sorting on them costs **3011 shared buffer hits and
  2000 index searches** where the join costs **18**, because Postgres does not common up two
  identical scalar sub-selects. Moving the policy out of the database was the point; paying a round
  trip on every operator write, or two thousand index probes on an unbounded roster, was not.
- **What holds the shapes together.** The columns are named once, so only the ORDER is written
  twice (as Go control flow and as a coalesce), and `principal_ident_test.go` drives a human, a
  service account, a node and an unknown id through EVERY shape against a real database, asserts
  they agree and asserts what they agree ON, and fails on the first disagreement. Asserting only
  the agreement would pass when all shapes are wrong the same way, which is exactly what naming the
  wrong column in the one source list would do. That is the recompute-and-compare shape the gateway
  already uses wherever one fact has two readers, applied to a policy rather than to a derived
  column.
- **The audit READ moved with them.** `ListAuditLog` kept its own hand-written
  `coalesce(ah.username, a.actor_username, '')`, which resolved a HUMAN actor live and a SERVICE
  actor only from the row's snapshot: a fourth statement of the policy, asymmetric between two kinds
  for no recorded reason. It now resolves both live through the same sources and falls back to the
  snapshot, which is what makes the trail survive a purge (ADR-0016).
- **A node is still not a source.** `principal_label` never read `node.name`, so a node principal
  resolved to null, and this change preserves that rather than quietly widening what an audit row
  says. Nothing seeds a node grant today, so no path reaches it; widening the resolution is its own
  decision with its own test.
- **Tracked under** [#564](https://github.com/hyperscaleav/omniglass/issues/564), under
  [#613](https://github.com/hyperscaleav/omniglass/issues/613).

### ADR-0111: A service account's identifier is a name, and it is unique

- **Date:** 2026-08-13 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/), [audit](/architecture/audit/)
- **Context:** `service` has exactly two columns, `principal_id` and `label`, and the second one is
  what IDENTIFIES a service principal: it is the username analogue for `kind=service`, the only
  operator-visible handle the row has. It was also on the wire as `svcBody.Label`, three lines from
  the human body's `display_name`, so two different concepts read as one. Under the identity triad
  an identifier is a `name`, and this was the only place in the schema where `label` meant one,
  which matters beyond tidiness because `label` is about to become the schema's word for the
  friendly string on eighteen tables ([#613](https://github.com/hyperscaleav/omniglass/issues/613)).
- **Decision:** the column is `service.name`, on the wire as `name`, in the console as **Name**.
- **Uniqueness: yes, answered rather than inherited.** The column carried no index and no
  constraint. It is now `service_name_key`, matching `human_username_key` and `node_name_key`, so
  all three principal-kind identifiers behave the same way. The reason is not symmetry, it is that
  the string is DENORMALIZED as bare text where nothing survives beside it: into
  `audit_log.actor_username` at write time so the trail outlives a purge, and into an alarm's
  acknowledgement on every read. Two service accounts sharing a name make both unresolvable after
  the fact, which is exactly why a username is unique. It is also free today and expensive later:
  there is no create path for a service principal on the gateway or the API, so nothing can be
  holding a duplicate an operator typed, and the estate has no releases and no operator data. The
  migration still copes: a dedupe backfill runs first, keeping the name on the oldest row of each
  duplicated set (`principal.id` is uuidv7, so it sorts by creation time) and suffixing every other
  with its own principal id, which is unique by construction rather than by luck.
- **The declaration moved with it, and grew a guard.** `service` was declared `ShapeIDOnly`, whose
  published sentence is "nobody names it". That was believable only while the identifier was called
  `label`, and renaming the column would have left the declaration saying the opposite with a green
  suite, because nothing checked it. `service` is now `ShapeHumanNotAKey` (a username analogue, on
  its own rule, not an address), and a new guard reads the generated schema facts and fails any
  `ShapeIDOnly` table carrying a `name` column.
- **The mixed fallback is what the rename was for.** The group roster read
  `coalesce(h.display_name, s.label, '')` into one field: a human's friendly string falling through
  to a service account's identifier. Written out after the rename it reads
  `coalesce(h.display_name, s.name, '')`, and the mistake is on the page. The fix is not a better
  chain but two fields, `name` (the identifier, resolved by the gateway's `principalIdent`) and
  `display_name` (the friendly string, which only a human has a column for), with the renderer
  choosing. A service member's identifier used to arrive in a field called `display_name`, which an
  API test asserted verbatim.
- **Breaking wire change**, on two read shapes: `svcBody.label` is `svcBody.name`, and the roster's
  `username` is `name` with `display_name` narrowed to humans. No CLI flag moves, because neither
  field is a request field.
- **Tracked under** [#563](https://github.com/hyperscaleav/omniglass/issues/563), under
  [#613](https://github.com/hyperscaleav/omniglass/issues/613).
### ADR-0112: A generated flag carries the schema's type, and a structured field carries JSON

- **Date:** 2026-08-13 | **Status:** Accepted | **Pages:** [the CLI](/guides/cli/), [API first](/contributing/api-first/)
- **Decision:** `cmd/cligen` derives each body flag's **type** from the OpenAPI property it comes
  from. A scalar the shell can type takes that flag type (`integer` is an `int` flag, `boolean` a
  `bool` flag, `number` a `float64` flag, `string` a string flag), so a value the schema refuses is
  refused **at the shell** rather than by the server's 422 after an authenticated round trip. Every
  other shape keeps **one string flag parsed as JSON**: an object, an array, an untyped `any`, and a
  nullable number or boolean.
- **Context:** every body flag was a string, and every non-string field was coerced at run time by
  `jsonOrString`. The schema said integer, the CLI said string, and the generated CLI reference
  published `string` for all of them, which is the generate-first drift class moved inside the
  generator: a fact the spec states, restated by hand as something else. The same wave surfaced it
  twice, on `name_rule` (an object) and on `settle_window_seconds` (an integer with a floor of 0 the
  flag could not carry).
- **Why a structured field stays one JSON string, decided rather than left:** a nested value has no
  shell-native flag type. The alternatives are repeated `key=value` pairs (pflag's `StringToString`),
  which can express neither nesting nor an array member, or a flag per leaf, which would make the flag
  NAMES depend on how a `$ref` happens to nest and rename them when it changes. Above both,
  **`null` has to stay sendable**: a nullable object is cleared by naming it in `update_mask` and
  sending null ([ADR-0106](#adr-0106-a-location-type-is-platform-owned-and-a-nullable-object-clears-under-the-mask)),
  and a typed flag has no null, so `--name-rule null` is the only spelling of that clear. That is why
  a nullable number or boolean keeps the JSON spelling too, though the spec carries none today.
- **The one nullable exception:** a nullable **string** stays a plain string flag. This API clears a
  string with the empty string rather than with null, and routing it through JSON would hand it the
  quoting hazard the string passthrough exists to avoid (a name that is literally `30`).
- **What moved for an operator:** a boolean is now a real bool flag, so `--propagates=false` is the
  spelling and `--propagates false` reads `false` as a positional argument. One documented line in the
  CLI guide was exactly that mistake, so the docs flag check now fails on a bool flag handed a
  space-separated `true` or `false`, which is a claim a page can no longer make by accident. A
  required flag's rendered example names what the flag takes (`--position <int>`, `--fields <json>`)
  instead of repeating the field name, since `--position position` read as a runnable line and is now
  refused by the parser rather than by the server.
- **Corrected on the way in:** the issue names `expected_ordinal` as one of the two fields that
  surfaced this. That field does not exist: the #702 review replaced it with `expected_name`, and
  `internal/docslint` refuses the word. `settle_window_seconds` is the integer under test instead.
- **Tracked under** [#711](https://github.com/hyperscaleav/omniglass/issues/711).

### ADR-0113: A validation rule is TypeScript, and a native constraint attribute is not one

- **Date:** 2026-08-13 | **Status:** Accepted | **Pages:** [design system](/contributing/design-system/)
- **Decision:** a console control carries **no** `required`, `min`, `max`, `minlength`, `maxlength`,
  `pattern` or `step`. A rule is a **pure function** over the typed value (`lib/validate.ts`,
  `readSettleWindow` in `lib/command_types.ts`), the surface renders its message **inline beside the
  field**, and the form binding's `disabled` / `valid` refuses the submit. `aria-required` stays,
  because it announces a fact to a screen reader without claiming the browser will refuse the value.
  `validation-guard.test.ts` scans every `.tsx` for a native constraint on an `input`, `select` or
  `textarea`.
- **Context:** the browser enforces a constraint attribute on a real form **submission**, and this
  console performs none on the paths an operator uses. A Drawer's action rail is drawn by the shell
  and portaled outside the `<form>` ([ADR-0054](#adr-0054-the-shell-owns-a-panels-action-rail-the-body-registers-and-never-draws)),
  a blade has no `<form>` at all, and the inline editors (RoleEditor, ContractEditor) save from an
  `onClick`. **The audit is what decided it:** 21 attributes on 24 rendered controls, of which 17
  sites are on a surface with no form submission at all, and the remaining four (three on Login, one
  on the forced password change) sit in forms whose submit button is `disabled` in exactly the states
  native validation would have refused. **Zero of the twenty-four could ever fire.** A reader could
  not tell a live attribute from a decorative one because there were none of the first kind.
- **Why not make them live instead (`form.requestSubmit()` from the rail):** it would have covered
  the Drawers and left every blade needing this decision anyway, since a blade is a field set with a
  contributor registry rather than a form, so native validation could not be the one vocabulary. It
  would also mean threading a form ref through the binding into a portaled shell to reproduce an
  effect `disabled` / `valid` already produce, and then **undoing** the disabled gate so the browser
  could refuse instead, which is a worse operator experience: a native bubble is unstyled browser
  chrome that says less than the inline message and vanishes on blur.
- **What it costs and what it does not:** every removed attribute already had a TypeScript rule
  behind it except one, the token drawer's `min="1" max="365"`, which is now `tokenTtlError` and
  refuses a 400-day token at the field rather than at the server's 422. `type="email"` and
  `type="number"` stay: an input TYPE carries keyboard, autofill and spinner behaviour, and is not a
  claim that a value will be refused.
- **Found by** [#718](https://github.com/hyperscaleav/omniglass/issues/718), whose brief asked for a
  `min="0"` on an input that had carried one since [#411](https://github.com/hyperscaleav/omniglass/issues/411)
  and had never fired once. **Tracked under** [#724](https://github.com/hyperscaleav/omniglass/issues/724).
