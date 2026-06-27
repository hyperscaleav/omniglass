<!--
PR title MUST be a conventional commit: "<type>: <description>"
type = feat | fix | docs | ci | chore | refactor | test | perf
The title becomes the squash commit on main and drives the release.
-->

## What and why

<!-- One paragraph: the behavior this PR adds or changes, and the slice it advances. -->

Closes #

## How it was tested

<!-- The test that failed before and passes now. Tier(s): unit / integration / e2e. -->

## Checklist

- [ ] Tests added/updated; failed before the change, pass after (`make test` green, once the toolchain lands)
- [ ] Docs updated in `docs/` (or `no-docs` label with justification)
- [ ] Operator-facing surface also teaches its concept (learning-tool restriction), if applicable
- [ ] Conventions: gofmt, lint, no em-dashes, no AI attribution
- [ ] Linked to an issue under an epic
