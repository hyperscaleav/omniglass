---
title: "The friendly string an operator reads is a `label`, and unset is SQL NULL"
date: 2026-08-14
status: "Accepted"
summary: "`display_name` becomes **`label`** on all twenty-three tables that carry it, and `display_name_generated` becomes **`label_generated`** on the three that carry a pen. The rename **finishes a word rather than introducing one**: `internal/label/` is the rule engine, `label_rule` is a column on five tables plus a table of its own, and the API already says `:renderLabel` / `:previewLabels` / `:recomputeLabels`, so until now a route called `:recomputeLabels` wrote a column called `display_name`. The column is **nullable** and unset is **SQL NULL and nothing else**, normalized in **Go at the gateway write path** (`labelOrNull` / `labelPatch`) rather than by a constraint. This slice first shipped the opposite, `NOT NULL DEFAULT ''`, and **reversed it before merge on the strength of its own diff**: the floor forced all seven registry orderings to be spelled `order by nullif(label, '') nulls last`, and a read that has to convert the stored representation back is the schema saying which value it should have stored. The argument for the floor had a hole worth recording: it claimed collapsing unset on a nullable column needs a `CHECK`, but this repo puts that class of rule in Go (ADR-0110 had just moved `principal_label` out of the database), and a Go rule maps empty to NULL as cheaply as to `''`. The migration is the smaller half; what makes NULL the ONLY spelling is the Go normalization, proved by a sweep that drives every write path with a WHITESPACE label and reads the raw column, complete by construction (it caught `CreateStandard` still binding raw). A PATCH carries a **set flag beside the value**, since clear and leave-alone both arrive as NULL. Reads coalesce, so the wire keeps a plain string and no generated artifact moves but the schema facts. Orderings are `order by label nulls last, name`; the two shadow-resolved registries resolve on jsonb **key presence**, so a fork that cleared its label sorts at the end rather than under the official one. `stem` and `abbrev` are NOT swept (their NULL means inherit-from-ancestor). `registry_shadow.image` is the one place the old word survived as DATA; `audit_log`'s images are left as the record they are"
---

- **Date:** 2026-08-14 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/), [storage](/architecture/storage/), [the API](/architecture/api/), [identity and access](/architecture/identity-access/), [glossary](/architecture/glossary/), [design system](/contributing/design-system/)
- **Decision:** `display_name` becomes **`label`** on all twenty-three tables that carry it, and the
  pen beside it becomes **`label_generated`** on the three that carry one. The column is
  **nullable**, and unset is **SQL NULL and nothing else**, enforced in Go at the gateway write
  path rather than by a constraint. Breaking wire change: the JSON field, the CLI flag
  (`--display-name` becomes `--label`), and the console's field label move with it. The wire itself
  keeps a plain string, because every read projection coalesces.
