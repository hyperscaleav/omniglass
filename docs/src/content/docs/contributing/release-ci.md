---
title: "Release and CI"
description: "The CI gates on every PR and the manual semantic-release versioning cut from main."
---

`make test` is the local gate (the [test-driven](/contributing/test-driven/) doctrine says
validate locally, do not lean on CI). CI is the backstop and, on `main`, the release driver.
Thirteen workflows carry it: `test`, `pr-title`, `image`, `docs-shots`, `gen-drift`,
`docs-build`, `migrations`, `preview-comment`, `demo`, `demo-tag`, `demo-prune`, `release`, and `goreleaser`.

## On every pull request

- **`test.yml`** (the test gate) runs `go build ./...` and then `make test`, the same target
  contributors run locally, so the CI gate cannot drift from the local one (#214): the SPA
  typecheck and vitest suite (`test-web`), every Go package, then the NET_ADMIN tier in its
  container. It runs on a runner with a Docker daemon, so the testcontainers-backed integration
  and e2e tiers actually execute (after widening `net.ipv4.ping_group_range` so the collection
  tier's unprivileged ICMP probe can open its socket), and with `TEST_PARALLEL` raised to the
  runner's core count. A second job in the same workflow runs the browser e2e tier
  (`make test-e2e`): the console driven as an operator would against the real binary, fully
  containerized, covering the CRUD round-trips plus one smoke per console route from the
  route manifest, so a PR cannot merge with a route that errors in a real browser. Both jobs
  also run on `main` after merge.
- **`pr-title.yml`** lints the PR title to the conventional-commit grammar. This matters
  because the repo squash-merges: the squash subject *is* the PR title, and semantic-release
  reads it to decide the next version. A malformed title would either mis-version or silently
  skip a release, so it is blocked at the PR.
- **`gen-drift.yml`** runs `make gen` and fails the PR on any diff in the committed generated
  artifacts (the OpenAPI, the cobra tree, the CLI reference, the ERD, the typed SPA client,
  the protobuf wire), the drift gate [API first](/contributing/api-first/) promises.
- **`docs-build.yml`** (path-filtered to `docs/**`) builds the Astro docs site, so a broken
  page fails the PR that introduces it instead of the next deploy.
- **`migrations.yml`** (path-filtered to `db/**`) fails a PR that adds a migration whose
  version is not newer than the newest on the base branch's tip, fetched fresh, or is stamped
  later than the clock (`make migration-check` is the same check, locally). It also runs on
  each push to `main` against the commit before the push, the backstop for two PRs that both
  went green before either merged; there the remedy is an issue, never a rename, since a merged migration may already be applied. Uniqueness needs no base, so `make test` holds it.
- **`docs-shots.yml`** (path-filtered to `docs/**` and `web/**`) gates the generated docs
  screenshots: every declared shot has a committed PNG, and a recapture against the real
  console fails on any unmasked pixel difference (nondeterministic regions are masked in
  each page's frontmatter, so the pinned browser's captures are byte-stable and the
  tolerance is zero).

Three more PR workflows: **`image.yml`** builds the multi-arch container image (see
[Container image](/guides/container-image/)), **`preview-comment.yml`** reacts to the
`run:preview` label (see [PR previews](/guides/pr-previews/)), and **`demo.yml`**
reacts to the `run:demo` label, recording the [demos](/contributing/demos/) the PR body tags
(each on a runner of its own) to the PR's evidence ref, keeping one comment listing every demo and
writing each demo's clip into the PR body (see [Labels](/contributing/labels/)); nightly, the same
workflow re-records every demo on main and opens an issue when one fails. **`demo-tag.yml`** lints
the PR body's demo tag on every PR. **`demo-prune.yml`** deletes an evidence ref 30 days after its
PR closes.

## Pinned actions and Dependabot

Every action a workflow uses, first-party `actions/*` included, is pinned to a full commit SHA
with its version in a trailing comment (`uses: actions/checkout@<sha> # v4.4.0`), never to a tag
(#66). A tag is mutable: one force-pushed or compromised upstream would run someone else's code
with the workflow's token, and the release path's token can publish. A SHA cannot move.

`.github/dependabot.yml` keeps the pins current. Weekly, it opens a PR per ecosystem (the
workflows' actions, the Go module, the console's npm tree, and the docs site's pnpm tree) that
bumps each SHA and rewrites its version comment together, minor and patch grouped and a major
alone. Its titles are conventional commits the PR-title gate accepts (`ci:` for actions,
`chore(deps):` or `chore(deps-dev):` otherwise), so a bump never cuts a release by itself, and it
applies no labels, since the [label set](/contributing/labels/) is fixed. Its PRs carry no demo,
so the demo-tag lint skips them by author. A new workflow pins its actions the same way from its
first commit.

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
publish), and CI runs the same snapshot on any pull request that touches the release config,
uploading the archives as downloadable workflow artifacts. A green PR snapshot is real
evidence that the tagged release will build.

## Why the PR title, not the commits

A squash merge collapses a branch's commits into one, and GitHub uses the PR title as that
commit's subject. So the PR title is the single conventional-commit that lands on `main`, and
it is the unit both the merge model and semantic-release reason about. That is why
`pr-title.yml` is a required check, not advisory.
