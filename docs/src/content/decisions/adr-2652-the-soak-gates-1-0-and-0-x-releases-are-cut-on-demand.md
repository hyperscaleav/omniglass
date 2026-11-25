---
title: "The soak gates 1.0, and 0.x releases are cut on demand from main"
date: 2026-11-24
status: "Accepted"
summary: "The 7-day soak gates a final release from 1.0 on; a 0.x final needs only the full CI its release-branch push already ran. v0.1.0 is cut by hand once the release App is set up, and after it a release is cut whenever the work calls for one, roughly weekly, by one dispatch with nothing to choose: it makes the next minor, release/0.<n+1> from main's tip, and cuts a candidate and then a final on that commit, with the version still from cmd/releaseversion. A scheduled check nudges when a cut is about a week overdue and never cuts by itself. Until 1.0 the README, the docs landing page and the release notes say Omniglass is pre-production, and each release line keeps its own docs behind a version picker"
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
     needs. A cut is a single dispatch of a cut workflow that takes no inputs. It makes
     `release/0.<n+1>` from `main`'s tip, where `0.<n>` is the newest release line, dispatches a
     candidate (`v0.<n+1>.0-rc.1`), and once that branch push's CI is green dispatches the final on
     the same commit (`v0.<n+1>.0`). Nobody picks a version, a branch or a commit. It refuses when
     `main` has no commit since the last cut or `RELEASES_ENABLED` is off, and it never cuts `1.0`
     (a hand cut, with its soak). A fix that cannot wait for the next cut still goes by cherry-pick
     to the newest branch and a hand dispatch, which numbers it as that line's patch (`v0.<n>.1`).
  4. **A nudge, never an automatic cut.** A scheduled check opens one issue saying a cut is due
     when the newest final is a week old and `main` has commits since it, keeps that one issue
     open rather than opening another, and closes it when the next cut lands. Nothing scheduled
     ever cuts a release.
  5. **The version is still the branch's.** The cut workflow only chooses the branch name;
     `cmd/releaseversion` turns the branch name and the tags already cut into the tag, unchanged.
     semantic-release stays out of the release path: computing the version from conventional-commit
     subjects would number the first release `1.0.0` from breaking changes made before any release,
     and releasing on every merge would cut a release (a multi-arch image and a GoReleaser run) for
     each of the dozen or more PRs a day `main` takes, which is noise rather than a cadence.
  6. **Pre-production until 1.0.** The README, the docs landing page and every release's notes
     say that Omniglass is pre-production software: breaking changes can land in any release, at
     any time, without notice, and it is not for production use yet. The notice comes off in the
     PR that cuts `1.0`, and not before.
  7. **Each release line keeps its docs.** From `v0.1.0` on, the docs for each release line are
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
  release with no decisions in it; a cut that is one dispatch, with the version and branch worked
  out for it, gives that without tying a release to every merge or to the calendar. The question
  "is this the release?" becomes "is it time for the next one?", and the nudge answers it when
  nobody has asked for a week. Versioned docs let a reader on an older build read the docs that
  match it.
- **Not chosen:**
  - **A cut on a fixed weekly schedule.** It would ship whatever `main` holds when the calendar
    says, mid-refactor included, and a week with a fix everyone is waiting on would still wait.
    The dispatch keeps a person choosing the moment, and the nudge keeps the cadence from slipping.
  - **One long-lived branch with a candidate and final each cut.** Each cut `release/0.1` would
    have to take `main` by a merge, which contradicts fixes by cherry-pick and makes the branch a
    lagging copy of `main`, and every release would be a patch (`v0.1.7`), so a patch would no
    longer tell a fix from a feature. A new minor a cut uses `cmd/releaseversion` as it is, at the
    cost of a branch a release and a candidate tag beside each final.
  - **semantic-release on every merge.** Already dropped by ADR-2139; restated above.
  - **Keeping the soak on 0.x and cutting less often.** It keeps the gate that has blocked the
    first release, for a stability promise 0.x does not make.
  - **A subdomain a version** (`v0-1.docs...`). Each line would need a custom domain added by
    hand, a manual step every release.
  - **Rebuilding every version in one deployment.** The root build would grow with every release,
    and a version's docs would be rebuilt by today's toolchain rather than kept as published.
- **Follow-up (not built here):** the cut workflow (a dispatch with no inputs that makes the
  branch, dispatches the candidate, waits for the branch's CI and dispatches the final); the
  scheduled nudge that opens and closes the "a cut is due" issue; the notice in the notes
  `cmd/releasenotes` writes; the versioned docs build, the Pages Function, `/versions.json` and the
  picker, with the Cloudflare project building `release/*` branches; and a release binary's console
  linking its own line's docs.
