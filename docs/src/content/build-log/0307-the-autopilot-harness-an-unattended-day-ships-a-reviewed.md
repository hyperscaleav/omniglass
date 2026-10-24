---
title: "The autopilot harness: an unattended day ships a reviewed slice"
date: 2026-09-17
refs: []
form: bullet
---

- **The autopilot harness: an unattended day ships a reviewed slice**
  ([#844](https://github.com/hyperscaleav/omniglass/issues/844)). The Define gate assumed a present
  architect: no approval comment, no branch, so a day nobody attended shipped nothing while the
  backlog aged. ADR-0138 moves that approval to the PR for slices the autopilot scopes itself: a
  scheduled Routine fires a fresh session each morning, the session reconciles the standing
  autopilot PRs, then takes the first rung that applies (an in-progress loop, an
  architect-approved definition, a `Bug` by board priority, else it files a marked definition in
  the `/define-work` shape and proceeds), and the architect accepts by merging or vetoes by
  closing. The bounds are the point: the authorization layers, the migration rules, the audit
  contract, breaking API changes and dependency majors still wait for a human comment; at most two
  autopilot PRs stay open; merge stays human, always. Two disciplines ship with it because
  unattended is where they pay. `/assess-ui` makes the visual claim checkable: the state list is
  written before the code, the matrix is captured against the real console (`make dev`,
  `shot.mjs`, the seeded fleet, dark-only), every capture is actually read against an eight-line
  rubric, and first-pass failures are named in the PR rather than smoothed over. `/file-bug` makes
  every finding an issue in the house shape rather than a TODO or a drive-by hunk; #845 (the
  viewport flag the squeeze check wants) is the discipline's first artifact. Provenance is a
  marker line in the issue and PR body, never a new label class, the taxonomy staying fixed.
