---
title: "The tag cascade follows the component it resolves for"
date: 2026-08-09
status: "Accepted"
summary: "Effective tags are scoped by the component the cascade resolves FOR, not per band: a caller who can read the component sees every value that cascades onto it, including from systems and locations it could not list directly; the `?system=` seed is a filter over that answer, never a widening of it"
---

- **Date:** 2026-08-09 | **Status:** Accepted | **Pages:** [tags](/architecture/tags/),
  [identity and access](/architecture/identity-access/)
- **Decision:** Effective tags are authorized by the **component the cascade resolves for**, and by
  nothing else. A caller who can read that component sees every value that cascades onto it,
  including values owned by a system or a location the caller could not have listed directly. The
  `?system=` parameter is a **filter over that answer**, choosing which membership seeds the system
  band, and never a widening of it: `seed_sys` already requires the named system to be one the
  component belongs to. Per-band scope checks are deliberately NOT applied.
- **Context:** [#641](https://github.com/hyperscaleav/omniglass/issues/641) proposed resolving
  `?system=` through the caller's read scope. Three findings closed it as the posture we already
  hold rather than a defect. First, the obvious implementation is the exact change
  [#627](https://github.com/hyperscaleav/omniglass/issues/627) made and reverted as a critical:
  `ResolveTags` resolves its scope for **component** (the route is gated on `component:read`), and
  `inScopeTree` walks the target table's own chain, so a component-tier id can never appear in a
  system's ancestor chain and every non-all caller's band silently vanished, a wrong answer with no
  error rather than a refusal. `TestResolveTagsSystemBandSurvivesScopedCaller` stands as that
  regression and asserts the band IS present for a scoped caller. Second, the check would not close
  the disclosure it named: the same values reach the same caller through the primary-membership
  default seed, through `sys_chain`'s ancestor walk above the seeded system, and through the
  deliberately scopeless batch resolver behind the directory's Tags column. Third, and decisively,
  the cascade is a property OF the component: a value that resolves onto a component the caller may
  read is part of that component's configuration, and withholding it would report a resolved
  configuration that no longer resolves to what the platform actually applies. The alternative, a
  per-band scope-filtered cascade, is a coherent design and a much larger one (per-tier scopes
  threaded from the API, a rule for the default seed and the ancestor walk, redaction of shadowed
  candidates, a batch contract, and that regression rewritten). It is not ruled out forever; it is
  ruled out as a patch, and it would need a threat model that wants it.
