---
title: "Every platform health figure has a rule or a reason (#1983)"
date: 2026-11-15
refs: [1983, 1982, 1977, 1984]
---

The Platform health page's figures had no check that a rule watched them, so three shipped bare
(#1977) and a fourth set was about to (#1980, #1984). `TestEveryPlatformHealthFigureHasARuleOrAnExemption`
now holds each node column, each server panel key (a ratio panel through its numerator) and the pipeline's
loss ratio to the `sample_key` of a shipped event rule or to a one-line reason in
`storage.PlatformRuleExemptions`; an empty reason, an exemption a rule has overtaken, and an exemption
naming no figure each fail it. Today's uncovered figures are on the list as "no rule yet" with the issue
that adds the rule, so each rule slice deletes its lines. The collection page renders the list beside the
rules from the seed facts. `TestPlatformCoverageRefusesABareFigure` drives the gate over a synthetic page.
