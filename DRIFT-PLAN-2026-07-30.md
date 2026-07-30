# Drift correction and prevention plan (2026-07-30)

Companion to `AUDIT-2026-07-30.md`. That file is the evidence; this file is the plan:
what we correct, which side of each contested claim wins, how shipping stops producing
new drift, how far the docs can move to a generated model, and how the corpus slims
down. Per the tracking doctrine, nothing here starts until its issue exists and its
scope is approved; section 7 is the proposed issue breakdown.

One observation drives the whole plan. The audit covered two fully generated artifacts
(the ERD, the CLI reference) and roughly seventy hand-written pages. The generated
artifacts audited clean; nearly every hand-written claim about a generated or built
surface drifted. The corrective is not "write more carefully." It is: **generate the
facts, lint the prose, fence the design, and keep hand-writing only the narrative.**

---

## 1. Surgical correction where the code is the source of truth

Default rule: for anything the code does today that is tested, deliberate, and not in
conflict with a recorded decision, the code wins and the prose moves. The audit's
Batches 1 through 7 (AUDIT section 5) are the work list. Sequencing, not content, is
the decision here:

**Wave 1, the honesty patches (small PRs, days).** Fix the claims most likely to
mislead someone acting on them, before any restructuring:

- The claims that are flatly false about built behavior: api.md's secret-floor and
  all-scope-directory claims, identity-access.md's cross-tier cascade and group-scope
  sections (mark Design), alarms-actions.md's terminal-upstream premise,
  storage.md's lineage CHECK block, properties.md's `instance` section, audit.md's
  missing read surface.
- The guides where a reader types a command and it fails: guides/cli.md,
  guides/admin/properties.md, guides/container-image.md, plus the vendors/drivers
  "products are not built" passages.
- The four badge corrections (messaging, workers, groups, glossary to `Partial`) and
  the dozen stale "Still Design" callout bullets.

These are surgical: sentence and section edits, no page restructuring, each PR
reviewable in minutes. Do them before the prevention tooling so the lints (section 3)
land on a clean baseline instead of a thousand pre-existing failures.

**Wave 2, the vocabulary sweep.** One PR: `datapoint` and the other retired names
(AUDIT Batch 1) across docs, console strings, seed comments, Makefile and proto
comments. Mechanical, large, low-risk. Lands together with the banned-vocabulary lint
so the sweep can never regress.

**Wave 3, the meta-layer.** decisions.md hygiene (renumber the duplicate ADR-0036,
rebuild the index, restore ordering, add the missing Date/Status/Pages fields,
remove the personal-name attribution), roadmap rewrite, status.mdx intro, the
architecture/index.md link and sidebar fixes. Also one PR, but it should follow the
decisions-format lint (section 3) so the format is enforced from then on.

Wave 1 and the P0 code fixes (section 2) can run in parallel; Wave 2 and 3 follow.

---

## 2. Where the code falls short: the judgment calls

The rulings below are recommendations for approval, not settled facts. Grouped by
which side wins.

### The code catches up (the design is right)

