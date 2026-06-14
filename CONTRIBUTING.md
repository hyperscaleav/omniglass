# Contributing to Omniglass

Omniglass is built in the open, one vertical slice at a time. This document is the
contract for how a change gets in. The deeper doctrine lives in the docs site
([API first](docs/src/content/docs/contributing/api-first.md),
[Test-driven](docs/src/content/docs/contributing/test-driven.md),
[Docs with everything](docs/src/content/docs/contributing/docs-with-everything.md),
[The learning-tool restriction](docs/src/content/docs/contributing/learning-tool.md),
[The design system](docs/src/content/docs/contributing/design-system.md)); this is the
operational checklist.

## The cardinal rules

1. **Everything is a pull request.** No direct commits to `main`. `main` is protected:
   PRs require a passing CI run and one review.
2. **Everything is an issue.** Work is tracked as GitHub issues grouped under epics.
   We do not keep TODO docs in the tree and we do not write `// TODO` without an issue
   reference (`// TODO(#123): ...`). If it is worth doing later, it is worth an issue.
3. **Test first.** A change that adds or alters behavior carries a test that failed
   before the change and passes after. A bug fix starts with a failing regression test.
4. **Docs with everything.** A user-facing change ships the docs that teach it, in the
   same PR. See the docs-touched gate below.
5. **One shippable slice per PR.** Prefer a thin vertical cut that a user can observe
   over horizontal scaffolding.

## Workflow

```bash
git fetch origin main
git switch -c <type>/<short-name> origin/main
# ... work, test-first ...
git push -u origin <type>/<short-name>
# open a PR; title is "<type>: <description>"
```

- **Branch prefix and PR title** use the conventional-commit type: `feat`, `fix`,
  `docs`, `ci`, `chore`, `refactor`, `test`, `perf`. CI enforces the PR-title format.
- **Squash merge only.** The PR title becomes the commit on `main` and drives
  semantic-release: `feat:` = minor, `fix:`/`perf:` = patch, `BREAKING CHANGE:` in the
  body = major; `chore`/`docs`/`ci`/`test`/`refactor` produce no release.
- **Validate locally before pushing.** `make test-short` for fast iteration,
  `make test` before opening the PR. Do not lean on CI to find what a local run would.

## The per-PR gate (what CI checks)

| Gate | What it means |
|---|---|
| **Tests** | Unit tests + lint + format run on every PR. Full `make test` is label-gated (`run:test`). |
| **Docs touched** | A feature PR changes `docs/`, or carries the `no-docs` label with a one-line justification. |
| **Conventions** | gofmt, lint, naming, no em-dashes in written artifacts, API-drift + route coverage (once the API exists). |
| **PR title** | Conventional-commit format. |
| **Review** | At least one approving review. Heavy jobs (image build, CVE scan, preview env) are label-gated. |

A label-gated trigger can also invoke **Claude Code** on a PR for review or assist
(apply the `claude` label).

## House style

- **No AI/assistant attribution** in commits, PR titles/bodies, code comments, or any
  visible artifact.
- **No em dashes** in written artifacts. Use commas, colons, periods, or parentheses.
- **Head-noun-last naming** (`<qualifier>_<genus>`), matching the architecture glossary.

## Licensing of contributions

Omniglass is licensed under the GNU AGPL v3.0. By contributing, you agree your
contribution is licensed under the same terms. Do not paste code you do not have the
right to submit under the AGPL.
