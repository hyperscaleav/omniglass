---
title: "The issue forms emit the dispatcher's ticket contract, and tier and risk join the label set"
date: 2026-11-09
status: Accepted
summary: "Accepted: every non-epic issue form is the five-section ticket contract the dispatcher launches on (Goal, Spec, Acceptance criteria, Out of scope, Files likely touched), each form sets its native Issue Type, and two closed label families join the taxonomy, tier:{jr,mid,sr} and risk:{low,high}, applied from the forms' dropdowns by a labeler workflow because a GitHub form cannot apply a label from a dropdown. The open tickets filed before the contract are rewritten into it in the same pass."
refs: [1602, 1138]
---

- **Date:** 2026-11-09 | **Status:** Accepted | **Pages:** [labels and the board](/contributing/labels/), [autopilot](/contributing/autopilot/)
- **Decision:**
  1. **One ticket shape, the dispatcher's.** The bug, feature, chore, docs and spike forms each
     render the same five required textareas, headed exactly as the dispatcher's lint matches them:
     `### Goal`, `### Spec`, `### Acceptance criteria` (a checkbox list, seeded with the failing-test
     line), `### Out of scope`, `### Files likely touched`. The house questions the old forms asked
     as their own fields (surfaces, how it is tested, the thin cut and what it defers, permission and
     scope, the timebox, the learning surface) become the descriptions and placeholders of those
     five sections, so nothing the forms used to ask is lost and a form-filed ticket passes the lint
     as filed.
  2. **The forms set the Issue Type.** Each form carries the top-level `type:` key (`Bug`,
     `Feature`, `Task`; `Epic` on the epic form) and drops its old `labels:` line, which named
     labels the repository does not have (`bug`, `feature`, `chore`, `docs`, `spike`) and so applied
     nothing. The epic form keeps `labels: ["epic"]`: that is the label the dispatcher refuses, by
     design, so an epic is never launched.
  3. **Tier and risk are labels, applied from the form.** `tier:{jr,mid,sr}` and `risk:{low,high}`
     join `area:*` as describing families (the dispatcher reads them: the starting rung, and whether
     the review is a high-risk one). Every form has a `Tier` and a `Risk` dropdown; because a GitHub
     issue form cannot apply a label from a dropdown, the answer renders into the body under
     `### Tier` and `### Risk`, and `ticket-labels.yml` runs `scripts/ticket-labels.sh` on every
     issue open and edit to make the labels match, removing a stale sibling of the same family. The
     script is pure over `gh issue view --json body,labels` and is unit-tested under `go test
     ./scripts/`; an issue without the two sections is left alone, so hand-filed and agent-filed
     tickets carry the labels directly.
  4. **The backlog is rewritten, not grandfathered.** The open tickets filed before the contract do
     not pass its lint, so the same pass that adopts it triages every open ticket (keep, kill,
     combine) and rewrites each kept one into the five sections with its tier and risk; kills and
     combines are staged for the architect, body rewrites are reversible through the edit history.
- **Divergences and follow-ups:** none to a page's design; the labels page said the taxonomy was
  two families and now says three. The dispatcher's own repo installer that would place these forms
  is not wired yet; the forms are hand-maintained here until it is.
- **Context:** the dispatcher launches an agent session on a ticket only when the ticket alone is
  enough to work from, and it checks that mechanically before launch: the five sections present and
  non-empty, at least one checkbox criterion, exactly one tier and one risk label, no epic label.
  The forms predated that check and asked good questions under different headings, so every
  form-filed ticket failed it and every dispatch needed a hand rewrite first. Making the forms emit
  the contract moves the rewrite to filing time, where the filer knows the answers.
- **Rejected:** a label-free contract (tier and risk inside the body): the dispatcher queries by
  label, and a body field cannot be filtered in the issue list. Adding the labels through the form's
  `labels:` key: a form can only apply a fixed label list, not a dropdown answer. Keeping the old
  house headings and teaching the lint to accept both: two shapes for one fact, and the lint belongs
  to the dispatcher, which serves more than one repository.