| Item | Ruling and rationale |
|---|---|
| **Delivery guarantees** (fire-and-forget publish, non-idempotent multi-tx ingest, #311) | **Fix the code, staged.** A monitoring product that silently loses and duplicates telemetry is failing its one job; the docs' at-least-once + dedup + idempotent-sink design is the correct target, not an overreach. Stage 1 (immediately, docs side): one honest sentence on workers/messaging/nodes stating today's contract, so the docs stop lying while the code catches up. Stage 2 (slice): JetStream publish with ack + `Nats-Msg-Id` from the node, retry buffer bounded like the self-log buffer. Stage 3 (slice): single-transaction sink (or per-sink idempotency keys) so redelivery is safe; needs a uniqueness key on `metric` and `event`. Stage 4 (decision): concurrency; the serial-dispatch dependency of the transition guard must be redesigned (keyed sequencing or an ingest-side advisory lock) before any horizontal-scale claim returns. Each stage is its own ADR-backed slice. |
| **`instance` identity** (writer stamps interface name; reconciliation and settlement read `""`) | **Fix the code, after a one-page ADR.** This is a genuine design gap, not a bug on one side: ADR-0038 instanced the verdict by interface, properties.md designed operator-authored sub-entity instances, and the readers assumed neither. Recommended decision: `instance` = the observing interface for observed provenance; reconciliation and settlement resolve per-interface and reduce (any-interface-agrees for settlement, worst-case for drift). The properties.md `key[instance]` authoring syntax moves to the Design fence until the extractor engine exists. |
| **`ListSecrets` scope injection** | **Fix the code.** The unscoped-then-filter N+1 violates a doctrine invariant ("scope injected on every applicable query"); the doc's claim of an all-scope directory is also wrong, so both move: gateway gets the scope-aware query, api.md documents the per-row-scoped list it actually is. |
| **Audit `old`/`new` read surface** | **Fix the code.** The columns exist and are written; the page's purpose ("changed to what?") is right. Expose them on `GET /audit-log` (admin tier already gates it) and render a diff in the console Audit drawer. Small slice. |
| **One-open alarm uniqueness** | **Fix the code.** Two pages already reason from "the one-open index"; a partial unique index on `(component_id) WHERE cleared_at IS NULL` is one migration and makes the documented invariant true before the rule engine starts raising alarms concurrently. |
| **`OMNIGLASS_SECURE_COOKIES`** | **Fix the chart and the docs.** Default it on in the Helm chart (previews and any documented deploy are TLS-fronted), document it in container-image.md and helm.md. Consider auto-detecting via `X-Forwarded-Proto` later. |
| **uuid-as-Id console regression** | **Fix the code.** ADR-0062 is explicit: the uuid is identity, the name is the address operators read. Render `name` in Id columns, filter on it, fix the created-row focus. One slice across the four catalog pages plus `refCell`. |
| **Registry namespace collision** (property/event last-write-wins) | **Fix the code.** The two-registry model is deliberate (ADR-0063); the flat map is an implementation shortcut. Enforce cross-registry name uniqueness at seed and registration time (409 on collision), and key the ingest lookup so a collision can never silently reroute. |
| **Schema CHECKs vs the model** | **Fix the code, one tightening migration:** drop `'group'` from `scope_kind` (until group scope ships), `'declared'` from the sample-table provenance CHECKs, `'log'` from `property_type.kind`, `'system'` from the secret owner CHECK (ADR-0052; with a guard against existing rows), plus the constraint-name collision artifacts from the uuid refactor. Each is a dead branch that invites a wrong write the resolvers then silently ignore. |
| **Dead grants and dead table** | **Fix the code.** Remove `platform_setting`, and remove the unrouted grants (`config:*`, `alarm:ack,snooze,resolve`, `unit:create`, `severity_level:create`, `source:create`) from `roles.yaml` until their routes exist; the Roles view currently teaches permissions that gate nothing, in a product whose Roles view is explicitly pedagogical. Re-add each with its feature slice. |
| **Guard-test prefix hole** | **Fix the test** (`docs_commands_test.go`): a documented command must resolve consuming every non-placeholder word. This is what let three broken invocations ship. |
| **Missing CI gates** | **Fix CI.** `make gen && git diff --exit-code` and an Astro docs build as PR checks. Both are claimed by contributing docs already; the audit is the proof they are needed. |

### The docs move (the code's shape is right, or the design was overtaken)

| Item | Ruling and rationale |
|---|---|
| **api.md's list conventions** (`filter`, `order_by`, `page_token`, `fields`, ETag) | **Docs move to the Design fence.** Good AIP aspirations, zero implementations, and the one real paginated route chose `before`+`limit` deliberately. Implement cursor pagination when a list actually needs it; do not retrofit for conformance. |
| **The error envelope** (`code`, `violations`) | **Docs adopt reality.** Huma's stock RFC-9457 model is serving fine across 141 routes; a custom envelope is cost without a driving consumer. Record as a small ADR so the aspiration is formally retired. |
| **The two-lane data plane** (raw/trusted streams, admission consumer, republish) | **Docs keep it, fenced, and stop describing it as built.** The single-stream inline consumer is the right thin cut at this scale; the two-lane split is a scaling design that should stay in the Design fence of messaging/workers/scaling with the trigger condition stated (when multi-consumer or replay lands). |
| **Cycle-safety in alarms-actions.md** | **Docs re-derive the argument.** The health rollup writing calculated-provenance state on alarm raise is correct behavior; the page's premise ("alarms never write samples") was too strong. The real invariant is "consequence writes are calculated-provenance and rules never route on their own consequences": write that down, with an ADR. |
| **"Views by default" / the projector story** | **Docs move.** ADR-0065 already ruled the `property` cache is the architecture of record; workers.md and storage.md prose catch up. |
| **`command` as a template declaration, `action` rows, `UpsertOfficial`, the registry `scope` ladder, `session_log`/`internal_log`, the `global` owner** | **Docs move.** All overtaken by shipped decisions (command pillar, `official` boolean, platform tier). The scope ladder specifically gets retired-or-fenced in one pass across properties/events/glossary. |
| **Cross-tier scope cascade, group-as-scope** | **Docs fence.** Both are still wanted (the worked examples are compelling) but the code's own-tier thin cut is deliberate and test-asserted. Fence them as Design with their trigger slices named. |

### Genuinely deferred judgment (raise as questions, not rulings)

- **CLI ergonomics:** the camelCase custom-method verbs (`setTag`) vs the kebab auth
  family, and `list` as the leaf for singleton reads (`component reachability list`).
  Mechanically derived, operator-hostile. Fixing means a naming ADR and a breaking CLI
  rename; the cost grows with adoption, so decide before any public release (#57).
- **Official-row refusal status** (422 on five registries, 409 on two): pick one; 422
  matches the majority and the validation framing.
- **`interface.node_name`** (uuid in a `_name` column): a rename migration is cheap
  now and conformant with ADR-0056; the only cost is churn in the collection code.

---

## 3. Prevention: gates, lints, and skill changes

The theme: every drift class the audit found by hand becomes a mechanical check that
runs in `make test` (so `/ship-slice`'s existing "fresh test" gate inherits it) or in
the skill checklist itself.

### 3a. A docs lint suite (`internal/docslint`, run by `make test` and CI)

Go tests that parse `docs/src/content/docs/**` and validate factual identifier classes
against their sources of truth. In descending value order:

1. **Banned-vocabulary lint.** A curated denylist (term, replacement, retiring ADR):
   `datapoint`, `caused_by_event_id`, `--mode node`, `ListView`, `UpsertOfficial`,
   the `global` owner, `value_type`-as-column, proto `Event`, `component_template`
   (until it exists), etc. Scans docs, `web/src` user-facing strings, and seed YAML
   comments. **Every vocabulary-retiring ADR must append its term in the same PR**
   (enforced by the ADR skill below). This single lint retires the audit's largest
   drift theme permanently. Allowlist escape: decisions.md and status.mdx may name
   retired terms (they are history).
2. **CLI-invocation lint.** The existing `TestDocsOnlyNameRealCommands` with the
   prefix hole closed, extended to also validate documented flags (`--id` vs
   `--name` class) against the cobra tree.
3. **Route lint.** `GET /api/v1/...` and `` `/resource` `` patterns in docs must
   appear in `api/openapi.json` paths (with a fenced-design escape, section 5).
4. **Permission lint.** `resource:action[:admin]` tokens in docs must exist in the
   route-derived permission universe or `roles.yaml`; also the inverse check that
   seeded grants gate a real route (this is the dead-grants finding, kept honest
   forever).
5. **Make-target, env-var, and file-path lints.** `make <target>` exists in the
   Makefile; `OMNIGLASS_*` exists in `internal/config` (and inverse: every config
   var appears in container-image.md); `internal/...`, `cmd/...`, `web/src/...`
   references resolve to real paths.
6. **Decisions-format lint.** Unique ADR numbers, numeric ordering, an index row per
   entry, required fields (Date, Status from the declared vocabulary, Pages),
   anchors resolvable. Would have caught every decisions.md finding.
7. **Storage-table lint** (after section 4's schema.json exists): any
   `table.column` or storage-table row in prose must exist in the introspected
   schema.

Build these in two slices: (1+2+6) first, they need no new infrastructure; (3+4+5)
second; (7) rides the generated-facts slice. Each lint starts warn-only for one PR to
flush stragglers, then becomes a red gate.

### 3b. `/ship-slice` additions

Amend the existing checklist (keep it one skill; a separate drift skill would not get
run):

- **Gate 4 gains teeth:** "docs consistent" changes from prose judgment to "the
  docs lint suite is green" plus two targeted sweeps: (a) grep the architecture
  pages the slice touches for `Still Design`, `later slice`, `not yet`, `soon`, and
  `deferred` and confirm none of those fences describe what this slice just built;
  (b) if the slice renames or retires any identifier, the denylist entry and the
  docs sweep land in the same PR (the rename ripple, mirroring
  `/storage-schema-change`'s gateway ripple).
- **New checklist line, the neighbor check:** a slice that flips behavior must grep
  the docs for the claim it falsified, not just update its own page (the
  alarms-actions premise died on a health slice; the vendors guide died on the
  products slice). Concretely: grep docs for the key nouns of the changed behavior
  and list the hits in the ship-review's Docs line.
- **Ship-review template:** the `Docs:` line gains `lint: green` and the
  `Status:` line gains `fences swept: <pages | n/a>`.

### 3c. A new `/record-decision` skill

Wraps every ADR: assign the next number (from the lint's authoritative count), write
the entry with required fields, add the index row, update the Pages it names, append
any retired vocabulary to the denylist, and re-run the decisions-format lint. This
removes the by-hand numbering that produced the duplicate ADR-0036.

### 3d. A periodic `/docs-audit` skill

Encode the fan-out audit that produced `AUDIT-2026-07-30.md` (the per-cluster
prompts are reusable verbatim) and run it quarterly or before a release tag. The
lints catch identifier drift; the periodic audit catches the semantic drift no lint
will (inverted claims, stale arguments, missing surfaces).

---

## 4. The generated-docs question

**Verdict: a full docstring-extracted docs model is the wrong target, but a hybrid
"generated facts, hand-written narrative" model is feasible, proven in-repo, and
should absorb most of the drift surface.**

Why not full generation: the architecture corpus is the product's pedagogy (doctrine
4); its value is narrative (why the cascade exists, what the exclusive arc means, how
scope composes). Godoc extraction produces reference, not teaching, and Go doc
comments cannot carry a cross-cutting argument that spans five packages. Nobody
drifts less by moving prose into comments; they just drift where there is no lint.

Why the hybrid works here, specifically: this repo already runs the model three
times, and all three audited clean.

- `data-model.md`: generator writes a marker-fenced region, a drift test
  (`internal/erd/drift_test.go`) fails the build if the schema moved.
- `reference/cli/index.md`: fully generated from the cobra tree by `cmd/docsgen`.
- `status.mdx`'s grid: an Astro component reading page frontmatter live.

The plan is to extend those exact patterns to the fact classes that drifted worst.
Proposed artifacts, all emitted by `make gen` into `docs/src/generated/*.json` and
rendered by Astro components (preferred over marker regions for tables: no merge
noise in prose files, styling for free, one source consumed by many pages):

| Generated artifact | Source of truth | Replaces (today's drift sites) |
|---|---|---|
| `schema.json` (tables, columns, types, CHECKs, FKs, per subsystem) | erdgen's existing introspection + cluster map | every hand-written "Storage" table at the bottom of ~10 architecture pages (a top drift class: wrong columns on identity, tags, variables, storage, events, alarms pages) |
| `api-surface.json` (route, method, permission stamp, request/response fields, description) | `api/openapi.json` incl. `x-omniglass-permission` | api.md's per-resource route and body tables; the per-page "Reading X" route lists |
| `seed.json` (roles with effective permissions, property/event/command types, secret types, standards, location types, vendors, drivers, products, capabilities) | `internal/seed/*.yaml` via the seed loader | every "seeded set" claim in guides and architecture pages (another top drift class: the phantom `cpu.utilization`, the wrong secret-type ids, role capability lists) |
| `config.json` (env vars, defaults, doc strings) | `internal/config` struct tags (add `doc:` tags) | container-image.md and helm.md env tables |
| `permissions.json` (the route-derived universe + role matrix) | openapi stamps + roles.yaml | identity-access.md's role table, guides/admin/access.md |

Doc-comment leverage that *is* worth taking: the Huma struct `doc:` tags already flow
to OpenAPI, then to the CLI reference and the SPA types. That is the repo's real
docstring pipeline. Two actions: fill the blank ones (the vendor/driver/product/
capability/location create inputs render empty flag docs today), and treat Huma
descriptions as linted docs (the stale "blob is left in place" description shipped
to operators through this pipeline; the neighbor check in 3b covers it, and the
banned-vocabulary lint should scan `internal/api` descriptions too).

What stays hand-written: the spine, why, per-subsystem narrative, the doctrines,
ADRs, guides' walkthroughs. Their embedded facts become component renders or
lint-checked claims.

Sequencing: `schema.json` + the storage-table replacement first (highest drift
density, introspection already exists), then `seed.json` (guides depend on it),
then `api-surface.json`, then `config.json`/`permissions.json`. Each is a normal
vertical slice: generator, drift test, component, page migration.

---

## 5. Slimming the corpus and separating design from actuality

The audit's structural finding: the badge system is page-granular but the pages are
not. Nineteen `Partial` badges each fence an arbitrary mix of built fact and future
design with ad-hoc callouts, and the callouts rot (a dozen stale "Still Design"
bullets). The fix is to make the fence structural and the granularity sectional.

**5a. A `design` fence with teeth.** A custom Starlight aside (`:::design`, styled
distinctly, e.g. "Target design, not yet built, see #issue / ADR") wraps every
unbuilt section on an otherwise-built page. The contract, enforced by the lint
suite: **outside a design fence, identifier lints run strict** (routes, tables,
permissions, commands must resolve); **inside a fence, existence lints are waived**
but vocabulary lints still apply (future design must still use current nouns). This
inverts today's model: instead of prose defaulting to aspiration with "built"
callouts, prose defaults to fact with fenced aspiration. The badge then derives its
meaning honestly: `Design` = the page is one big fence; `Built` = no fences;
`Partial` = mixed, and the reader can see exactly where.

**5b. The page diet.** Targets, in order of payoff:

- **Delete the per-page storage tables** (section 4 replaces them, and the
  relational skeleton already lives in data-model.md). Estimated cut: 300+ lines of
  the highest-drift-density content in the corpus.
- **One home per concept.** The cascade is currently explained on cascade.md,
  variables.md, tags.md, and groups.md; scope on identity-access.md, storage.md,
  and core-entities.md; the two-lane data plane on messaging.md, workers.md,
  scaling.md, storage.md, and properties.md (where it drifted five separate times).
  Nominate the owning page, reduce the others to one linking paragraph. The
  duplication is not just length: it is why one decision (ADR-0065) had to be
  hand-applied to twelve pages and was not.
- **Demote the pure-design pages** (`ai`, `time`, `calculations`, `expressions`,
  `templates`, `views`, and post-fence remainders of `messaging`/`workers`) into a
  separate sidebar group, "Design sketches," visually distinct from the
  architecture of record. They keep their content (slimmed) but stop implying
  actuality by their location. When one starts building, its built sections
  graduate onto the record page.
- **Split status.mdx**: the grid and legend stay; the 138-entry build log moves to
  its own page (or per-epic pages) with date/epic headings and stable anchors.
- **Prune the glossary** to terms that exist in code (linted), with design-only
  terms moving to their sketch pages. The glossary becomes the lint's friend
  instead of its largest offender.

Measured goal: architecture section word count down ~40%, zero unfenced unbuilt
claims, and every remaining page either fully strict-linted or explicitly a sketch.

**5c. What not to do.** Do not collapse the architecture into generated reference
(loses the pedagogy that is doctrine 4), and do not fork "design docs" per feature
into a parallel tree that will itself rot; the fence keeps design adjacent to the
fact it aspires to change, which is what keeps it updated.

---

## 6. Sequencing summary

| Phase | Content | Depends on |
|---|---|---|
| 0 (immediate) | Wave 1 honesty patches; CI gates (`make gen` drift, docs build); guard-test hole; banned-vocabulary + CLI + decisions-format lints (warn-only) | approval of this plan |
| 1 | P0 code fixes as slices: settlement/`instance` ADR + fix, `ListSecrets`, audit diff read, alarm unique index, secure cookies, uuid-as-Id console, registry uniqueness, CHECK tightening, dead grants/table | 0 |
| 2 | Vocabulary sweep + lints to red; Wave 3 meta-layer (decisions, roadmap, status intro, spine links); `/ship-slice` amendments; `/record-decision` skill | 0 |
| 3 | Generated facts: `schema.json` + storage-table replacement, then `seed.json`, `api-surface.json`, `config.json`; remaining lints (routes, permissions, env, paths, storage) | 2 |
| 4 | Design fences + page diet + sketch demotion + status log split + glossary prune | 3 |
| 5 (staged, parallel) | Delivery-guarantee stages 2-4 (publish ack, idempotent sink, concurrency decision) | 1 (stage 1 note ships in phase 0) |
| ongoing | `/docs-audit` quarterly | 2 |

## 7. Proposed issue breakdown (for approval before any branch)

- **Epic: drift honesty pass** — one issue per Wave-1 PR (approx. 6 small PRs).
- **Epic: drift prevention tooling** — CI gates; `internal/docslint` slice 1
  (vocabulary, CLI, decisions); slice 2 (routes, permissions, make/env/paths);
  ship-slice + record-decision skill changes.
- **Epic: telemetry integrity** — the instance ADR + reconciliation/settlement fix;
  delivery stages 2, 3, 4 as separate issues; registry namespace uniqueness.
- **Epic: scope and secrets integrity** — ListSecrets scoped query; secret system
  band removal (CHECK + UI + guide); CHECK tightening migration; dead grants/table
  removal.
- **Epic: console address honesty** — uuid-as-Id fixes; blank Huma `doc:` tags;
  audit diff drawer.
- **Epic: generated docs facts** — schema.json; seed.json; api-surface.json;
  config.json/permissions.json (one issue each).
- **Epic: corpus restructure** — design fence component + lint contract; one-home
  dedup; sketch demotion; status log split; glossary prune; decisions/roadmap/spine
  refresh.
- **Deferred decisions to schedule:** CLI verb naming ADR (before #57), official-row
  refusal status, `interface.node_name` rename.
