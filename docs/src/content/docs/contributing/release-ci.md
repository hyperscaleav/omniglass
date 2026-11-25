---
title: "Release and CI"
description: "The CI gates on every PR and the release cut from a release branch, by hand or by the one-dispatch 0.x cut."
---

`make test` is the local gate (the [test-driven](/contributing/test-driven/) doctrine says
validate locally, do not lean on CI). CI is the backstop, and the release workflow cuts each
release from a release branch.
Fourteen workflows carry it: `test`, `pr-title`, `image`, `docs-shots`, `gen-drift`,
`docs-build`, `migrations`, `preview-comment`, `demo`, `demo-tag`, `regression-test`, `demo-prune`, `release`, and `goreleaser`.
The three agent workflows (`agent-build`, `agent-review`, `agent-fix`) run Claude Code agents
on GitHub-hosted runners and have a page of their own, [Agents in CI](/contributing/agents-in-ci/).

## On every pull request

- **`test.yml`** (the test gate) runs the parts of `make test`, the target contributors run
  locally, as parallel jobs, so the CI gate cannot drift from the local one (#214) and its wall
  time is the slowest part rather than the sum (#1536): the SPA typecheck and vitest suite
  (`test-web`), the Go tier as three shards (`test-go-storage` and `test-go-devseed`, the two
  slowest packages, and `test-go-rest`, every other package), and the NET_ADMIN tier in its
  container (`test-netadmin`). The rest shard's package list is derived from `go list ./...`
  minus the named shards, and `make test-go-shards-check` fails if a package is in no shard or in
  two. The jobs run on runners with a Docker daemon, so the testcontainers-backed integration and
  e2e tiers actually execute; only `test-go-rest` widens `net.ipv4.ping_group_range` (the
  collection tier's unprivileged ICMP probe needs it), and `TEST_PARALLEL` is raised to the
  runner's core count. Each Go shard runs under `cmd/gotestreport`: the log reads as a plain
  `go test` run, and ends with the shard's 20 slowest tests and every package's wall time.
  The required check is the final job, named `test`, which `needs` every shard, runs
  `if: always()`, and fails unless each one succeeded; add a new shard to its `needs`. A second
  job in the same workflow runs the browser e2e tier
  (`make test-e2e`): the console driven as an operator would against the real binary, on a
  throwaway lab, covering the CRUD round-trips plus one smoke per console route from the
  route manifest, so a PR cannot merge with a route that errors in a real browser. All of these
  jobs also run on `main` after merge and on every push to a release branch.
- **`pr-title.yml`** lints the PR title to the conventional-commit grammar. This matters
  because the repo squash-merges: the squash subject *is* the PR title, and a release's
  notes group it by its type. A malformed title would muddy the history and land in the
  notes' Other changes, so it is blocked at the PR. The version itself comes from the release branch, not the titles.
- **`gen-drift.yml`** runs `make gen` and fails the PR on any diff in the committed generated
  artifacts (the OpenAPI, the cobra tree, the CLI reference, the ERD, the typed SPA client,
  the protobuf wire), the drift gate [API first](/contributing/api-first/) promises.
- **`docs-build.yml`** (path-filtered to `docs/**` and `api/openapi.yaml`) builds the Astro docs site, so a broken
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

Three more PR workflows: **`image.yml`** publishes the multi-arch container image on `main` and
release pushes (see [Container image](/deployment/container-image/) and [Fast iteration](#fast-iteration-on-a-pr) below), **`preview-comment.yml`** reacts to the
`run:preview` label (see [PR previews](/developer/pr-previews/)), and **`demo.yml`**
reacts to the `run:demo` label, recording the [demos](/contributing/demos/) the PR body tags
(each on a runner of its own) to the PR's evidence ref, keeping one comment listing every demo and
writing each demo's clip into the PR body (see [Labels](/contributing/labels/)); nightly, the same
workflow re-records every demo on main and opens an issue when one fails. **`demo-tag.yml`** lints
the PR body's demo tag on every PR. **`regression-test.yml`** fails a PR that closes a bug or flaky-test
issue (label or Bug type) without changing a test or carrying a reasoned `regression-test: none (<reason>)` line.
**`demo-prune.yml`** deletes an evidence ref 30 days after its
PR closes.

## Fast iteration on a PR

A push to a PR should cost what it changes (#2597). `test.yml` starts with a `changes` job that runs
`scripts/ci-changes.sh` on the PR's changed files (a composite action, `.github/actions/changes`, shares it
with `gen-drift.yml`); the script is the whole policy and is tested in `scripts/ci_changes_test.go`.

| The push changes | Runs |
|---|---|
| docs only (`docs/**`, `*.md`, evidence screenshots, issue templates, `.claude/**`) | the docs checks (`docs-build.yml`, `docs-shots.yml`, their own path filters) |
| web (`web/**`) | web unit tests, `gen-drift`, the shared build, `e2e`, `smoke-lab` |
| Go (`**/*.go`, `go.mod`, `db/**`, `internal/**`, `cmd/**`, `api/**`, `proto/**`) | the Go shards, `gen-drift`, the shared build, `e2e`, `smoke-lab`; `api/openapi.yaml` also runs the docs checks (`docs-build.yml` reads it) |
| generated docs (`docs/src/generated/**`, the reference pages, the data model page) | `gen-drift` |
| workflows, the Dockerfile, the Makefile, `scripts/**`, or any path no area claims | everything |

A draft runs the unit jobs its files call for and no image, `e2e` or `smoke-lab`; marking it ready
for review (`ready_for_review`) runs everything. `main` pushes, release pushes and merge queue entries
run everything. The image is built once, by the `build` job, with the BuildKit GitHub Actions cache, and
handed to `e2e` and `smoke-lab` as one artifact (the image under both its tags, and the host binary); they
load it and run with `SKIP_BUILD=1` (see the Makefile's `all`), so nothing on a PR push builds the image twice. `docs-shots.yml`
and the opt-in `run:demo` pipeline still build their own, since their build id differs.

Skipped work still reports, so a required check never waits forever: `e2e`, `smoke-lab` and `gen-drift`
always run and pass in seconds when nothing needs them (and fail if the build or the change lookup
failed), and `test` counts a skipped shard as a pass only when the change lookup succeeded. Publishing
is off the PR path: the arm64 smoke runs on pushes and queue entries, the multi-arch push on `main`
and release pushes only, and a PR gets one amd64 image (`pr-<n>` and its head sha) only with the
`run:preview` label.

The agents follow the same rule. `agent-review` waits for the `test`, `e2e`, `smoke-lab` and `gen-drift`
checks to exist and finish on the same head commit, and holds (no review, the check red with the reason)
if one is red or they have not finished in 40 minutes. `agent-fix` merges `main` only when the PR
conflicts with it, or is behind it with a required check red (`scripts/agent-sync.sh needed`), because
each merge is a new head commit that restarts every check.

## Merging through the queue

A PR reaches `main` through GitHub's merge queue (#2181), so nobody rebases a green PR or re-runs
its CI by hand. Once its required checks are green on the PR and it is approved, the architect
queues it with one click. The queue builds a temporary branch of the PR on top of the current
`main` (and of any PRs queued ahead of it), runs the required checks there, and squash-merges it
when they pass; when they fail, the PR leaves the queue and `main` is untouched. That is the
up-to-date rule met on the commit that actually lands, without a rebase loop or the admin bypass.

The queue waits for each required check (`test`, `e2e`, `gen-drift`, `smoke-lab` and `lint`) to
report on the queue entry, so every workflow that reports one also runs on `merge_group`. A
queue entry has its own ref, so the workflows' `github.ref` concurrency groups give each entry a
run of its own. Three lints have nothing to read there: `pr-title.yml`, `demo-tag.yml` and `regression-test.yml` checked
the PR's title and body before it could be queued, so on `merge_group` their `lint` job passes
without reading a PR. `image.yml` runs only the arm64 smoke in the queue and publishes no image, since
a queue entry's commit may never land; the publish runs on `main` and release pushes. A queue entry runs
every job in full: the path selection below applies to a PR push only.

A guard test, `internal/build/merge_group_guard_test.go`, reads `.github/workflows/` and fails when
a workflow that reports a required check lacks `merge_group`, or when no workflow reports one. The
required names are a list in the test, kept in step with the rulesets on `main` by hand: a check
made required there is added to the list in the same change, or the queue would wait on it forever.

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

Releases are **not** cut on merge to `main`, and `main` never freezes for one. A release is cut
from a **release branch**, `release/<major>.<minor>`, made from `main` on the day of the cut,
and there is exactly one way to cut one: dispatch the **release** workflow (`release.yml`) on
that branch from its "Run workflow" button (#2139). A dispatch on any other ref fails at its
first step.

- **A candidate.** A dispatch tags the branch tip as the next candidate, `v0.1.0-rc.1`, and
  publishes it as a GitHub prerelease with its image and binaries (below). From `1.0` on, that
  candidate is what [the soak gate](#the-soak-gate) runs; a `0.x` candidate needs only the CI
  its branch push already ran (ADR-2652).
- **A fix.** A fix lands on `main` first, by its own PR, and reaches the branch by a cherry-pick
  PR into `release/<major>.<minor>`. The next dispatch tags `rc.2`, and from `1.0` on its soak
  starts over.
- **The release.** Once a candidate is ready (its branch push's CI green; from `1.0` on, its
  soak passed too), a dispatch with **final** ticked tags that same commit `v0.1.0`, a full
  release. It is refused unless the commit is the newest candidate, so what ships is exactly what
  was tested.
- **A patch.** The branch lives on: after `v0.1.0`, the next candidate is `v0.1.1-rc.1`.

The version comes from the branch name and the tags already cut
(`cmd/releaseversion`), never from commit subjects, so the first `0.x` release cannot number
itself `1.0.0` from an old breaking change.

**The notes** cover every commit since the previous final release, so a candidate's notes are
cumulative, and group each PR title by its type (`cmd/releasenotes`, #2142): Breaking changes
(a `!` after the type, or a `BREAKING CHANGE:` footer), Features (`feat`), Fixes (`fix`),
Performance (`perf`), then Other changes, with a compare link to the previous final release. A
`dry_run` dispatch shows them in the run's summary. To preview them locally:

```bash
go run ./cmd/releasenotes -repo hyperscaleav/omniglass -tag v0.1.0-rc.1 -previous v0.0.0
```

**A final release comments** "Released in" with a link to the Release on each PR it ships and
each issue that PR closed, once per release. Candidates comment nothing. A release that ships
more than 100 PRs, as the first does, skips the comments and says so in the run's summary.

Every push to a release branch runs the same CI as `main` (`test`, `image`, `gen-drift`,
`migrations`, `docs-build`, `docs-shots`), and `image` publishes the commit's sha image, so a
candidate has had the full CI before it is tagged. A pull request into the branch runs the
per-PR gates as one into `main` does.

**Releasing is switched off** until the repository variable `RELEASES_ENABLED` is `true`
(Settings, Secrets and variables, Actions, Variables). While it is unset, a dispatch can only
plan: the `dry_run` toggle works, and a dispatch without it fails at its first step, before any
tag. Every publishing step checks the flag too, so nothing reaches the tag, the Release, or
GHCR until it is flipped (#1940).

**The tag is pushed by the release App.** The `v* tags: created by admins only` ruleset refuses
the workflow's own `GITHUB_TOKEN`, so the workflow mints a token from a dedicated release GitHub
App, the one bypass on that ruleset, when the variable `RELEASE_APP_CLIENT_ID` and the secret
`RELEASE_APP_PRIVATE_KEY` are set. Without them it pushes with `GITHUB_TOKEN`, and the push is
refused (#1940). A `dry_run` dispatch mints the App's token too and reports it in the run's
summary, so a wrong client id, key or permission fails a dry run, not the first candidate
(#2147). Only a real tag push exercises the App's bypass on the ruleset. To try a dry run before
the real cut, an admin can create a throwaway release branch such as `release/0.0`, dispatch on
it with `dry_run`, and delete it. Release branches themselves are protected like `main` by a `release/*`
ruleset: pull requests only, the same required checks, no force-push or deletion.

To preview locally, from the release branch:

```bash
make release-plan            # the tag the next dispatch cuts
make release-plan FINAL=1    # the final release of the newest candidate
```

No changelog is ever committed back, so a release never writes to `main` or the branch. The
notes live on the GitHub Release.

## Cutting a 0.x release

`v0.1.0` is the first public build, cut by hand as above once the release App is set up and
`RELEASES_ENABLED` is on (#1940). After it, a `0.x` release is cut whenever the work calls for
one, roughly weekly, and cutting one takes a single action (ADR-2652):

- **One dispatch, no inputs.** Dispatching the cut workflow makes the next minor's branch from
  `main`'s tip: after `v0.4.0`, `release/0.5`. It dispatches a candidate (`v0.5.0-rc.1`) and,
  once that branch push's CI is green, the final on the same commit (`v0.5.0`). Nobody picks the
  version, the branch or the commit.
- **It refuses** when `main` has no commit since the last cut, or `RELEASES_ENABLED` is off.
- **It never cuts `1.0`.** `1.0` is a hand cut, and it needs [the soak](#the-soak-gate).
- **A nudge, not a schedule.** When the newest final is a week old and `main` has commits since
  it, a scheduled check opens one "a cut is due" issue, and the next cut closes it. Nothing
  scheduled cuts a release.
- **The version is still the branch's.** The cut only chooses the branch name, and
  `cmd/releaseversion` works out the tag as it does for a hand dispatch. semantic-release stays
  out of the path: it would number the first release `1.0.0` from breaking changes made before
  any release, and cut a release for every merge.
- **A fix that cannot wait** for the next cut goes by cherry-pick to the newest release branch
  and a hand dispatch, which makes it that line's patch (`v0.5.1-rc.1`, then `v0.5.1`).

:::note[Not built yet]
Neither the cut workflow nor the nudge exists yet, so until they land each `0.x` release is cut
by hand, the same way. The follow-up work is listed on #2652.
:::

Until `1.0`, Omniglass is pre-production software: breaking changes can land in any release,
without notice, and it is not for production use. The README and the docs home page say so, and
every release's notes will too; the notice comes off in the PR that cuts `1.0`.

## The soak gate

From `1.0` on, no final release ships unless the build has run a week under the 3,000-system
corporate soak without failing silently (#1988). A `0.x` final needs no soak record (ADR-2652):
the soak can still be run on a `0.x` candidate by choice, and its record is committed the same
way. The soak is run by hand on a test host, never a CI job: a
week-long run on a dedicated machine does not belong on a runner, and its scheduling is not
automated. The steps, between a candidate and its final release:

1. Cut the candidate ([above](#cutting-a-release-manual)), which publishes its image and binary
   from one commit, and start the soak on the test host with that `v<version>-rc.<n>` build, as
   the [soak run](/developer/lab/#a-soak-run) describes. Note the time it starts.
2. Leave it for 7 days.
3. Judge it, from any machine that reaches the soak's server, with a token that reads outposts,
   systems and components over everything:

   ```bash
   omniglass lab soak verdict --since <start> --record-dir docs/src/content/soak-records
   ```

4. Commit the record the command wrote, by a PR to `main`. **From `1.0` on, no passing record
   for the candidate's build, no final release.** A failing soak verdict exits 1, names every reason, and is also
   recorded; fix the build and run a new week.

The soak verdict is the platform's own, not a harness's figures derived again: it reads through the API
the platform system's alarm history and the Platform health page's loss ratios, and nothing from
the database. It passes when all of these hold:

| Rule | Threshold |
|------|-----------|
| The soak ran | at least 7 days (`--since` to `--until`, now by default) |
| A platform alarm still open | none, however old |
| A platform alarm raised in the window | open no longer than `--tolerance` (default 30 minutes) |
| A loss ratio on the page (each hop, kind, component and outpost, the newest window and the last hour, the end to end, the worklist pull) | 0 |

A reading that cannot answer fails rather than passes: no build reported, two builds among the
servers, no loss ratio on the page, a token that cannot read the whole page, or an alarm history
that reached the API's 500-alarm cap. The thresholds are named in code (`SoakMinWindow`,
`SoakAlarmTolerance` and `SoakLossCeiling` in `internal/lab/soak.go`), and the alarm history reaches back
30 days, so a soak verdict is read within 30 days of the soak's start. It gets stricter as the
platform raises more alarms about itself: it judges by whatever the platform alarms on today.

**The record** is one file per build, `docs/src/content/soak-records/<build>.md`, so the release's
notes can link the soak it passed. Its front matter carries the facts and its body the soak verdict in
words (a failure lists its reasons):

```md
---
title: "Soak record: v1.4.0"
build: v1.4.0
verdict: pass
since: 2026-11-20T08:00:00Z
until: 2026-11-27T09:12:00Z
host: soak-1 linux/amd64 16 cpu machine-id:3f2a9c1d7b40
---
```

The host is the machine the soak verdict ran on (name, platform, cores and a short digest of its
machine id, never the id), which on a soak host is the machine the soak ran on.

## Binaries on the release

The release workflow cuts the tag and the Release; the same job then builds and pushes the
multi-platform release image to GHCR, and [GoReleaser](https://goreleaser.com/) fills the
Release with the cross-platform binaries, each stamped with that image's digest so a release
binary's `omniglass lab run` pulls exactly the image it was released with
([ADR-1257](/architecture/decisions/#adr-1257-a-release-binary-pulls-its-lab-image-by-the-digest-stamped-at-release)).
The workflow owns the version and the notes, GoReleaser only builds artifacts and attaches
them (its `release.mode: keep-existing` leaves the notes untouched, and `prerelease: auto` keeps
a candidate a prerelease). The matrix is `linux/amd64`,
`linux/arm64`, `darwin/amd64`, `darwin/arm64`, and `windows/amd64`, plus a `checksums.txt`
and an SBOM per archive. Because the binary is pure Go with CGO disabled, all of it
cross-compiles from one runner; the SPA is built once by a `before` hook (`make web`) and
embedded in every target via `-tags web`.

No workflow runs on a tag push, so the image and the binaries are built inline in
`release.yml` rather than by a tag-triggered workflow; a `v*` tag pushed any other way builds
nothing. Validate a config change locally
with `make release-snapshot` (builds the whole matrix, no tag, no publish), and
`goreleaser.yml` runs the same snapshot on any pull request that touches the release config,
uploading the archives as downloadable workflow artifacts. A green PR snapshot is real
evidence that the release will build.

## Why the PR title, not the commits

A squash merge collapses a branch's commits into one, and GitHub uses the PR title as that
commit's subject. So the PR title is the single conventional-commit that lands on `main`, and
it is the unit both the merge model and the release notes reason about. That is why
`pr-title.yml` is a required check, not advisory.
