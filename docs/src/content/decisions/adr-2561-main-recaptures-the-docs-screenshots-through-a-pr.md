---
title: "Main recaptures the docs screenshots through a PR the architect approves"
date: 2026-11-24
status: Accepted
summary: "A PR no longer re-renders existing docs shots. A workflow on push to main recaptures both shot sets and, when any changed, pushes them to claude/docs-shots-recapture and opens or updates one PR as the agents' App, with no change to branch protection and no bypass. A guard fails a PR that modifies an existing shot."
refs: [2561, 2192, 1860]
---

- **Date:** 2026-11-24 | **Status:** Accepted | **Pages:** [docs with everything](/contributing/docs-with-everything/)
- **Decision:** (#2561, ruled by the architect in the console review)
  1. **How the recapture reaches the protected branch: a PR, not a bypass.** Branch protection and
     the ruleset are unchanged and no App is added to a bypass list. On push to `main` (and
     `release/**`) the `recapture` job runs `make docs-shots` on the merged tree and, when either shot
     set changed, force-pushes one commit to `claude/docs-shots-recapture` (`claude/docs-shots-recapture-<release>`
     for a release branch) and opens the PR "docs: recapture the docs screenshots" or updates its body
     if one is open. The architect approves it like any PR. If a later recapture matches the committed
     shots the open PR is closed as superseded. The job mints the same agents' App token
     `agent-open-pr.yml` uses and opens the PR as that App; it does not dispatch `agent-open-pr.yml`
     itself, because that workflow's trust gate admits only a maintainer as the triggering actor and a
     workflow's own token is not one.
  2. **Latest main wins.** The job's concurrency group is per branch with cancel-in-progress, so merges
     landing close together coalesce, and a run whose commit is no longer the branch head stands down
     before pushing, so an older tree never overwrites a newer recapture.
  3. **PRs stop modifying existing shots.** `scripts/docs-shots-guard.sh` runs in the `verify` job on
     pull requests and fails a diff that modifies, deletes or renames a file under
     `docs/public/screenshots/` or `docs/screenshots/baseline/`; adding a shot for a new id is allowed.
     The recapture branch is exempt.
  4. **PR-time freshness stays off.** The `freshness` comparison was already off on pull requests; it
     is now the `recapture` job, push-only. A PR's visual evidence stays under `.github/screenshots/<n>/`.
- **Context:** On 2026-11-23 every merge made several open PRs conflict on the same shared PNGs (#2508
  ejected from the queue twice, #2499 merging main three times, thirteen PRs holding). Git cannot merge
  binaries, so the shots needed one writer.
