---
title: "Platform: the settings engine (install-wide level)"
date: 2026-07-19
refs: []
form: bullet
---

- **Platform: the settings engine (install-wide level).** The [settings](/architecture/settings/) subsystem ships its first
  slice: a pure `settings` package that resolves an effective configuration document by deep-merging ordered layers (embedded
  declared defaults, an optional operator `file`, and the install-wide DB override) most-specific-wins in JSON map-space, tracking
  per-key **provenance** and enforcing a top-down **lock** (broader level wins). The single **`setting_override`** table holds
  only the override level, since the base layers are recomputed in memory each boot and restore is therefore a delete
  ([ADR-0033](/architecture/decisions/#adr-0033-settings-persist-only-the-override-level-base-layers-are-recomputed-in-memory)),
  and its Gateway methods are **unscoped**, gated by the `settings:<action>` permission alone
  ([ADR-0034](/architecture/decisions/#adr-0034-the-settings-gateway-is-unscoped-only-the-permission-gates-it)), reusing the
  [cascade](/architecture/cascade/) primitive on the principal axis
  ([ADR-0035](/architecture/decisions/#adr-0035-settings-resolve-as-a-cascade-over-principals-with-a-broader-wins-lock)). The
  API exposes an admin **`GET /settings`** with provenance, a client-safe authn-only **`GET /settings/me`**, and **`PATCH`** /
  **`DELETE`** / **`POST /settings:restoreDefaults`** writes under `settings:read` / `settings:update`; the generated CLI and
  typed client follow. Two `profile`-domain namespaces seed (**`ui`** and **`keybindings`**), and **`ui.theme` is wired end to
  end**: the console reads its theme from `/settings/me`, so an admin setting the org theme default re-themes the SPA on next
  load. The Admin **Settings** page (namespace sections, provenance badges, lock chips, restore-to-default) replaces the
  `/settings` nav stub. Proven by the pure engine unit suite (deep merge, RFC 7386 merge-patch, cascade resolution, lock
  enforcement, provenance), a testcontainer override round-trip (upsert, audit-in-transaction, delete-restores), an HTTP e2e
  (admin read with provenance, patch-then-read reflects the override, `/settings/me` readable by a non-admin, the admin read
  forbidden to a viewer), and web tests (the page renders a provenance badge; the theme mapper). Deferred to the fast-follow:
  the **group** and **user** override rungs and the Profile preferences tab, the `settings:lock` split for group-admins,
  `platform`-domain namespaces (`retention`, `integrations`), a GitOps read-only mode, and live file reload (SIGHUP).
