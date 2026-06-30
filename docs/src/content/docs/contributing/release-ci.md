---
title: "Release and CI"
description: "The CI gates on every PR and the semantic-release versioning that runs on merge to main."
---

`make test` is the local gate (the [test-driven](/contributing/test-driven/) doctrine says
validate locally, do not lean on CI). CI is the backstop and, on `main`, the release driver.
Three workflows carry it.

## On every pull request

- **`test.yml`** (the test gate) runs `go build ./...` and `go test ./...` on a runner with a
  Docker daemon, so the testcontainers-backed integration and e2e tiers actually execute. It
  also runs on `main` after merge.
- **`pr-title.yml`** lints the PR title to the conventional-commit grammar. This matters
  because the repo squash-merges: the squash subject *is* the PR title, and semantic-release
  reads it to decide the next version. A malformed title would either mis-version or silently
  skip a release, so it is blocked at the PR.

The other PR check, **`image.yml`**, builds the multi-arch container image (see
[Container image](/guides/container-image/)).

## Cutting a release (manual)

Releases are **not** cut automatically on merge to `main` (deliberately, for now). A release
is a deliberate act, run from an up-to-date `main` with [semantic-release](https://semantic-release.gitbook.io/),
which reads the conventional-commit subjects since the last tag, computes the next version,
pushes a git tag, and creates a GitHub Release with generated notes.

Two make targets:

```bash
make release-plan    # dry run: print the next version + notes, publish nothing
make release-apply   # tag + create the GitHub Release
```

The same thing can be dispatched in CI from the **release** workflow's "Run workflow" button
(with a `dry_run` toggle), for a release cut from a clean checkout instead of a laptop.

| Title prefix | Release |
|--------------|---------|
| `feat:` | minor |
| `fix:`, `perf:` | patch |
| `BREAKING CHANGE:` (footer) or `feat!:` | major |
| `docs:`, `ci:`, `chore:`, `refactor:`, `test:` | none |

The tag is the only artifact: no changelog is committed back to `main`, so the release never
writes to the default branch. The generated notes live on the GitHub Release. The
[binary release pipeline](https://github.com/hyperscaleav/omniglass/issues/55) builds its
cross-platform artifacts off the tag.

To switch to release-on-merge later, change the release workflow's trigger to `push` on
`main`; the make targets stay as the local preview path.

## Why the PR title, not the commits

A squash merge collapses a branch's commits into one, and GitHub uses the PR title as that
commit's subject. So the PR title is the single conventional-commit that lands on `main`, and
it is the unit both the merge model and semantic-release reason about. That is why
`pr-title.yml` is a required check, not advisory.
