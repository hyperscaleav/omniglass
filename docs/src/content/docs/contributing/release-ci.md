---
title: "Release and CI"
description: "The CI gates on every PR and the release cut from main by merging release-please's release PR."
---

`make test` is the local gate (the [test-driven](/contributing/test-driven/) doctrine says
validate locally, do not lean on CI). CI is the backstop, and release-please cuts each
release from `main`.
Fifteen workflows carry it: `test`, `pr-title`, `image`, `docs-shots`, `gen-drift`,
`docs-build`, `migrations`, `preview-comment`, `demo`, `demo-tag`, `regression-test`, `demo-prune`, `release-please`, `release`, and `goreleaser`.
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
  jobs also run on `main` after merge (and on a push to a `release/**` branch, a trigger left
  from the release branches ADR-2652 retired).
- **`pr-title.yml`** lints the PR title to the conventional-commit grammar. This matters
  because the repo squash-merges: the squash subject *is* the PR title, and a release's
  notes group it by its type. A malformed title would muddy the history and land in the
  notes' Other changes, so it is blocked at the PR. The titles also pick the version: release-please reads their types
  to choose the next release's number (see [cutting a release](#cutting-a-release-manual)).
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

The path-filtered checks that are not required (`docs-build`, `docs-shots` verify, `migrations` and the
`goreleaser` snapshot) run none of their work on a draft (#2309): each job is guarded on the draft flag, so it
reports skipped rather than never appearing, and each workflow listens for `ready_for_review`, which runs it on
the draft's current head once the PR is marked ready. A draft that wants them now takes the `run:test`
label ([labels](/contributing/labels/#runtask-triggers)): adding it starts them, and a push while it stays
on runs them as on a ready PR. Any other label starts none of them. `pr-title` and `demo-tag` are metadata checks
and run on a draft. The merge queue and pushes to `main` and release branches run all of it. CodeQL is the
repository's default setup, not a workflow here, so a draft guard cannot be written for it in this tree.

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

Releases are **not** cut on merge to `main`, and `main` never freezes for one. Every release, a
minor or a patch, is cut from `main` by [release-please](https://github.com/googleapis/release-please)
(ADR-2652), and there is exactly one way to cut one: merge the release PR.

- **The release PR.** On every push to `main`, `release-please.yml` keeps one PR open,
  `chore(main): release 0.<n>.<p>`, holding the next version and that release's notes, and
  updates it as more PRs merge. It changes only `version.txt` and
  `.release-please-manifest.json`; no CHANGELOG is ever committed.
- **The release.** Merging the release PR, through the queue like any other, is the release:
  on that push, release-please tags the merge commit `v0.<n>.<p>` and creates the GitHub Release
  with the PR's notes, and `release.yml` then builds the image and attaches the binaries
  ([below](#binaries-on-the-release)). Cut one whenever the work calls for one, roughly weekly;
  the open PR is the reminder.
- **No release branches, no backports.** Releases go forward only. A fix lands on `main` by its
  own PR and ships in the next release; an older line takes no patches.
- **No candidates before 1.0.** A `0.x` release is the merge commit `main` already ran its full
  CI and the merge queue on. How a `1.0` candidate is cut for [the soak](#the-soak-gate) is
  decided before `1.0`.

**The version is not a judgment call.** release-please reads the PR titles squash-merged on
`main` since the last release (`release-please-config.json`):

- **A breaking change** (a `!` after the type, or a `BREAKING CHANGE:` footer) bumps the
  **minor** while the version is below `1.0.0` (`bump-minor-pre-major`).
- **A `feat`** bumps the minor, and **a `fix` or `perf`** the patch.
- **Anything else alone** (`docs`, `ci`, `chore`, `refactor`, `test`) releases nothing, and opens
  no release PR.
- **`1.0` is set on purpose**, by a `release-as` in the config in the PR that cuts it, never by a
  commit.

**The notes** group each PR title by its type: Breaking changes, Features (`feat`), Fixes
(`fix`), Performance (`perf`), and Reverts; the other types are left out. They are the release
PR's body until it merges, and the Release's body after. To preview the same grouping locally for any
range (`cmd/releasenotes` renders its own format, close to release-please's but not identical):

```bash
go run ./cmd/releasenotes -repo hyperscaleav/omniglass -tag v0.1.0 -previous v0.0.0
```

**A release comments** "Released in" with a link to the Release on each PR it ships and each
issue that PR closed, once per release. A release that ships more than 100 PRs, as the first
does, skips the comments and says so in the run's summary.

**Releasing is switched off** until the repository variable `RELEASES_ENABLED` is `true`
(Settings, Secrets and variables, Actions, Variables). While it is unset, `release-please.yml`
does nothing on a push to `main`, and `release.yml` refuses to run, so nothing reaches a tag, a
Release, or GHCR until it is flipped (#1940).

**It runs with the release App's token.** `release-please.yml` mints a token from the release
GitHub App (the variable `RELEASE_APP_CLIENT_ID` and the secret `RELEASE_APP_PRIVATE_KEY`), with
contents, pull-request and issue write, and release-please opens the PR and creates the tag and the
Release with it. `GITHUB_TOKEN` would not do: the `v* tags: created by admins only` ruleset lets
only admins and the App create tags, and a PR opened with `GITHUB_TOKEN` starts no workflow, so the
release PR's required checks would never run. release-please marks its PR with two labels of its
own, `autorelease: pending` and `autorelease: tagged`, which it reads to find the PR to tag.

**A failed build is rerun by hand.** The tag and the Release exist before `release.yml` starts, so
a release whose image or binaries failed is built again by dispatching **release** with its tag
(`v0.1.0`); it refuses a tag with no Release.

Until `1.0`, Omniglass is pre-production software: breaking changes can land in any release,
without notice, and it is not for production use. The README and the docs home page say so, and
every release's notes will too; the notice comes off in the PR that cuts `1.0`.

## The soak gate

From `1.0` on, no final release ships unless the build has run a week under the 3,000-system
corporate soak without failing silently (#1988). A `0.x` release needs no soak record (ADR-2652):
a `0.x` build can still be soaked by choice, and its record is committed the same way. The soak
is run by hand on a test host, never a CI job: a week-long run on a dedicated machine does not
belong on a runner, and its scheduling is not automated. The steps, between a candidate and its
final release:

1. Start the soak on the test host with the build under test, as the
   [soak run](/developer/lab/#a-soak-run) describes, and note the time it starts. Before `1.0`,
   that is any released `0.x` build; how a `1.0` candidate is cut from `main` is decided before
   `1.0` (ADR-2652).
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

release-please cuts the tag and the Release; `release.yml` then builds and pushes the
multi-platform release image to GHCR, and [GoReleaser](https://goreleaser.com/) fills the
Release with the cross-platform binaries, each stamped with that image's digest so a release
binary's `omniglass lab run` pulls exactly the image it was released with
([ADR-1257](/architecture/decisions/#adr-1257-a-release-binary-pulls-its-lab-image-by-the-digest-stamped-at-release)).
release-please owns the version and the notes, GoReleaser only builds artifacts and attaches
them (its `release.mode: keep-existing` leaves the notes untouched). The matrix is `linux/amd64`,
`linux/arm64`, `darwin/amd64`, `darwin/arm64`, and `windows/amd64`, plus a `checksums.txt`
and an SBOM per archive. Because the binary is pure Go with CGO disabled, all of it
cross-compiles from one runner; the SPA is built once by a `before` hook (`make web`) and
embedded in every target via `-tags web`.

No workflow runs on a tag push: `release-please.yml` calls `release.yml` on the tag it just
created, so a `v*` tag pushed any other way builds nothing. Validate a config change locally
with `make release-snapshot` (builds the whole matrix, no tag, no publish), and
`goreleaser.yml` runs the same snapshot on any pull request that touches the release config,
uploading the archives as downloadable workflow artifacts. A green PR snapshot is real
evidence that the release will build.

## Why the PR title, not the commits

A squash merge collapses a branch's commits into one, and GitHub uses the PR title as that
commit's subject. So the PR title is the single conventional-commit that lands on `main`, and
it is the unit both the merge model and the release notes reason about. That is why
`pr-title.yml` is a required check, not advisory.
