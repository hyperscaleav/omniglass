---
title: Admin guide
description: "Administering an Omniglass platform: the people who can sign in, the access they carry, the audit trail, and the config and credentials that resolve down the estate."
---

This is the how-to for **administering the platform**, the standing job of deciding who may
sign in, what each account can see and do, and the config and credentials the estate runs on.
It is a different job from **operating** the estate (running the inventory, reading the data),
which is the [operator guide](/guides/operator/), and from **standing the platform up**, which
is [deployment](/guides/install/).

The dividing line is the [two authorization layers](/architecture/identity-access/), both
enforced in the app on every request: a `<resource>:<action>` **permission** checked on every
route, and an ABAC **scope** injected by the Storage Gateway on every applicable query. An
administrator is simply a principal whose grants carry the admin-tier permissions (`principal:*`,
`role:read`, `audit:read`, `secret:*`, and so on); the surfaces below render and refuse exactly
along those grants, so what an administrator sees is what they are allowed to change.

## Where the admin surfaces live

Every task on these pages has two front doors, and they call the same API with the same checks:

- **The console.** The **Settings** area of the [web console](/guides/console/) holds Users,
  Roles, Groups, Audit, Secrets, and Variables. A tab you have no read grant for is hidden and
  its route refused, so the console never paints a page you cannot use.
- **The CLI.** The same surfaces are generated commands on the [`omniglass` CLI](/guides/cli/)
  (`principal`, `principal-group`, `secret`, `variable`, and the rest), plus the trusted
  direct-database lane (`bootstrap`, `token`, `set-password`) that mints the very first owner
  before any server is running.

## In this guide

- **[Users, roles, and groups](/guides/admin/identity/)** is identity and access management:
  the principal directory, the built-in roles, user groups as shared grant anchors, and the
  grant builder that assigns a role at a scope.
- **[The audit trail](/guides/admin/audit/)** is the read-only record of every privileged
  action and every sign-in, including who acted behind an impersonation.
- **[Secrets and variables](/guides/admin/config/)** is the config and credentials the estate
  resolves down the [cascade](/architecture/cascade/): encrypted secrets and plaintext
  variables, owned at a scope and resolved most-specific-wins onto a component.

The model behind all of this is [identity and access](/architecture/identity-access/) and
[config and credentials](/architecture/variables/); those pages say how it is built, these say
how to run it.
