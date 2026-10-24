---
title: "The cascade's least-specific tier is `platform`"
date: 2026-07-22
refs: []
form: bullet
---

- **The cascade's least-specific tier is `platform`.** One word, `global`, named two unrelated things: the
  install-wide **binding tier** and the singleton estate **owner** where health and KPIs roll up. The tier is
  renamed **`platform`** on both axes, at exactly the rung it already occupied, and `default` is documented off
  the axis entirely ([ADR-0057](/architecture/decisions/#adr-0057-the-cascades-least-specific-tier-is-platform-and-a-default-is-not-a-tier)).
  An idempotent migration rewrites `owner_kind` on `variable`, `secret`, and `tag_binding` plus `setting_override.scope`,
  with the check constraints and the partial unique indexes keyed on the value; **no precedence changes, no rows are
  added, and every existing deployment resolves byte-identically after upgrade**. The settings engine's `code` level
  becomes **`default`** and its `global` level **`platform`**, the enum rides `make gen` into the OpenAPI, the typed
  client, and the CLI (`tag setPlatform` / `clearPlatform`, `--owner-kind platform`), and the console renders the tier
  and a declared default as what they are ("Declared default" carries no origin badge, because nobody set it). The
  new capability is authorization: a write at the tier now needs **`platform:<action>`** on top of the resource
  permission, published per route as `x-omniglass-platform-permission` and seeded to `admin` only, so an all-scoped
  operator runs every site without being able to move the install-wide value under them. `platform` joins the
  **sensitive-resource set** (with `secret` and `settings`) so a bare `*:update` cannot reach it: install-wide
  authority is never implied by estate reach, in the rbac core and in the console's mirror of it. The console gates
  every tier control on **both** halves, on all three surfaces that write at the tier: the Settings page, and the
  Secrets and Variables pages, where a caller without the tier half is not offered the Platform scope on the create
  form nor Edit / Delete on a tier row, and reads which capability is missing instead of meeting a 403. **Breaking:** a secret
  sealed at the old tier cannot be decrypted after upgrade, since the AAD binds `ownerKind|ownerID|name|field`
  (accepted deliberately, see the ADR). Proven by a testcontainer upgrade test (pre-rename rows resolve identically
  after the migration, the constraints and unique indexes still hold one row per identity), the settings resolver
  unit suite on the renamed levels, and an HTTP end-to-end authz test (an all-scoped role without `platform:*` is
  refused at the tier and permitted below it, and the seeded roles carry the capability exactly where they should).
  The dev seed carries the no-root-location rule into `make dev`: the example estate is a forest of three unparented
  tops with devices under two of them, so the location binding at HQ visibly misses the East campus and only the
  `platform` value reaches both (proven by a pure fixture test and a seeded effective-tags assertion).
  A closing migration puts `setting_override.scope` under its own CHECK. It was the one tier column with no
  constraint behind it, which is how the rename passed a green suite while the Go layer still wrote `global` and
  orphaned every override in silence. The legal set is the levels that are persisted as rows, today `platform`
  alone: `default` is reflected off the `Settings` struct and `file` is read from disk at boot, so neither can ever
  be a row, and the group and user rungs will widen the CHECK when they land.
  The [cascade](/architecture/cascade/) page moves to
  `Partial`: its binding chain ships for tags, secrets, and variables, while the template bands, group placement by
  weight, rule accumulation, and the resolve view stay `Design`.
