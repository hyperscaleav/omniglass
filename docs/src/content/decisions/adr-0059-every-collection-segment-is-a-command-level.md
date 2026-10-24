---
title: "Every collection segment is a command level"
date: 2026-07-22
status: "Accepted"
summary: "Every collection segment is a command level"
---

- **Date:** 2026-07-22 | **Status:** Accepted | **Pages:** [api-first](/contributing/api-first/), [CLI guide](/guides/cli/)
- **Decision:** the CLI command path is derived from the **whole route**: every collection segment
  contributes a level and the verb is last, so a subresource is always addressed under the resource
  that owns it. `/components/{name}/properties` is `component property list`;
  `/principals/{id}/grants` and `/principal-groups/{id}/grants` are `principal grant create` and
  `principal-group grant create`.
- **Context:** the old rule used only the collection **nearest the leaf**, so it could not tell two
  parents apart. Across 195 operations it produced **24 collisions**, `property list` seven ways.
  Cobra does not treat a duplicate name as an error: it registers both and returns the first, so the
  second was unreachable and the only symptom was a command that ran the wrong thing. Granting a role
  to a principal had no CLI path at all ([#357](https://github.com/hyperscaleav/omniglass/issues/357)).
- **`nameOverride` was the rule, written out by hand fifty times.** It had grown to 53 entries, and
  the comment on nearly every one said the same thing: "the leaf-noun heuristic would collapse both
  into one group." Each was added after somebody typed a broken command. It is now **14 entries**,
  all of them the genuinely non-AIP `/auth` family, and none of them about a collision.
- **A name depends only on its own route.** This is the property worth having: under a
  disambiguate-only-when-ambiguous rule, adding a route could rename an existing command. Here it
  cannot, so the naming is stable as the API grows.
- **The grouping had to become a tree.** Fixing the derivation alone was not enough: the generator
  bucketed commands by their first word and used only the **last** word as the leaf name, so a
  three-word path rendered as two and collided again. It now builds an N-level tree, which is also
  what makes `node run` and `type secret list` render as written.
- **`omniglass statu` shipped.** The depluralizer took `-s` off `status`. A small irregular set is
  declared instead; the route vocabulary is ours, so this is a known list, not an English problem.
- **Cost accepted:** 67 of 202 commands are renamed, 135 unchanged. At v0.0.0 that is a docs edit,
  and the guides are corrected mechanically from the route map in the same change.
- **Two guards, because regeneration does not fix prose.** `TestNoCommandNameCollisions` fails on any
  duplicate name (its known-collision list is now **empty**, and its second half forced those entries
  out once fixed). `TestDocsOnlyNameRealCommands` walks the guides and fails on a documented command
  that does not resolve; it immediately found `omniglass secret-type list`, which had never existed in
  any build, and two commands with no API route behind them
  ([#359](https://github.com/hyperscaleav/omniglass/issues/359)).
- **Supersedes** the naming half of [ADR-0058](#adr-0058-a-run-mode-is-a-verb-under-its-noun-and-no-command-may-be-shadowed),
  whose guard this keeps and whose exception list this empties.
- **Tracked as** [#357](https://github.com/hyperscaleav/omniglass/issues/357).
