# The anti-pattern catalog

Seeded from the 2026-07-30 audit's failure classes. Each entry: the detection cue the
reviewer sweeps with, the failure it causes, and the fix pattern. **Living artifact:**
a slice that fixes a class flips its status here in the same PR; a review that finds a
new class appends one. Entries are never silently deleted; a fixed class is marked
`retired` with the fixing PR, because it can regress.

## unscoped-gateway-list (retired 2026-08-01 by #458; watch for regressions)

- **Cue:** a Storage Gateway read that takes a scope/actor and does not bind it into the
  SQL WHERE clause; per-row filtering in Go after an unscoped SELECT.
- **Failure:** the ABAC invariant ("scope injected on every applicable query") is
  violated; rows the actor must not see are fetched and can leak through a later refactor
  or a miscounted filter; N+1 cost besides.
- **Fix:** inject the scope in the query itself. Exemplars: the scoped-CRUD primitive
  (`scopedListSQL`) for single-tier trees, and the arc-scope primitive
  (`arcScopeCTEs` / `arcScopePredicate` in `internal/storage/scopetree.go`, consumed by
  `ListSecrets`) for rows owned on the exclusive arc. The last known instance
  (`ListSecrets`'s per-row Go filter) was fixed by #458; new gateway reads must start
  from one of the two primitives.

## missing-audit-row (live)

- **Cue:** a new privileged mutation (estate or IAM write) whose handler does not write
  the audit row in the same transaction; a new auth event path with no `WriteAuthEvent`.
- **Failure:** a committed change with no accountable actor; under impersonation, the
  real actor is lost. The audit page's promise ("who changed this?") silently breaks.
- **Fix:** `writeAuditRes` in the mutation's transaction; `WriteAuthEvent` on the
  read/no-tx auth path. Ship-slice gate 10 is the rollup backstop; catch it per slice.

## missing-permission-stamp (live)

- **Cue:** a new route registered without the permission gate; a permission string that
  exists on no seeded role; a route whose stamp disagrees with the nav gate that links it.
- **Failure:** an ungated route is reachable by any session (the `POST /nodes:claim`
  allow-list is the one deliberate exception); a dead stamp gates nothing and teaches a
  phantom permission in the pedagogical Roles view.
- **Fix:** every route carries its `<resource>:<action>` gate at registration; the
  permission resolves against the seeded role matrix.

## uuid-as-address (retired 2026-08-02 by the #432 console slices #466/#467/#469/#470; watch for regressions)

- **Cue:** an operator-facing surface (console cell, filter, delete confirm, CLI output)
  rendering or matching a uuid where ADR-0062 says the `name` is the operator address;
  a `_name`-suffixed column or field holding a uuid.
- **Failure:** typing the human handle finds nothing; confirms print opaque ids; created
  rows lose focus because the form keys by handle while rows key by uuid.
- **Fix:** uuid is identity, name is the address: render, filter, and confirm on `name`.

## non-idempotent-ddl (live)

- **Cue:** a migration without `IF NOT EXISTS`/`IF EXISTS` guards; a column rename
  without the catalog-guarded `DO` block; any edit to an already-applied migration file;
  seed rows inside a schema migration.
- **Failure:** partial-state databases fail mid-migrate; an edited applied migration
  silently diverges environments (dbmate keys on the version, not the contents); a
  schema squash drops the smuggled seed rows.
- **Fix:** the `/storage-schema-change` skill is the procedure; new migration files only.

## fixed-testcontainer-port (live)

- **Cue:** a test binding a literal host port; a testcontainer with a fixed port mapping.
- **Failure:** parallel runs and CI collide; the suite is flaky in exactly the way that
  erodes trust in the gate.
- **Fix:** testcontainers-go's random ephemeral ports, address read from the container.

## react-intuition-solid (live, a false-finding class)

- **Cue:** a review claim about `web/src` that assumes React semantics: destructured
  props losing reactivity "bugs" that are actually correct Solid, missing dependency
  arrays, keyed-list re-render assumptions, `()` signal calls flagged as errors.
- **Failure:** wrong findings block correct code, or "fixes" break reactivity.
- **Fix:** load the `solidjs` skill before judging; a finding that assumes React
  semantics is invalid.

## falsified-neighbor-claim (live)

- **Cue:** the diff flips a behavior and no docs grep for the old claim's key nouns is in
  evidence; only the slice's own page was updated.
- **Failure:** a neighboring page keeps teaching the falsified claim (the
  alarms-actions cycle-safety premise died on a health slice; the vendors guide died on
  the products slice).
- **Fix:** grep `docs/src/content/docs` for the changed behavior's nouns; update or file
  per hit; list hits in the ship-review Docs line.

## exactly-once-assumption (live; #430 tracks the contract)

- **Cue:** new bus code that publishes fire-and-forget to a JetStream subject, assumes
  redelivery cannot happen, or writes sinks in multiple transactions before a single ack.
- **Failure:** samples silently lost on a JetStream outage; duplicates on redelivery
  into sinks with no uniqueness key (the audit's P0-1).
- **Fix:** until #430's staged contract lands, new code must not widen the gap: publish
  with ack where the API allows, and never assume dedup exists downstream.

## hand-written-fact (live)

- **Cue:** the diff adds a hand-written artifact restating a fact the code already knows:
  a route/column/env-var/seeded-row table, a copied schema, an asset a generator could
  emit.
- **Failure:** drift waiting to happen; the audit found the generated artifacts were the
  only ones that had never drifted.
- **Fix:** the generate-first rule (CLAUDE.md): a generated render in the same PR, or a
  filed issue named in the ship-review.

## mocked-sut (live)

- **Cue:** a test that mocks the system under test, mocks the database in an integration
  tier, or proves a DB-backed behavior only under `-short` or a cached run.
- **Failure:** a green gate that verifies the mock, not the behavior; the exact class the
  capability-wrapping carve-out exists to prevent.
- **Fix:** real Postgres via testcontainers for integration; fakes only at the
  environment-risky capability seam, with the real-implementation test before merge.

## house-style (live)

- **Cue:** an em dash in any written artifact; AI/assistant attribution in a commit, PR
  body, code comment, or visible artifact; a head-noun-first name that breaks the
  `<qualifier>_<genus>` convention.
- **Failure:** the ship-slice scan red-gates it at rollup; catching it per slice is
  cheaper.
- **Fix:** commas, colons, periods, parentheses; no attribution; glossary-conformant
  names.

## swallowed-build-gate (live; found by the #480 review)

- **Cue:** a claim that some check "fails the build" backed only by a remark/rehype
  `file.fail` (or any error raised inside Astro's content-layer render); no test or
  script proves the build's exit code actually goes nonzero.
- **Failure:** the Starlight docs loader catches per-page render errors, logs them, and
  builds the page empty with exit 0, so the "gate" publishes the broken page instead of
  blocking it. Found live on the `:::design` fence gate during #480 (fixed with the
  pre-build `scripts/check-design-fences.mjs`); the `::screenshot` frontmatter gate has
  the same hole on `.md` pages.
- **Fix:** gate outside the render path: a pre-build script wired into `pnpm build`
  (exemplar: `docs/scripts/check-design-fences.mjs`) or a lint in `internal/docslint`,
  and verify the exit code, not the log line.

## vacuous-async-spy (live)

A synchronous `expect(spy).not.toHaveBeenCalled()` guarding the absence of a network call or
effect that is inherently a microtask away (an async middleware, a queued promise, a Solid
effect). The assertion runs before the effect could ever fire, so it passes under any
implementation, leaking or not: a pin that cannot fail pins nothing. Found in #609's
gated-fetch test, where the api client's async onRequest middleware deferred every fetch past
the synchronous assertion. Detection cue: a sync spy assertion immediately after `render()`
or `mount()` claiming an effect did NOT happen. Fix: assert the structural fact synchronously
(the query cache holds no entry for the gated key), or flush a tick before the spy assertion
and prove the test can fail by breaking the guard.
