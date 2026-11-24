---
title: "Main recaptures the docs screenshots after a merge (#2561)"
date: 2026-11-24
refs: [2561]
---

The `docs-shots` workflow's `recapture` job runs on push to main and release branches: it recaptures
both shot sets and, if any changed, pushes them to `claude/docs-shots-recapture` and opens or updates
one "docs: recapture the docs screenshots" PR as the agents' App, for the architect to approve.
`scripts/docs-shots-guard.sh` fails a PR that modifies, deletes or renames an existing shot; the
`verify` job runs it on every PR. `docs-with-everything.md` states the permanent rule, replacing the
interim note, and `/ship-slice` and `/assess-ui` no longer tell a PR to re-render shots.
