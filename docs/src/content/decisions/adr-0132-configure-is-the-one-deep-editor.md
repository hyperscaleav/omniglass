---
title: "Configure is the one deep editor"
date: 2026-08-25
status: "Accepted"
summary: "Every fleet workspace carries a Configure tab as the ONE deep editor; the blade stays a quick face whose rows jump into it; `?edit=1` retargets to Configure and the classic detail face retires with an explicit miss face (#800, diverging deliberately from ADR-0129's edit-in-blade target)"
---

- **Date:** 2026-08-25 | **Status:** Accepted | **Pages:** [ui](/architecture/ui/)
- **Decision:** Every fleet workspace (system zoom, location zoom, component leaf) carries a
  Configure tab, and it is the ONE deep editor: identity, classification, placement, the
  kind's manage panels, and tags in one savable face. The blade stays a quick face: its label
  pen and tag adder edit in place, and its other rows are jump anchors into Configure
  (`?tab=configure&edit=1#section`). `?edit=1` retargets from the retired edit face to
  Configure (the ADR-0120 URL-fact rule, new destination); the classic detail face and
  `?view=detail` retire entirely, with an explicit miss face replacing the old not-found and
  disambiguation pages.
- **Context:** ADR-0129 kept the classic face alive "until edit-in-blade lands" and named
  edit-in-blade as the target. Building it showed two editors (a growing blade and a
  workspace) would duplicate every manage panel; the #800 ruling (the One Way to Edit float)
  chose one deep editor in the workspace plus a thin quick face, deliberately diverging from
  0129's recorded target. 0129's zoom-by-default half stands untouched.
