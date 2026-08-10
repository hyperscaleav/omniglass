---
title: Admin guide
description: "Administering an Omniglass platform: the people who can sign in, the access they carry, the audit trail, and the config and credentials that resolve down the estate."
---

This is the how-to for **administering the platform**, the standing job of deciding who may
sign in, what each account can see and do, and the config and credentials the estate runs on.
It is a different job from **operating** the estate (running the inventory, reading the data),
which is the [operator guide](/guides/operator/), and from **standing the platform up**, which
is [deployment](/guides/deployment/).

The dividing line is the [two authorization layers](/architecture/identity-access/), both
enforced in the app on every request: a `<resource>:<action>` **permission** checked on every
route, and an ABAC **scope** injected by the Storage Gateway on every applicable query. An
administrator is simply a principal whose grants carry the admin-tier permissions (`principal:*`,
`role:read:admin`, `audit:read:admin`, `secret:*`, and so on); the surfaces below render and refuse exactly
along those grants, so what an administrator sees is what they are allowed to change.

## Where the admin surfaces live

Every task on these pages has two front doors, and they call the same API with the same checks:

- **The console.** The **Admin** area of the [web console](/guides/operator/) holds Users,
  Roles, Groups, Settings, and Audit. Secrets and Variables have moved to **Inventory**, in the **Values**
  band. A tab you have no read grant for is hidden and its route refused, so the console never
  paints a page you cannot use.
- **The CLI.** The same surfaces are generated commands on the [`omniglass` CLI](/guides/cli/)
  (`principal`, `principal-group`, `secret`, `variable`, and the rest, all in the [CLI
  reference](/reference/cli/)), plus the trusted direct-database lane (`bootstrap`, `token`,
  `set-password`) that mints the very first owner before any server is running.

## In this guide

- **[Manage users](/guides/admin/users/)** is the principal directory: creating a human,
  editing and renaming, the disable-archive-purge lifecycle, admin password reset, and profile
  pictures.
- **[Roles, groups, and grants](/guides/admin/access/)** is giving a user access: the built-in
  roles, user groups as shared grant anchors, and the grant builder that assigns a role at a scope.
- **[The audit trail](/guides/admin/audit/)** is the read-only record of every privileged
  action and every sign-in, including who acted behind an impersonation.
- **[Secrets](/guides/admin/secrets/)** and **[variables](/guides/admin/variables/)** are the
  config and credentials the estate resolves down the [cascade](/architecture/cascade/):
  encrypted secrets and plaintext variables, owned at a scope and resolved most-specific-wins
  onto a component.
- **[Files](/guides/admin/files/)** is the content kept with the estate: uploads in the blob
  store, owned at a scope like the other values.
- **[Tags](/guides/admin/tags/)** is the governed key vocabulary behind the tag chips, and the
  signal catalog is two lane pages: **[properties](/guides/admin/properties/)** for the canonical
  typed values and **[metrics](/guides/admin/metrics/)** for the numeric quantities, with their
  units and precision.
- The classifier catalogs shape the estate's entities:
  **[location types](/guides/admin/location-types/)** for locations, **[standards](/guides/admin/standards/)** for
  systems, and **[products](/guides/admin/products/)** for components, with
  **[vendors](/guides/admin/vendors/)** and **[drivers](/guides/admin/drivers/)** the reference
  registries a product ties together. Beside standards sit the two coarse genus registries the
  console calls **Types**: the
  [component type](/architecture/core-entities/#catalog-reference-data-component_type) tree above
  products, and the [system type](/architecture/core-entities/#catalog-reference-data-system_type)
  tree that says what kind of space a system is.

The model behind all of this is [identity and access](/architecture/identity-access/) and
[config and credentials](/architecture/variables/); those pages say how it is built, these say
how to run it.
