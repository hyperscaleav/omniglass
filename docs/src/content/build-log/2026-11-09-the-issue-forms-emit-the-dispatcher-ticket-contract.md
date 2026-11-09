---
title: "The issue forms emit the dispatcher's ticket contract (#1602)"
date: 2026-11-09
refs: [1602, 1138]
---

Every non-epic issue form (bug, feature, chore, docs, spike) now renders the five sections the
dispatcher's lint launches on, `### Goal`, `### Spec`, `### Acceptance criteria`, `### Out of
scope` and `### Files likely touched`, each required, with the old forms' house questions folded
into the section descriptions. Each form sets its native Issue Type and gains a `Tier` and a
`Risk` dropdown; a new `ticket-labels.yml` runs `scripts/ticket-labels.sh` on every issue open and
edit to turn those answers into exactly one `tier:{jr,mid,sr}` and one `risk:{low,high}` label,
replacing a stale sibling and leaving an issue without the sections alone (unit-tested under
`go test ./scripts/`). The labels page documents the two new families and the contract table,
`/file-bug` files in the shape, and the open backlog is being rewritten into it in the same pass
(ADR-1602).
