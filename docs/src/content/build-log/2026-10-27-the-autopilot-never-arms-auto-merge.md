---
title: "The autopilot never arms auto-merge (#859)"
date: 2026-10-27
refs: [859]
---

The `/autopilot` skill no longer tries `gh pr merge --auto` after opening its PR. Agent PRs are
authored under the architect's token, so a required approving review can never be given, and
without one auto-merge fired on green CI alone. The architect's manual merge stays the gate
([ADR-0859](/architecture/decisions/#adr-0859-the-autopilot-never-arms-auto-merge-the-architects-merge-is-the-approval)).
