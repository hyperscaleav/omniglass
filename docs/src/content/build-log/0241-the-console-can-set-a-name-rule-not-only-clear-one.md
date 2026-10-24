---
title: "The console can set a name rule, not only clear one"
date: 2026-08-14
refs: []
form: bullet
---

- **The console can set a name rule, not only clear one.** A [location type](/guides/admin/location-types/)
  carries an optional **name rule**, and having one is the type's opt-in to the platform naming the
  locations it classifies. An earlier slice gave the console the exit and no entrance, which made the
  affordance half a control: it could undo a state it could not create. It mattered more than an
  ordinary gap, because after ADR-0103's reversal **no shipped location type carries a rule**, so
  generated location names were reachable only through the API or the CLI and the capability shipped
  effectively unreachable for the operators it was built for.

  A rule is a **declaration** rather than a template
  ([ADR-0102](/architecture/decisions/#adr-0102-a-name-rule-is-a-declaration-a-type-opts-in-with-and-a-rule-change-renames-nothing)),
  so the editor is small: a tick box, a stem, and the flag that suppresses the number on the first of
  that stem under one parent. The rule's PRESENCE is the opt-in, so the tick box is its existence
  rather than a fourth field, and the clear moves inside the editor: unticking it is what turns
  naming off. That write still spells the clear the one way the wire has for a nullable object, the
  field named in `update_mask` with no rule in the body, and because a mask governs the whole write
  it names every field the blade writes rather than `name_rule` alone.

  **What the editor says a rule will produce comes from the server.** A `name_rule` now reads back
  with `examples`, the first two names it mints, produced by the same `nameMint` a create allocates
  from, and the blade composes its sentence around those two strings: "Named wing, then wing-2,
  within their parent." Nothing under `web/` knows that a counted name is `<stem>-<n>` any more,
  which is the answer #702 took for the drafted name applied to a rule rather than to a row. An
  UNSAVED rule is told nothing about its output on purpose, since the only honest source is the mint.
  The stem's character rule is restated in TypeScript and refuses the save inline (ADR-0113); the
  90-character ceiling is not, because that number is arithmetic over the mint's output space rather
  than a name rule.

  **Two things this found.** The mint's own refusal, a rule that mints legally at ordinal 1 and
  illegally at the ceiling, is unreachable through this body: `stem` carries `maxLength: 90` and 90
  plus the widest provable ordinal is exactly the 100-character name cap, so every stem the schema
  admits is legal at both ends. The refusals a surface renders are therefore the schema's, and the
  console had been rendering none of them: Huma's `detail` for a schema 422 is always the literal
  "validation failed", with the field and the reason in `errors[]`, which `describeError` dropped on
  every console surface. It appends them now.

  Proven at the wire by comparing the served examples with the names two nameless creates actually
  get, on all three rule shapes, and by opting the shipped `floor` type in: the edit forks the
  platform's row, a nameless create then stamps the forked rule's first example, and `:restore`
  takes both the fork and the rule away.