- **The rename finishes a word rather than introducing one.** When it was first proposed, "label"
  was a candidate. It is now the system's vocabulary everywhere except the column it named:
  `internal/label/` is the rule engine ([ADR-0098](#adr-0098-a-label-is-rendered-from-a-rule-and-a-name-is-not)),
  `label_rule` is a column on five tables plus a table of its own, the API says `:renderLabel`,
  `:previewLabels` and `:recomputeLabels`, and forty console files say `entityLabel`, `labelPen` or
  `labelGenerated`. Until this decision a route called `:recomputeLabels` wrote a column called
  `display_name`, and the console's `labelGenerated` read `display_name_generated`.
- **The pen renames in the same sweep or not at all.** `display_name_generated`
  ([#657](https://github.com/hyperscaleav/omniglass/issues/657)) is what answers "did an operator
  type this, or did the platform render it from a rule". Slice 2 of that epic deferred its naming
  here on exactly these grounds. It stays `NOT NULL DEFAULT false`: it answers a different question,
  and there is no third state for it to hold.
- **Nullable, and the argument is the useful part.** This slice first shipped the opposite,
  `NOT NULL DEFAULT ''`, and reversed it **before merge on the strength of its own diff**. The
  evidence was in the code the floor forced: every one of the seven registry orderings had to be
  spelled `order by nullif(label, '') nulls last, name`. A `nullif` in a read is the read path
  converting the stored representation back into the one it wanted, and seven of them in a single
  slice is not a coincidence, it is the schema telling you which value it should have stored.
  Postgres already has a value for "there is nothing here", `nulls last` is already the default for
  an ASC sort, and the orderings collapse to `order by label nulls last, name` the moment the column
  can hold it. The same test that made the ordering hazard visible stayed red through the reversal
  and green after it, against the new spelling, rather than being assumed equivalent.
- **The argument FOR the floor had a hole, and it is worth recording because it is the kind that
  recurs.** It ran: collapsing "unset" on a nullable column needs a `CHECK`, and
  [#570](https://github.com/hyperscaleav/omniglass/issues/570) is already filed against that
  pattern, therefore the empty string. But this repository puts that class of rule **in Go at the
  gateway write path**, which is exactly where [ADR-0110](#adr-0110-a-principals-identifier-is-the-gateways-answer-not-a-stored-functions)
  had just moved `principal_label` out of the database. Once the normalization is in Go it can map
  empty to NULL as cheaply as to `''`, so the choice was never "a CHECK or the empty string": the
  premise smuggled in the assumption that the rule had to live in the schema.
- **What makes NULL the ONLY spelling is the Go half, not the migration.** The migration drops the
  defaults and converts the rows that exist; on its own that trades one two-state mess for another
  the first time a write binds `""`. `labelOrNull` and `labelPatch`
  (`internal/storage/label_unset.go`) are the one place the decision is applied: they trim, and an
  empty result is NULL. Every write path binds through them, and a behavioural sweep drives each of
  those paths with a **whitespace-only** label and reads the RAW column back, because every
  projection coalesces and would report NULL and `''` as identical. It is complete by construction:
  a labelled table with neither a prover nor a declared reason fails the suite. That completeness
  is not decoration. It caught `CreateStandard` still binding its label raw after every other path
  had been converted, and it caught it as an ordering failure two registries away from the defect.
- **A PATCH needs two parameters, and that is where the empty string still means something.**
  Clearing a label and leaving it alone are different instructions that both arrive as SQL NULL, so
  `coalesce($n, label)` cannot express them. Each statement now carries a **set flag beside the
  value**, appended to the parameter list so no existing placeholder is renumbered. The empty string
  keeps its wire meaning ("clear it", the house convention
  [ADR-0114](#adr-0114-an-inherited-registry-fact-clears-with-the-empty-string-and-the-pattern-is-what-admits-it)
  settled for every other nullable string); what changed is that it is translated in Go rather than
  stored.
- **The read side keeps a plain Go string.** Every read struct's `Label` is a `string` and every
  patch struct's is a `*string`, checked by enumerating the declarations rather than assumed, so
  every read projection is `coalesce(label, '')` and nothing above the gateway learns that the
  column is nullable. That is why this reversal moves no generated artifact except the schema facts:
  the OpenAPI, the typed client and the CLI are unchanged. One trap in doing it: a `coalesce` inside
  a **CTE** drops the output column's NAME, so the two that feed a named reference downstream are
  spelled `coalesce(r.label, '') as label`.
- **The load-bearing consequence is still the ORDERING, and it was already broken before either
  spelling.** All seven registry orderings read this column, and all seven of those tables were
  storing `''` for unset, so all seven already floated unlabelled rows to the TOP of the picker.
  They are `order by label nulls last, name` now. `nulls last` is redundant on an ASC sort and
  written anyway, because it is the half that is a decision rather than a default. The two
  shadow-resolved registries resolve on the **presence of the jsonb key** rather than with a
  coalesce, so a fork that CLEARED its label sorts at the end with the other unlabelled rows instead
  of under the official row's label. The console re-sorts every registry list client-side, so the
  rule is written twice and both halves are pinned: `byLabel` in `web/src/lib/entities.ts`.
- **`stem` and `abbrev` are deliberately NOT swept.** Their `NULL` means *inherit from the nearest
  ancestor that sets one* ([ADR-0114](#adr-0114-an-inherited-registry-fact-clears-with-the-empty-string-and-the-pattern-is-what-admits-it)),
  a third state with real content, and it is a different question from whether a row has a label.
- **One place the old word survived as DATA rather than as an identifier.** `registry_shadow.image`
  is an operator's forked copy of a shipped registry row, stored as jsonb keyed by column name, and
  a column rename cannot reach inside a jsonb value. An operator who had relabelled a forked
  `component_type` or `location_type` would have had that relabelling silently ignored the moment
  the gateway started asking for `image->>'label'`. A cleared label survives there as a PRESENT key
  holding JSON null, never as a dropped key: dropping it would re-inherit the official row's label,
  which is the opposite of what an operator who cleared it asked for. `audit_log.old` /
  `audit_log.new` are deliberately left alone: they record what a row looked like under the field
  names it had at the time, and rewriting them would falsify the record.
- **The word now collides with the console's own `label`, and two source guards were re-anchored
  rather than suppressed.** `label` was already the name of the prop a Button, an InfoTip, an
  InlineActions row, a nav item and a facet chip take, so the one-renderer guard's patterns matched
  eleven pieces of ordinary UI chrome the day the column moved. The fallback pattern now excludes
  the exact read `props.label` and nothing wider (a component handed an entity reads
  `props.entity.label`, which still matches), and the interpolation pattern is anchored to an
  identifier read on the same line, which is the shape of the search-haystack bug it exists for.
  "Display name" joins "Technical name" and "Segment" on the console's retired-word list, and
  `display_name`, `--display-name` and the two-word noun join the docs denylist.
