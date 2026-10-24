---
title: "`credential` is renamed `secret`; the cascade is the reuse mechanism"
date: 2026-07-09
status: "Accepted"
summary: "The access-secret member of the config / credential / variable trio is renamed credential to secret: an encrypted-at-rest typed value resolved most-specific-wins down the cascade"
---

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
