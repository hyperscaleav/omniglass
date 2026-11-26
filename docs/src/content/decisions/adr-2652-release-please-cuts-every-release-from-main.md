---
title: "release-please cuts every release from main, and the soak gates 1.0"
date: 2026-11-26
status: "Accepted"
summary: "Every release, minor or patch, is cut from main by release-please. It keeps one release PR open on main with the next version and the notes, read from the squash-merged PR titles since the last release; merging that PR is the release, and it tags the merge commit and creates the GitHub Release, whose run then builds the image and the binaries. A feat or a breaking change bumps the minor before 1.0, a fix or perf the patch, and 1.0 is only ever set on purpose. There are no release branches and no backports: releases go forward only, and a fix ships in the next one. No CHANGELOG is committed. It runs with the release App's token. Candidates are deferred to 1.0, whose final still needs the 7-day soak. Until 1.0 the README, the docs home page and the release notes say Omniglass is pre-production, and each release line keeps its own docs behind a version picker. Supersedes ADR-2711 and most of ADR-2139, and retires make release and cmd/releaseversion"
refs: [2652, 2139, 2711, 1988, 1940, 2142]
---

- **Date:** 2026-11-26 | **Status:** Accepted | **Pages:** [release and CI](/contributing/release-ci/#cutting-a-release-manual), [the soak gate](/contributing/release-ci/#the-soak-gate), [a soak run](/developer/lab/#a-soak-run), [home](/)
- **Decision:** (#2652, decided by the architect: one release process for a minor and a patch, with nothing to work out at the cut. Supersedes [ADR-2711](#adr-2711-the-commits-pick-the-release-branch-the-branch-numbers-the-release), and [ADR-2139](#adr-2139-releases-are-cut-from-a-release-branch) except its release App and its soak, which decision 7 scopes to 1.0.)
  1. **release-please cuts every release, from `main`.** On every push to `main`, the
     release-please workflow (`googleapis/release-please-action`) keeps one release PR open,
     `chore(main): release 0.<n>.<p>`, holding the next version and that release's notes. It
     updates the PR as more PRs merge. Merging the release PR is the release: release-please tags
     the merge commit `v0.<n>.<p>` and creates the GitHub Release with the PR's notes, and the
     same run then builds the release image and attaches the binaries, as `release.yml` does today,
     and comments "Released in" on the PRs it ships. A minor and a patch are the same step.
  2. **The commits pick the version.** The squash-merged PR titles since the last release are
     the conventional commits release-please reads, so the PR-title gate already holds the input.
     A breaking change (a `!` after the type, or a `BREAKING CHANGE:` footer) or a `feat` bumps the
     minor, and a `fix` or `perf` the patch. `bump-minor-pre-major` keeps a breaking change on the
     minor while the version is below `1.0.0`, and `bump-patch-for-minor-pre-major` stays off, so
     a `feat` is still a minor. `1.0` is never reached by a commit: it is a `release-as` set in
     the config on purpose, the release that first needs the soak.
  3. **Forward only: no release branches, no backports.** Every release is a tag on `main`'s
     history, and an older line takes no patches: a fix ships in the next release, and an
     operator upgrades to get it. A pre-production project with no production users has nothing
     to backport to, and a backport path is a second process to keep working. If a fix ever has
     to reach an older line (once `1.0` has users), release-please can run a release PR on a
     `release/<major>.<minor>` branch made from that line's tag (its `target-branch`), and that
     is the time to decide it, by a new ADR.
  4. **The release PR is the approval.** Nothing scheduled merges it and nothing cuts a release
     without it. A release is cut whenever the work calls for one, roughly weekly, by merging the
     release PR, whose title and body show the version and the notes before anything ships. The
     PR stands open the whole time, so it is its own reminder, and no separate nudge is needed.
     The release-please workflow does nothing while `RELEASES_ENABLED` is unset (#1940).
  5. **No CHANGELOG is committed.** The release type is `simple` with `skip-changelog`, so the
     release PR changes only `version.txt` and `.release-please-manifest.json`, and the notes
     live on the GitHub Release (release-please writes the Release body from the release PR, not
     from a changelog file). Those two files are the only thing a release writes to `main`.
  6. **It runs with the release App's token.** Three things need a token other than
     `GITHUB_TOKEN`: the `v*` tag ruleset lets only admins and the release App create tags; a PR
     opened with `GITHUB_TOKEN` starts no workflow, so the release PR's required checks would never
     run and it could not merge; and a release created with it triggers no other workflow. So the
     workflow mints the release App's token (#1940), with contents and pull-request write, and
     release-please opens the PR and creates the tag and the Release with it.
  7. **Candidates wait for 1.0, and the soak gates 1.0.** Before `1.0` there is no candidate: a
     `0.x` release is the merge commit `main` already ran its full CI and the merge queue on. From
     `v1.0.0` on, a final release still needs a passing soak record for its build (#1988), which
     needs a candidate; how a candidate is cut from `main` (release-please's `prerelease`
     versioning, or a branch for the soak) is decided before `1.0`. The soak tooling and its
     records stay, and a `0.x` build can be soaked by choice.
  8. **Pre-production until 1.0.** The README, the docs home page and every release's notes say
     that Omniglass is pre-production software: breaking changes can land in any release, at any
     time, without notice, and it is not for production use yet. The notice comes off in the
     release PR that cuts `1.0`, and not before.
  9. **Each release line keeps its docs.** From `v0.1.0` on, the docs for each release line are
     published beside the newest, and a version picker reaches them:
     - **Where.** `main`'s docs stay at the site root (`docs.omniglass.hyperscaleav.com`), so every
       link already in the console, the code and the issues keeps working; a release line's docs
       live under `/v<major>.<minor>/` on the same host. One version a minor: a patch updates its
       line's docs.
     - **How each release publishes them.** The release run builds the site at the release's tag
       with Astro's `base` set to `/v<major>.<minor>/` and deploys it to the Pages project as a
       branch alias named for the line. A Pages Function on the root deployment serves
       `/v<major>.<minor>/*` from that alias, so a new line needs no DNS, domain or project setting
       of its own.
     - **How the picker finds them.** The root build writes `/versions.json` from the release
       tags (one entry a minor, newest first, with its newest patch). The picker, a Starlight
       component override in the header, reads it from the root at run time, so a line's docs list
       the releases cut after it. A versioned page carries a banner naming its line and linking the
       same page on the newest, and its canonical link points at the root page, so search engines
       index one copy.
- **Context:** No release has shipped. ADR-2139 put a 7-day, 3,000-system soak in front of every
  final release, and on a pre-1.0 project with no production users that week held `v0.1.0` back
  indefinitely. ADR-2711 then had the commits pick a release branch, but a branch per minor made
  every patch a cherry-pick, even when `main` held nothing but fixes, so a minor and a patch were
  cut two different ways. The architect asked for one process with no thought in it, like the
  semantic releases this project started with. Windmill (`windmill-labs/windmill`) releases this
  way: release-please on `main`, a patch and a minor cut the same way, no release branches and no
  backports. Unlike semantic-release, which ADR-2139 dropped, release-please releases only when a
  person merges the release PR, and `bump-minor-pre-major` closes the hole ADR-2139 named, the
  first breaking change numbering a release `1.0.0`.
- **Not chosen:**
  - **A release branch per minor, patches by cherry-pick** (ADR-2711). Two processes, and a
    cherry-pick PR for every fix even when `main`'s tip is the patch.
  - **Fast-forwarding the release branch for a patch.** It works only until the first
    cherry-pick, and the `release/*` ruleset takes pull requests only, so it needs a bypass push.
  - **Tagging `main` with our own `make release`.** The same flow as release-please, but a tool
    of our own to keep, where a maintained one already reads the same conventional commits.
  - **semantic-release on every merge.** It releases with no approval, a multi-arch image and a
    GoReleaser run for each of the dozen or more PRs `main` takes a day.
  - **A committed CHANGELOG.md.** release-please's default; the notes already live on the
    Release, and a changelog would be a second copy in every release PR.
  - **Keeping the soak on 0.x.** It keeps the gate that has blocked the first release, for a
    stability promise 0.x does not make.
  - **A subdomain a version** (`v0-1.docs...`). Each line would need a custom domain added by
    hand, a manual step every release.
  - **Rebuilding every version in one deployment.** The root build would grow with every release,
    and a version's docs would be rebuilt by today's toolchain rather than kept as published.
- **Follow-up (not built here):** the release-please workflow, its config and manifest, and
  `version.txt`; `release.yml` run from it on a created release instead of dispatched on a
  branch; `make release`, `make release-plan`, `cmd/release`, `cmd/releaseversion` and their
  packages removed, and the release page rewritten to match; the release App with pull-request
  write and its bypass on the tag ruleset (#1940); the release PR exempted from the lints that
  read a human PR's body; the notice in each release's notes; the versioned docs build, the Pages
  Function, `/versions.json` and the picker; and a release binary's console linking its own
  line's docs.
