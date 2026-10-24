---
title: "A resource is one kebab-case noun; nesting means ownership"
date: 2026-07-22
status: "Accepted"
summary: "A resource is one kebab-case noun; nesting means ownership"
---

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
