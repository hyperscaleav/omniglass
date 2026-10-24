---
title: "`secret` is a sensitive resource; a per-secret `admin_sensitive` flag flips a secret to the `:admin` tier"
date: 2026-07-13
status: "Accepted"
summary: "`secret` leaves the bare `*` wildcard's reach (direct match and read floor); a per-secret `admin_sensitive` flag flips a secret to the `:admin` tier, so operators read operational device secrets in scope while platform credentials stay admin/owner-only at the same scope"
---

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
