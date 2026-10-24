---
title: "The autopilot ships a daily slice, and PR review is its approval gate"
date: 2026-09-17
status: "Accepted"
summary: "A scheduled daily session (the autopilot) ships one thin slice per day: for slices it scopes itself it files the definition in the `/define-work` shape with the `Mode: autopilot (ADR-0138)` marker and proceeds, the Define gate's approval comment moving to the architect's review of the PR (merge accepts, close vetoes). An in-progress loop, an approved definition, and a priority `Bug` outrank self-scoping; invariant surfaces (the authorization layers, the migration rules, the audit contract), breaking API changes and dependency majors stay behind a human approval comment; at most two autopilot PRs stay open; provenance is a marker line, never a new label class; the autopilot never merges"
---

- **Date:** 2026-09-17 | **Status:** Accepted | **Pages:** [autopilot](/contributing/autopilot/),
  [slice workflow](/contributing/slice-workflow/), [feature loops](/contributing/feature-loops/)
- **Decision:** A scheduled daily session (the autopilot) selects and ships one thin slice per
  day. Work is taken in a fixed order: an in-progress loop, an architect-approved definition, an
  open `Bug` by board priority, and only then a slice the autopilot scopes for itself. For that
  last rung the Define gate's human approval comment is replaced by the architect's review of
  the PR: the autopilot files the definition issue in the `/define-work` shape, marks it
  `Mode: autopilot (ADR-0138)`, and proceeds; approving and merging the PR is the accept,
  closing it is the veto. Self-scoping is bounded: anything touching an invariant (the two
  authorization layers, the migration rules, the audit contract), a breaking API change, or a
  dependency major still waits for a human approval comment. At most two autopilot PRs stay
  open (past the cap the day is maintenance), provenance is a marker line in the issue and PR
  body rather than a new label class, and the autopilot never merges to `main`.
- **Context:** The architect cannot attend daily, and the Define gate assumed presence: with
  nobody to comment, no branch could ever start, so unattended days shipped nothing. The
  ship-review already front-loads the veto at the PR (Decisions I need from you; Risk), which
  makes PR review the natural single touchpoint; the slice gates, `/file-bug` capture, and the
  `/assess-ui` visual gate are what keep a self-scoped slice honest between wake-up and review.
  The label taxonomy stayed fixed deliberately: an autopilot marker describes provenance, which
  is neither a subsystem nor an automation trigger, so it rides the body text instead.
