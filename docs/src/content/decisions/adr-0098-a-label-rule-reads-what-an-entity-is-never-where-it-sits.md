---
title: "A label rule reads what an entity IS, never where it sits"
date: 2026-08-10
status: "Accepted; the placement exclusion reversed by [ADR-0100](#adr-0100-a-label-cascades-where-the-blast-radius-is-a-placement-and-waits-for-the-verb-where-it-is-the-estate), amended (#729)"
summary: "A label rule is Go `text/template` over a **closed `map[string]string`** AND a **closed grammar**: the sandbox is the data map (a secret is absent rather than filtered) plus an allowlist over the parsed tree (a closed set of node types and function names, so `printf` and friends are refused at rule-edit time; a length cap bounds output, which is not the same as bounding work). The map carries the entity's own columns and its resolved classification facts, and deliberately **no placement**: every input then changes on exactly five of the entity's own acts (create, rename, move, reclassify, reset), which is what makes the stored label's recompute-and-compare invariant provable. Exposing a location's name would put a location rename in that set and stale every label under it. The global tier is one row per entity kind with TWO columns, `default_template` (boot-seed space, authoritative) and `template` (operator space, nullable). **Amended (#729):** the key table in the entry is the map as of this decision; each kind's keys are now declared once in `internal/storage/label_keys.go` beside the accessor that produces each value, the map is built by ranging that declaration, and the docs render it from `docs/src/generated/labeldata.json`, so the taught set and the reachable set cannot disagree"
---

- **Date:** 2026-08-10 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [storage](/architecture/storage/), [glossary](/architecture/glossary/)
- **Decision:** A label rule is an operator-authored Go `text/template` evaluated over a **closed,
  flat `map[string]string`** and a **closed FuncMap** (`title`, `upper`, `lower`, `slug`). The map
  carries the entity's own columns and the facts resolved from its **classification** chain, and
  carries **nothing about its placement**: not its location, not its parent, not the system it
  staffs.

  | entity | keys |
  | --- | --- |
  | component | `Name`, `Ordinal`, `TypeName`, `TypeAbbrev`, `Stem`, `ProductName`, `VendorName` |
  | system | `Name`, `TypeName`, `TypeAbbrev`, `Stem`, `StandardName` |
  | location | `Name`, `TypeName` |

- **Decision (the sandbox is the data map AND the grammar):** Two allowlists, the same argument
  twice.

  The DATA half filters nothing. The value type is `string`, so field traversal and a method call
  fail at execution rather than reaching further; a key the map does not carry renders as the empty
  string (`missingkey=zero`, set explicitly rather than relied on); a secret, a credential and a
  token are absent **structurally**, never put in. That is why a general template engine beat a
  custom interpolation DSL: the security property a label rule needs is not a syntax that cannot
  express dangerous things, it is an environment that contains nothing dangerous, and the environment
  is ours entirely. Adding a key is the only way to widen what a rule can read, so the key set is
  pinned by a test.

  The GRAMMAR half is an allowlist over the **parsed tree**, checked at parse time, admitting a
  closed set of node types (text, action, pipeline, command, field, dot, identifier, literal,
  `{{if}}`, comment) and a closed set of function names (`title`, `upper`, `lower`, `slug`, plus
  `and`/`or`/`not` and the six comparisons). Everything else text/template offers is refused:
  `printf`, `print`, `println`, `call`, `index`, `slice`, `len`, `html`, `js`, `urlquery`, variable
  declarations, `{{range}}`, `{{with}}`, `{{template}}` and `{{define}}`.

  **A superseded claim, corrected here:** an earlier draft of this entry said `printf`'s width verb
  was "the one way a closed map of short strings still produces output far larger than its inputs"
  and that the ceiling therefore lived in a cap on rendered length. Both halves were wrong. The cap
  bounds bytes reaching the WRITER, and a value built inside a pipeline is materialized by `fmt` and
  never written, so the cap never sees it: 437 bytes of operator-authored rule allocates 85 MB and
  writes 8, with every further doubling 35 more bytes of rule for twice the memory, which OOMs the
  single binary and re-triggers on every later write to any entity of that type. Refusing `:=` does
  not fix it (the nested-pipeline form needs no assignment) and neither does removing a FuncMap entry
  (`printf` is a builtin the FuncMap never granted). A closed grammar does, and it does it at
  rule-edit time so nothing is ever stored. The length cap stays as a ceiling on OUTPUT, which is now
  reachable only by literal text an operator typed.

