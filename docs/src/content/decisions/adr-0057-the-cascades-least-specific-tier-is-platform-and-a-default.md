---
title: "The cascade's least-specific tier is `platform`, and a `default` is not a tier"
date: 2026-07-21
status: "Accepted"
summary: "The cascade's least-specific **binding** tier is renamed `global` to **`platform`** on both axes (same rung, no precedence change); a **`default`** is off the axis entirely, a column on a type declaration rather than a tier; there is **no root location**; a write at the tier needs its own **`platform:<action>`** permission. **Breaking:** a secret sealed at the old tier can no longer be decrypted (the AEAD binds the owner kind)"
---

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
