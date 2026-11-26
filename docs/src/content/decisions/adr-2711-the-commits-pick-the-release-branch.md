---
title: "The commits pick the release branch; the branch numbers the release"
date: 2026-11-25
status: "Superseded by [ADR-2652](#adr-2652-release-please-cuts-every-release-from-main-and-the-soak-gates-10)"
summary: "make release reads the conventional commits squash-merged on main since the newest release line branched and picks the next release branch: a feat, or a breaking change before 1.0, opens release/<major>.<minor+1>; a breaking change from 1.0 on opens the next major; a fix or perf is a patch, a cherry-pick onto the existing branch. It previews the notes and asks before creating the branch. The tag a dispatch cuts still comes from the branch name and the tags."
refs: [2711, 2139, 2142]
---

- **Date:** 2026-11-25 | **Status:** Accepted | **Pages:** [release and CI](/contributing/release-ci/#cutting-a-release-manual)
- **Superseded by** [ADR-2652](#adr-2652-release-please-cuts-every-release-from-main-and-the-soak-gates-10): release-please reads the same conventional commits and cuts every release from `main`, with no release branch, so `make release` and the cherry-pick list are retired.
- **Decision:** (#2711, asked for by the architect: cutting a release should take no thought.) Refines item 4 of
  [ADR-2139](/architecture/decisions/#adr-2139-releases-are-cut-from-a-release-branch):
  1. **The commits pick the line.** `cmd/release` (`make release`) reads the remote's release branches and tags (a tag
     deleted on the remote no longer counts, a local-only one never does) and `main` since the newest release line
     branched (its `release/<major>.<minor>` branch, else its newest tag) as conventional commits, with the same
     parser the notes use (`releasenotes.Parse`). A `feat` bumps the minor, a `fix` or `perf` the patch, anything
     else alone releases nothing.
  2. **No breaking change numbers 1.0.** A breaking change bumps the minor while the major is 0 and the major only
     from 1.0 on, so the trap ADR-2139 named (semantic-release numbering the first release `1.0.0`) cannot recur.
     Reaching 1.0 stays a decision someone makes by name.
  3. **A minor or major opens a branch; a patch does not.** A new line is created at `main`'s tip after a preview
     of the version, the commits that drove it and the full notes, and a confirmation (`DRY_RUN=1` previews,
     `YES=1` skips the question). A patch lists the fixes the line's branch lacks, matched by patch id,
     `cherry-pick -x` trailer, PR number or description, for cherry-pick PRs.
  4. **The branch still numbers the release.** `cmd/releaseversion` is unchanged: the tag a dispatch cuts comes
     from the branch name and the tags. The tool never dispatches the release workflow and never sets
     `RELEASES_ENABLED`.
- **Consequences:** the PR title sets the release line again, not only its line in the notes, so a mistyped
  `feat` on a fix-only week opens a minor. The preview is the check against that.
