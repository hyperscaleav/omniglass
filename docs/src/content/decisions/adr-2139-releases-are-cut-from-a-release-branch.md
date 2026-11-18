---
title: "Releases are cut from a release branch"
date: 2026-11-18
status: "Accepted"
summary: "A release is cut from a release/<major>.<minor> branch made from main, as numbered candidates (v0.1.0-rc.1, rc.2) and then a final tag on the newest candidate's commit once it has soaked. The version comes from the branch name and the tags already cut, not from commit subjects, and fixes reach the branch by cherry-pick"
refs: [2139, 1940, 1988, 1257, 64]
---

- **Date:** 2026-11-18 | **Status:** Accepted | **Pages:** [release and CI](/contributing/release-ci/#cutting-a-release-manual), [a soak run](/guides/lab/#a-soak-run)
- **Decision:** (#2139, decided by the architect for 0.1.0.)
  1. **A release branch, not a frozen main.** On the cut, `release/<major>.<minor>` is made from
     `main`, and `main` stays open. The release workflow runs only on such a branch.
  2. **Candidates, then a final.** Each dispatch tags the branch tip as the next candidate,
     `v<major>.<minor>.<patch>-rc.<n>`, a GitHub prerelease with its own image and binaries. The
     soak gate (#1988) runs a candidate. A dispatch marked final tags the newest candidate's commit
     `v<major>.<minor>.<patch>` and is refused on any other commit, so the release is exactly what
     soaked. After a final, the branch's next candidate is the next patch.
  3. **Fixes by cherry-pick.** A fix lands on `main` by its own PR, then reaches the branch by a
     cherry-pick PR; the next candidate's soak starts over.
  4. **The version is the branch's, not the commits'.** `cmd/releaseversion` reads the branch name
     and the tags already cut. semantic-release leaves the release path: computing the version from
     conventional-commit subjects would have numbered the first release `1.0.0` from breaking
     changes made before any release, and it cannot hold a version line on a branch. The notes are
     GitHub's generated notes since the previous final release.
  5. **The release App pushes the tag.** The `v*` creation ruleset refuses `GITHUB_TOKEN`, and
     GitHub will not take the Actions app as a ruleset bypass, so the tag is pushed with a token
     from a dedicated release GitHub App, the ruleset's one bypass besides the admin role (#1940).
- **Consequences:** every push to a release branch runs the same CI as `main`, so a candidate has
  had the full CI before it is tagged. Release branches need a ruleset of their own, like `main`'s.
  The PR title no longer sets the version, only the line it writes in the notes.
