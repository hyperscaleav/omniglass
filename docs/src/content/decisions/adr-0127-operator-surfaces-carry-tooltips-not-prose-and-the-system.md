---
title: "Operator surfaces carry tooltips, not prose, and the system body goes components-first"
date: 2026-08-20
status: "Accepted"
summary: "Operator surfaces carry no inline explanatory prose (explainers ride the label's tooltip: `InfoTip`, `Eyebrow`); the system body renders components-first, role as a badge, role chrome only where it earns it (quorum > 1, short, or unstaffed)"
---

- **Date:** 2026-08-20 | **Status:** Accepted | **Pages:** [ui](/architecture/ui/), [design system](/contributing/design-system/)
- **Decision:** Two console rules, ruled together on the system zoom review. First, an operator
  surface carries no inline explanatory prose: every explainer rides its label's `(i)` tooltip
  (`InfoTip`; `Eyebrow` is the section form), and the standard-editor's vocabulary (choices,
  alternates, accepted types) never leads an operator view. Second, the system body's unit is
  the COMPONENT, its role a badge on the card; role-level chrome renders only where it says
  something a badge cannot (quorum beyond one, a shortfall, an unstaffed role), because the
  overwhelmingly common room is one role to one occupant, and a box around a single card is
  chrome without information.
- **Context:** The zoom pages shipped with teaching paragraphs inline (the history strip's
  pedagogy, per-role arithmetic on healthy 1:1 roles, "built as all-in-one") and one card per
  role. Reviewing the built page, the architect ruled the prose buries the operator's read
  ("this is an operator's view") and the role-shaped body misrepresents the fleet's common
  case. The pedagogy doctrine is satisfied by hover; #784 tracks sweeping the remaining
  surfaces onto the same rule.
