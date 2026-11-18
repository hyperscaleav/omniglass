---
title: Admin
description: "Setting Omniglass up for your organization: accounts, access, audit, secrets and variables, the catalog, and automation."
sidebar:
  label: Overview
  order: 0
---

**For:** the people who configure Omniglass for everyone else: who can sign in and what they can
reach, what the fleet runs on, and what happens when something goes wrong.

**In this section:** users, roles, groups and grants; the audit trail; secrets, variables and files;
and the platform settings. Running the fleet day to day is the [Operator](/operator/) section's job,
and standing the platform up is [Deployment](/guides/deployment/).

## What makes someone an admin

Two checks run on every request, both in the app
([identity and access](/architecture/identity-access/)): a `<resource>:<action>` **permission** on
every route, and a **scope** the Storage Gateway adds to every query it applies to, so a grant at one
location reaches only that subtree. An administrator is simply a user whose grants carry the
admin-tier permissions (`user:*`, `role:read:admin`, `audit:read:admin`, `secret:*`, and so on). The
surfaces render and refuse along exactly those grants: what an administrator sees is what they may
change.

## Two ways in, one API

- **The console.** Its **Admin** group holds Users, Roles, User groups, Settings and Audit; Secrets,
  Variables and Files sit under **Values**. A page you have no read grant for is hidden and its route
  refused, so the console never paints a page you cannot use.
- **The CLI.** The same surfaces are generated commands on the [`omniglass` CLI](/guides/cli/)
  (`user`, `user-group`, `secret`, `variable`, `setting` and the rest, all in the
  [CLI reference](/reference/cli/)), plus the trusted direct-database lane (`bootstrap`, `token`,
  `set-password`) that mints the very first owner before any server is running.

## The pages

- **[Users](/admin/users/)**: creating a person, editing and renaming, the
  disable-archive-purge lifecycle, password reset, and profile pictures.
- **[Roles, groups, and grants](/admin/access/)**: the built-in roles, user groups as shared grant
  anchors, and the grant builder that assigns a role at a scope.
- **[The audit trail](/admin/audit/)**: the read-only record of every privileged action and every
  sign-in, including who acted behind an impersonation.
- **[Secrets](/admin/secrets/)** and **[variables](/admin/variables/)**: the credentials and config
  the fleet resolves down the [cascade](/architecture/cascade/), owned at a scope and resolved
  most-specific-wins onto a component, and the secret types that shape them.
- **[Files](/admin/files/)**: the content kept with the fleet, uploads owned at a scope like the
  other values.
- **[Platform settings](/admin/settings/)**: the install-wide namespaces, how long each kind of data
  is kept, and how often an endpoint reports on itself.

The catalog and automation pages are still the **Admin guide** under **Being rebuilt** in the
sidebar until they land here: [tags](/guides/admin/tags/), [properties](/guides/admin/properties/),
[metrics](/guides/admin/metrics/), [events](/guides/admin/events/),
[commands](/guides/admin/commands/), [location types](/guides/admin/location-types/),
[standards](/guides/admin/standards/), [products](/guides/admin/products/),
[vendors](/guides/admin/vendors/) and [drivers](/guides/admin/drivers/) (#2190), and
[rules and severity](/guides/admin/rules/), [calc rules](/guides/admin/calc-rules/) and
[notifications](/guides/admin/notifications/) (#2191).
