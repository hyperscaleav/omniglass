---
title: "Roles carry requirements, not an allow-list"
date: 2026-06-27
status: "Accepted"
summary: "Authorization is role + scope grants, not a per-principal allow-list"
---

- **Date:** 2026-06-27 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** Authorization is built from additive `(role x scope)` grants, where a role is a capability
  set of `<resource>:<action>` permissions. An earlier sketch attached a per-principal **allow-list** of
  permitted actions directly.
- **Context:** A per-principal allow-list does not compose: the same operator role at two scopes, or a role
  inherited and extended, would be re-listed by hand per principal. Roles plus scope make the common case
  (the same role at different places) a single reused definition, and keep permissions additive and
  positive (no negative entries). It is also what makes the per-grant binding (an action and its scope bind
  in the *same* grant) expressible.
