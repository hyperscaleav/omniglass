# Working in omniglass

Open observability and control plane for AV/IT estates, and a learning tool for how one
is built. Single Go binary (run modes: server, node, migrate), BYO PostgreSQL. The
architecture of record is the docs site under [docs/](docs/) (published at
omniglass.hyperscaleav.com/docs); read the architecture spine before non-trivial changes.

This repo is **public from the first commit** and built **one vertical slice per PR**.
Treat every change as portfolio-quality and externally visible.

## The five doctrines

1. **API first.** The Go API (Huma structs) is the source of truth; OpenAPI 3.1 is
   generated from it, and the typed SPA client, the cobra CLI, and the YAML JSONSchema are
   generated from that (`make gen`). Routes are AIP-style with `:verb` custom methods; the
   read side is the **views** BFF. [docs/contributing/api-first.md](docs/contributing/api-first.md).
2. **Test first.** Build the test before the feature. A behavior change is incomplete
   without a test that failed before it and passes after; a bug fix starts with a
   failing regression test. Full loop: [docs/contributing/test-driven.md](docs/contributing/test-driven.md).
3. **Docs with everything.** A feature is not done until the docs that teach it ship in
   the same PR. [docs/contributing/docs-with-everything.md](docs/contributing/docs-with-everything.md).
4. **Functional and pedagogical.** Omniglass is both a tool and a learning tool. Operator
   surfaces should also teach the concept they operate on, interactively, against real or
   simulated data. [docs/contributing/learning-tool.md](docs/contributing/learning-tool.md).
5. **Primitive first.** Build the reusable primitive, then consume it. Do not inline a
   one-off where a primitive belongs.

The UI is SolidJS + daisyUI + Tailwind, a generated typed client over the `ViewResult`
renderer contract; learning surfaces render the real engine, not static diagrams.
[docs/contributing/design-system.md](docs/contributing/design-system.md). Authorization is
two layers, both in the app: a `<resource>:<action>` permission checked on **every** route,
and ABAC **scope** injected by the Storage Gateway on **every** applicable query. These are
invariants, not conventions; see the architecture spine.

## Design for testability

Small, single-purpose functions, each with a full set of tests. Prefer pure functions:
output depends only on inputs. Push I/O, clock, randomness, and network to the edges (a
Storage Gateway is the only DB path; outbound effects go through the outbox / worker) so
the pure core is most of the code and unit-testable without infrastructure. If a unit is
hard to test, logic and I/O are tangled; split them.

## Test tiers

- **Unit:** pure logic, fast, no I/O.
- **Integration:** real Postgres, no mocking the database. Use `testcontainers-go` so
  each run gets an ephemeral instance on a random port (never bind a fixed host port).
- **End-to-end:** drive each entry point as the user would (API, CLI, UI), asserting the
  user-observable outcome, not internals.

No mocking the system under test. No tests-within-tests. `make test` is the gate: green
before commit and before merge. Validate locally; do not lean on CI.

**Capability-wrapping carve-out.** When a unit wraps an environment-risky capability (raw
sockets, ICMP, privileged syscalls, an external protocol), a fake-based unit test is
necessary but not sufficient. A fake-green seam is a legitimate checkpoint commit, but the
real-implementation integration test closes the increment and is an absolute gate before
any merge. The environment risk is the point of the primitive.

## Workflow

PR-only. Branch from `origin/main`, do the work in a git worktree under
`.claude/worktrees/` (gitignored), push, open a PR. Never commit to `main`.

```bash
git fetch origin main
git worktree add .claude/worktrees/<type>+<short-name> -b <type>/<short-name> origin/main
cd .claude/worktrees/<type>+<short-name>
```

- Branch prefix and PR title use the conventional-commit type (`feat`, `fix`, `docs`,
  `ci`, `chore`, `refactor`, `test`, `perf`). PR title is `<type>: <description>`; CI
  enforces it and semantic-release reads it on merge.
- Squash-merge. `feat:` = minor, `fix:`/`perf:` = patch, `BREAKING CHANGE:` = major.
- Validate locally: `make test-short` to iterate, `make test` before the PR.
- No `--no-verify` without explicit approval.

## Tracking: issues, not TODOs

All work lives in GitHub issues under epics. Do not keep a TODO doc in the tree and do
not write a bare `// TODO`; reference an issue (`// TODO(#123): ...`). If it is worth
doing later, file the issue.

## House style

- No AI/assistant attribution in commits, PRs, code comments, or any visible artifact.
- No em dashes in written artifacts; use commas, colons, periods, or parentheses.
- Head-noun-last naming (`<qualifier>_<genus>`); match the architecture glossary.

## Migrations and seeding

Schema is managed with **dbmate**: pure-DDL migrations under `db/migrations/`, embedded into
the binary (`//go:embed`) and applied by the `migrate` run mode. Two rules matter most:
migrations **run exactly once** (dbmate keys on the timestamp version, not the contents), and
you **never edit an applied migration**, you add a new one. DDL is idempotent. The full
workflow (incl. the Postgres rename DO-block) is the `/storage-schema-change` skill.

Three buckets, never conflated:

- **Schema migrations** (`db/migrations/*.sql`, dbmate): pure DDL. No seed rows (a schema
  dump/squash silently drops them).
- **Boot seed phase** (idempotent upsert on every server start): ship-with reference data as
  embedded YAML, authoritative via `ON CONFLICT DO UPDATE`; operator rows untouched.
- **One-time data backfills** (dbmate): transforming existing operator data, run once.

## Skills

Procedural workflows live under [.claude/skills/](.claude/skills/). Invoke with
`/skill-name` (ported and refined as the corresponding subsystems land):

- **`/storage-schema-change`** (ported) - how dbmate migrations work (run-once, never edited,
  idempotent, the PG rename DO-block), the three buckets, the Gateway ripple, and the
  testcontainer round-trip RED->GREEN.
- **`/canonical-datapoint`**, **`/add-collection-primitive`** - ported with the registry and
  collection-engine slices.
