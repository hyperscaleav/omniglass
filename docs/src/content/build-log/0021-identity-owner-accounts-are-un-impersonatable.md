---
title: "Identity: owner accounts are un-impersonatable"
date: 2026-07-07
refs: []
form: bullet
---

- **Identity: owner accounts are un-impersonatable.** A principal holding `owner @ all` cannot be impersonated
  by anyone, including another owner, in either mode (ADR-0012): a target-side check in the `:impersonate`
  handler before the mode branch (403), reusing the owner-invariant's `role='owner' and scope_kind='all'` lane.
  The escalation guard (`Covers`) already blocked a lesser admin from an owner, but `owner.Covers(owner)` was
  true, so owner-impersonates-owner was possible; this removes the highest-trust-account takeover vector
  explicitly. Impersonate stays gated by `principal:impersonate` **swept by `principal:*`** (holding it already
  lets a caller create and use its own principals, so impersonate confers no new reach there). Act-as scope
  intersection (#101) is **dropped**: act-as stays all-scope-only. Proven by the real-binary API test (an owner
  cannot view-as or act-as another owner, 403; admin-to-owner stays 403; a non-owner target still works).