- **Context (why placement is excluded):** The label is **stored**, so a stored value must equal what
  its rule produces, and every input to a rule is therefore something a write path has to re-render
  on. The keys above change on exactly five acts, all of them the entity's own: create, rename, move
  (which re-mints the ordinal), reclassify, and reset. That closed set is what makes the
  recompute-and-compare invariant provable rather than hopeful.

  Adding the location's name would add a sixth, and it would not be the entity's own: renaming a
  **location** would silently stale every label under it, and the invariant would be right to fail.
  The epic's acceptance text ("a component created with a product and a location gets a label")
  reads either way, and is satisfied by the classification alone. The cascade that would make
  placement facts safe belongs with the slice that owns bulk recompute
  ([#685](https://github.com/hyperscaleav/omniglass/issues/685)), which can then add the keys and the
  cascade together. Ancestry is not lost meanwhile: it is already the entity's `path` and its two
  renders, beside the label rather than inside it.

- **Decision (the global tier is two columns, not one):** The global rule is one row per labelled
  entity kind in `label_rule`, carrying `default_template` (boot-seed space, rewritten
  authoritatively on every start) and `template` (operator space, nullable), resolved as
  `coalesce(template, default_template)`. Clearing `template` is restore-to-defaults.

  One column could not be both. An authoritative boot seed over a single column stomps an operator's
  rule on the next restart; a seed-if-absent freezes the shipped default at whatever the first boot
  wrote and no release can improve it. That is the problem
  [ADR-0095](#adr-0095-an-operator-forks-a-shipped-registry-row-instead-of-the-platform-writing-it)
  solves for a shipped registry row, and this is the same answer at the smallest scale that expresses
  it: shipped values and operator values live apart and reads resolve one over the other. It is two
  columns rather than a `registry_shadow` row because this table is keyed on an entity kind rather
  than a uuid, and three rows do not earn an overlay of their own.

- **Decision (a rule inherits per node, like every other type fact):** On a nested registry a
  `label_rule` is nullable and resolves by the first-non-null walk `stem`, `icon` and `abbrev`
  already follow, crossing the fork boundary **per node** with the chain always the official one
  (ADR-0095 decision 3). So forking a shipped ancestor's rule reaches every descendant that declares
  none, and a descendant that declares one overrides for itself alone. The analogous question
  ADR-0095 settled for facts has the same answer for rules, because a rule **is** a fact of the node
  that declares it.

- **Decision (an empty render stores NULL and keeps the pen):** A rule with nothing to say about a
  row stores SQL `NULL`, not a blank string, and `display_name_generated` stays true. Marking the row
  operator-owned because a rule once rendered nothing would exclude it forever from the recompute
  that exists to fix exactly that.

- **Decision (the pen is backfilled, claimed only where there is no label):** The migration adding
  `display_name_generated` defaults it false, and a one-time backfill then claims it on every
  existing row whose `display_name` is `NULL` or empty, leaving it false where the operator typed
  something. A row with no label has nothing to protect and is the majority case; left unclaimed it
  is inert forever, because the write-path stamp returns immediately when the pen is false and a bulk
  recompute only touches rows the platform already owns. The backfill writes the PEN and not the
  label, since rendering needs the rule engine and logic lives in Go, so those rows take their labels
  from the next write that touches them or from the recompute that can now see them.
- **Consequence:** `label_rule` columns land on `component_type`, `system_type`, `location_type`,
  `product` and `standard`, but only `component_type` carries the fork today, so a rule on a
  **shipped** row of the other four is not operator-editable until each adopts ADR-0095's primitive.
  That is the pre-existing state of those registries rather than a new restriction, and the tier that
  matters most for components (`component_type`) is forkable now.
- **Amended (#729):** the key table above is the map **as of this decision**, and it is a historical
  snapshot rather than the live set: [ADR-0100](#adr-0100-a-label-cascades-where-the-blast-radius-is-a-placement-and-waits-for-the-verb-where-it-is-the-estate)
  added the placement keys and #686 gave a system its `Ordinal`. The map is no longer typed out
  anywhere. Each kind's keys are declared once in `internal/storage/label_keys.go`, beside the
  accessor that produces each value, `labelData` builds the map by ranging that declaration, and
  `docs/src/generated/labeldata.json` (`make gen`, via `cmd/labelgen`) renders the per-kind tables
  [core entities](/architecture/core-entities/) publishes. So "the key set is pinned by a test" above
  is now pinned by CONSTRUCTION, and what the tests hold is the artifact against the declaration and
  the two `label_rule` API descriptions against it too. That last one was already wrong: both
  enumerated the map as it stood before ADR-0100, so the console's field help and the CLI reference
  taught seven keys where a rule could read nine.
- **Tracked under** [#682](https://github.com/hyperscaleav/omniglass/issues/682), the second slice of
  epic [#657](https://github.com/hyperscaleav/omniglass/issues/657), amended by
  [#729](https://github.com/hyperscaleav/omniglass/issues/729).
