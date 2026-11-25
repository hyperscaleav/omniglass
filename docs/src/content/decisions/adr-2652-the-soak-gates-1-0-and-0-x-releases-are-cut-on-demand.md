---
title: "The soak gates 1.0, and 0.x releases are cut on demand from main"
date: 2026-11-24
status: "Accepted"
summary: "The 7-day soak gates a final release from 1.0 on; a 0.x final needs only the full CI its release-branch push already ran. v0.1.0 is cut by hand once the release App is set up, and after it a release is cut whenever the work calls for one, roughly weekly, by one command with nothing to choose, make release, which previews the cut, asks to confirm and dispatches it; the workflow, not the laptop, makes the branch and the tag. The bump is semantic, read from the conventional commits since the newest final: a feat or a breaking change makes the next minor, release/0.<n+1> from main's tip; fixes and the rest only make the next patch, by fast-forwarding the current release branch to main's tip. Either way it cuts a candidate and then a final on that commit, with the tag still from cmd/releaseversion. A scheduled check nudges when a cut is about a week overdue and never cuts by itself. Until 1.0 the README, the docs landing page and the release notes say Omniglass is pre-production, and each release line keeps its own docs behind a version picker"
refs: [2652, 2139, 1988, 1940]
---

- **Date:** 2026-11-24 | **Status:** Accepted | **Pages:** [release and CI](/contributing/release-ci/#cutting-a-0x-release), [the soak gate](/contributing/release-ci/#the-soak-gate), [a soak run](/developer/lab/#a-soak-run), [home](/)
- **Decision:** (#2652, decided by the architect; amends [ADR-2139](#adr-2139-releases-are-cut-from-a-release-branch), whose release branch, candidates, cherry-picked fixes, version rule and release App all stand.)
  1. **The soak gates 1.0, not 0.x.** A final release of `v1.0.0` or later still needs a passing
     soak record for its candidate's build (#1988). A `0.x` final needs only the full CI that every
     push to a release branch already runs, green on the candidate's commit. The soak tooling and
     its records stay; a `0.x` candidate can be soaked by choice, and its record is committed the
     same way.
  2. **v0.1.0 is the first public build.** It is cut by hand, as ADR-2139 describes, once the
     release App is set up and `RELEASES_ENABLED` is on (#1940): `release/0.1` from `main`, a
     candidate, then the final on that commit.
  3. **After it, a cut is one dispatch, made when we want one.** There is no fixed schedule:
     a `0.x` release is cut whenever the work calls for one, roughly weekly, at the frequency it
     needs. A cut is one command, `make release`, and the cut workflow it dispatches. The cut
     reads the conventional commits on `main` since the newest final, where `0.<n>.<p>` is that
     final, and bumps by them:
     - **A feat or a breaking change** (a `!` after the type, or a `BREAKING CHANGE:` footer, as
       the release notes already group them) **makes the next minor.** The cut makes
       `release/0.<n+1>` from `main`'s tip and cuts `v0.<n+1>.0`. Before `1.0` a breaking change
       bumps the minor, not the major, since `0.x` promises no stability.
     - **Anything else** (`fix`, `perf`, `docs`, `chore`, `ci`, `test`, `refactor`, `build`)
       **makes the next patch, `v0.<n>.<p+1>`, on the current line.** The cut fast-forwards
       `release/0.<n>` to `main`'s tip. Since every commit since the final is patch-level, `main`'s
       tip is that line's next patch, and the fast-forward puts exactly the commit `main` already
       ran CI on onto the branch, with no new commit and nothing to conflict.

     Either way it then dispatches a candidate on the branch tip and, once that branch push's CI is
     green, the final on the same commit. Nobody picks a version, a branch or a commit. The cut
     refuses when `main` has no commit since the newest final or `RELEASES_ENABLED` is off, and it
     never cuts `1.0` (a hand cut, with its soak). A cherry-pick to the branch and a hand dispatch
     stay for the rare fix that must ship without the features already on `main`. That
     cherry-pick gives the branch a commit `main` lacks, so the branch can no longer fast-forward:
     the next cut makes the next minor from `main` instead, whatever the commits say, and the
     preview says why. The cherry-picked line takes later patches only by cherry-pick and a hand
     dispatch.
  4. **`make release` previews, confirms and dispatches; the workflow cuts.** Run from any
     checkout, `make release` fetches `main` and the tags and prints the next version, the kind of
     bump (a new `release/0.<n+1>` branch, or a patch on `release/0.<n>`) and the commits that
     drove it. On a yes it dispatches the cut workflow (`gh workflow run`) with that version as
     its one input, an expectation rather than a choice: the workflow works the bump out again from
     `main` and refuses if the answer differs, so a merge that lands between the preview and the
     dispatch cannot ship unseen. `make release DRY_RUN=1` previews and stops. The workflow makes
     or fast-forwards the branch and pushes the tags with the release App's token (#1940), so the
     branch push runs CI, the `v*` tag ruleset holds, and nothing depends on the state of the
     laptop's checkout. The App needs a bypass on the `release/*` ruleset as well as the tag
     ruleset, since that ruleset otherwise takes pull requests only and a pull request cannot
     fast-forward (it lands as a squash or a merge commit). Until the workflow exists, a cut is the
     same steps by an admin, whose bypass on that ruleset allows the branch push. The preview and the workflow
     share one Go command beside `cmd/releaseversion`, as `make release-plan` already runs it.
  5. **A nudge, never an automatic cut.** A scheduled check opens one issue saying a cut is due
     when the newest final is a week old and `main` has commits since it, keeps that one issue
     open rather than opening another, and closes it when the next cut lands. Nothing scheduled
     ever cuts a release.
  6. **The bump is semantic, the tag is still the branch's.** The cut workflow applies
     semantic-release's bump rule to choose between a new branch and a fast-forward;
     `cmd/releaseversion` still turns the branch name and the tags already cut into the tag,
     unchanged, since after a final it already numbers the branch's next candidate as the next
     patch. The semantic-release tool stays out of the release path: it releases on every merge,
     which would cut a release (a multi-arch image and a GoReleaser run) for each of the dozen or
     more PRs a day `main` takes, with no click to approve it, and its default numbers the first
     breaking change `1.0.0`. The yes at `make release` is the approval, and before `1.0` breaking bumps the minor.
  7. **Pre-production until 1.0.** The README, the docs landing page and every release's notes
     say that Omniglass is pre-production software: breaking changes can land in any release, at
     any time, without notice, and it is not for production use yet. The notice comes off in the
     PR that cuts `1.0`, and not before.
  8. **Each release line keeps its docs.** From `v0.1.0` on, the docs for each release line are
     published beside the newest, and a version picker reaches them:
     - **Where.** `main`'s docs stay at the site root (`docs.omniglass.hyperscaleav.com`), so every
       link already in the console, the code and the issues keeps working; a release line's docs
       live under `/v<major>.<minor>/` on the same host. One version a minor: a patch updates its
       line's docs.
     - **How each release publishes them.** Cloudflare Pages already builds the site from the
       repository. A push to `release/<major>.<minor>` builds that branch with Astro's `base` set to
       `/v<major>.<minor>/` (read from the branch Pages builds, `CF_PAGES_BRANCH`) and deploys it as
       the branch's preview alias. A Pages Function on the root deployment serves
       `/v<major>.<minor>/*` from that alias, so a new line needs no DNS, domain or project setting
       of its own.
     - **How the picker finds them.** The root build writes `/versions.json` from the final release
       tags (one entry a minor, newest first, with its newest patch). The picker, a Starlight
       component override in the header, reads it from the root at run time, so a line's docs list
       the releases cut after it. A versioned page carries a banner naming its line and linking the
       same page on the newest, and its canonical link points at the root page, so search engines
       index one copy.
- **Context:** No release has shipped. ADR-2139 put a 7-day, 3,000-system soak in front of every
  final release, and on a pre-1.0 project with no production users that week, plus the wish for a
  first release that feels finished, held `v0.1.0` back indefinitely. Weighed against the risk, a
  0.x release reaches people who are evaluating and contributing, the notice says so, and the soak
  still guards the release that first promises stability. What semantic-release offered was a
  release with no decisions in it and a version that says what changed; a cut that is one
  command, with the bump read from the commits and the branch worked out for it, gives both
  without tying a release to every merge or to the calendar. The question
  "is this the release?" becomes "is it time for the next one?", and the nudge answers it when
  nobody has asked for a week. Versioned docs let a reader on an older build read the docs that
  match it.
- **Not chosen:**
  - **A cut on a fixed weekly schedule.** It would ship whatever `main` holds when the calendar
    says, mid-refactor included, and a week with a fix everyone is waiting on would still wait.
    The dispatch keeps a person choosing the moment, and the nudge keeps the cadence from slipping.
  - **`make release` tagging and pushing from the laptop.** The `v*` tag ruleset lets only
    admins and the release App create tags, so it would work only for an admin, and no workflow
    runs on a tag push, so the tag would build nothing; the candidate would also skip the branch
    push's CI and depend on whatever the local checkout holds. Dispatching keeps the laptop to a
    preview and a yes.
  - **A new minor every cut.** It would number a fixes-only release as a minor, so the version
    would stop telling a fix from a feature.
  - **One long-lived branch, every release a patch.** The same loss the other way: a feature
    release would read as a patch (`v0.1.7`).
  - **Merging `main` into the release branch for a patch.** The merge commit would be a new
    commit `main` never ran CI on, and the tag would land on it. A fast-forward tags the commit
    `main` already tested.
  - **Cherry-picking each commit since the cut for a patch** (ADR-2139's route for a fix). Every
    commit gets a new SHA that `main` never ran, a conflict stops the cut halfway, and when
    everything since the cut is patch-level the result is `main`'s tip anyway.
  - **semantic-release on every merge.** Already dropped by ADR-2139; restated above.
  - **Keeping the soak on 0.x and cutting less often.** It keeps the gate that has blocked the
    first release, for a stability promise 0.x does not make.
  - **A subdomain a version** (`v0-1.docs...`). Each line would need a custom domain added by
    hand, a manual step every release.
  - **Rebuilding every version in one deployment.** The root build would grow with every release,
    and a version's docs would be rebuilt by today's toolchain rather than kept as published.
- **Follow-up (not built here):** `make release` and the Go command it shares with the
  workflow (the bump from the commits, the preview, the confirm, `DRY_RUN=1`); the cut workflow
  (a dispatch whose one input is the expected version, which works the bump out again from the
  commits since the newest final, makes the next minor's branch or fast-forwards the current one,
  dispatches the candidate, waits for the branch's CI and dispatches the final), with the release
  App's bypass on the `release/*` ruleset; the
  scheduled nudge that opens and closes the "a cut is due" issue; the notice in the notes
  `cmd/releasenotes` writes; the versioned docs build, the Pages Function, `/versions.json` and the
  picker, with the Cloudflare project building `release/*` branches; and a release binary's console
  linking its own line's docs.
