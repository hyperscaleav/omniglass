---
title: "A registry row renames through its own :rename custom method"
date: 2026-10-26
status: "Accepted"
summary: "A vendor, driver, product, standard or location type renames through `POST /<registry>/{id}:rename`, gated by its own `<registry>:rename` (admin), not a name field on each patch as #555 first proposed. One gateway primitive (`RenameRegistry`) serves all five: it validates the name, refuses an official row (the boot seed owns it by name), moves the one column, carries any by-value copy of the name along (a location type's in `allowed_parent_types` and in forks), and audits it in the transaction. The telemetry-keyed catalogs and the remaining type registries wait on #1188"
refs: [555, 825, 1188]
---

- **Date:** 2026-10-26 | **Status:** Accepted | **Pages:** [API](/architecture/api/), [CLI](/guides/cli/)
- **Decision:** (#555, pre-tag hygiene for 0.1.0, #825.)
  1. **The rename is a custom method, not a patch field.** `POST /vendors/{id}:rename`,
     `/drivers/{id}:rename`, `/products/{id}:rename`, `/standards/{id}:rename`, and
     `/location-types/{id}:rename` take `{"name": "<new>"}` and answer the row as its `GET` reads it.
     The issue proposed a name field on each registry's patch struct, which
     [ADR-0076](#adr-0076-a-renameable-human-typed-identifier-stays-in-the-url-and-the-write-returns-the-uuid)
     had already ruled out for every renameable name (a rename is a custom method gated by
     `<resource>:rename`, not a field write), and which a component, system, location and principal
     group follow; the registries now follow it too, because a rename is a different act from an
     edit. Nothing inside the platform moves, since every foreign key holds the uuid
     ([ADR-0062](#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle)), but
     everything typed outside it (a script, a bookmark, an integration's config) stops resolving.
  2. **Its own permission.** Each is gated by `<registry>:rename`, which the admin role carries beside
     the registry's create, update and delete, so a role can be given edits without the act that
     breaks external references, exactly as `location:rename` sits apart from `location:update`.
  3. **One primitive.** `storage.RenameRegistry(table, ref, name)` is the act for every registry it
     covers, and the API registers the five routes through one function over it. It validates the
     name with the entity rule, refuses an official row (422: the boot seed upserts by name and would
     re-create the old one on the next start), refuses a taken name (409) and a location type named
     `root` (422), moves the one column, and writes the `rename` audit row in the same transaction.
  4. **A by-value copy moves with it.** A location type's name is also stored by value, in every
     type's `allowed_parent_types` and inside an operator's fork of a shipped type
     (`registry_shadow`), and the rename rewrites both in its transaction, keeping their order, so a
     placement rule never silently strands. A label rendered from the old name is not restamped; that
     stays the recompute verb's.
- **Context:** ADR-0062 gave every registry a uuid primary key so its name could change, and #555
  found that nothing but raw SQL could change it: no registry patch carried a name. The registries
  left out are the catalogs keyed by a telemetry name (metric, property, event and command types),
  whose key is also what a node, a driver spec and an integration push by name, and the remaining type
  registries (`component_type`, `system_type`, `secret_type`); each has a by-name reader a rename must
  answer for first, so they are [#1188](https://github.com/hyperscaleav/omniglass/issues/1188) rather
  than a primitive that would strand them. The console's blades still do not edit a registry name; the
  rename is the API's and the CLI's for now.
