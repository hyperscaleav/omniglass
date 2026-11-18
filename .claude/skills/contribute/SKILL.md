---
name: contribute
description: "Use when someone wants to add a feature to omniglass, request one, or report a bug, and the right path is not yet chosen: routes the ask to the skill that owns it (file-bug for a defect, define-work for a feature request or a body of work, run-feature-loop or a single slice to build an approved one, ship-slice before review) and holds the gates between them. A router: it carries no procedure of its own."
---

# Contribute to omniglass

Every change starts as an issue and ends as a reviewed PR. This skill picks the path; the
skills it names own the procedure, so read each one when you reach it rather than working
from memory.

## Which path

| The ask | Path |
|---|---|
| Something is broken, wrong, or drifted | `/file-bug`, then back to what you were doing |
| A new capability, not yet scoped | `/define-work` (a Feature for one slice, an Epic for several) |
| A new device, protocol, or vendor API | `/scope-protocol` first: it places the work, then files it |
| An approved definition, ready to build | `/run-feature-loop` |
| One approved slice, ready to build | the slice workflow below |

When the ask is ambiguous (a "bug" that is really a missing feature), say which path you
took and why in the issue body.

## Gates

- **No branch before an approved issue.** A feature request ends at the filed definition and
  the architect's approval comment; do not start building on the same turn.
- **Dedupe first.** Every path that files begins with a search of open and closed issues.
- **Never merge.** A PR is ready for review after `/ship-slice`; merging is the architect's.

## The slice workflow, by the skill that owns each step

1. Worktree from `origin/main` (repo CLAUDE.md, Workflow).
2. The failing test first ([test-driven](../../../docs/src/content/docs/contributing/test-driven.md)).
3. Build with the skill for the surface you touch: `/add-api-route` for a route,
   `/storage-schema-change` for schema, `/add-inventory-view` for an inventory page,
   `/assess-ui` for any console change, `/docs-diagram` for a diagram.
4. Docs in the same PR, a decision under `/record-decision` where the design moved.
5. `/adversarial-review`, then `/ship-slice`; its ship-review is the PR body.
