---
title: "The membership gate is two layers, and a refusal names the one that is missing"
date: 2026-08-12
refs: []
form: bullet
---

- **The membership gate is two layers, and a refusal names the one that is missing.** The review of
  the slice above found both halves of "the narrowing is met before the form is filled in" untrue for
  a shipped shape. The console gate read the `system:update` PERMISSION and nothing else, so a
  principal holding it over an empty scope was offered the picker, filled the form in and was refused
  on submit. And the bind resolved in the `system:update` scope alone, so a system outside it came
  back as the non-disclosing not-found and the route answered "system not found" for a row the same
  caller could `GET`.

  The principal is not exotic. `applicableKinds("system")` is `{"system"}` alone and the cross-tier
  expansion is unbuilt (#10), so a location-scoped `deploy` grant fills no system-tier scope at all,
  and devseed ships exactly that as `tech-east`. Driven end to end, that grant can do NO system work:
  create, update, rename, move and all three membership writes refuse it, and without a second grant
  it cannot even list a system. So the previous slice did not break a working role, it extended a
  pre-existing gap to one more path, which is why the fix here is an honest refusal and a console
  that does not offer what the server will decline, rather than the cross-tier expansion.

  The bind now takes `system:read` beside `system:update`. Update still decides whether it happens;
  read decides what the refusal may say. Out of the read scope is the same non-disclosing 422 as
  before, so nothing new is disclosed; inside it, a **403 names the scope**, which discloses nothing
  either, since that caller can read the row. The console gate now also requires a system carrying
  the scope-aware `update` action (the server's own per-row answer, from the same per-action scope
  the gateway enforces), offers only those systems, and says which of the two layers is missing.
