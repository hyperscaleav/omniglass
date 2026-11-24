---
title: "The docs lint checks console control names against web/src (#2599)"
date: 2026-11-24
refs: [2599, 446]
---

A bold or code span after a UI verb (click, select, choose, press, open, tap, toggle, pick) in
current-tense prose must match a string literal or JSX text in the console source
(`internal/docslint/uilabels.go`), case-insensitively; a `:::design` fence exempts it. The label set is
read from `web/src` at test time, so nothing is generated and nothing can drift. Real labels the scan
cannot see go in `uiLabelAllowlist` with a reason. The sweep fixed one real drift (the standards page
said "parent standard"; the console says "Variant of"). Tests: `uilabels_test.go`.
