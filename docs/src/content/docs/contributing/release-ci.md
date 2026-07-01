---
title: "Release and CI"
description: "The CI gates on every PR and the manual semantic-release versioning cut from main."
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

No changelog is ever committed back to `main`, so the release never writes to the default
branch. The generated notes live on the GitHub Release.

To switch to release-on-merge later, change the release workflow's trigger to `push` on
`main`; the make targets stay as the local preview path.

## Binaries on the release

semantic-release cuts the tag and the Release; [GoReleaser](https://goreleaser.com/) then
fills that Release with the cross-platform binaries. The two split cleanly: semantic-release
owns the version and the notes, GoReleaser only builds artifacts and attaches them (its
`release.mode: keep-existing` leaves the notes untouched). The matrix is `linux/amd64`,
`linux/arm64`, `darwin/amd64`, `darwin/arm64`, and `windows/amd64`, plus a `checksums.txt`
and an SBOM per archive. Because the binary is pure Go with CGO disabled, all of it
cross-compiles from one runner; the SPA is built once by a `before` hook (`make web`) and
embedded in every target via `-tags web`.

The binaries are always built in CI, never on a laptop, but which workflow builds them
depends on how the release was cut:

- **`make release-apply`** pushes the tag with your token, which cascades, so the
  tag-triggered `goreleaser.yml` workflow builds the binaries.
- **The CI-dispatch path** pushes the tag with `GITHUB_TOKEN`, which by design does not
  cascade, so `release.yml` builds the binaries inline in the same job.

Both drive the same `.goreleaser.yaml`, so the artifacts are identical either way. Validate a
config change locally with `make release-snapshot` (builds the whole matrix, no tag, no
publish).

## Why the PR title, not the commits

A squash merge collapses a branch's commits into one, and GitHub uses the PR title as that
commit's subject. So the PR title is the single conventional-commit that lands on `main`, and
it is the unit both the merge model and semantic-release reason about. That is why
`pr-title.yml` is a required check, not advisory.
