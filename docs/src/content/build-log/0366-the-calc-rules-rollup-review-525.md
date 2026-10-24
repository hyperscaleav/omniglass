---
title: "The calc rules rollup review (#525)"
date: 2026-10-22
refs: [525]
---

The review of the epic's rolled-up diff, fixed before the rollup. The preview withholds an expression over a
system with a member the caller may not read: owners were scoped but members were not, and an expression
reads a member by position (`members[1].value.t`), so a `rule:read` holder of the system could read an
out-of-scope member's own value; the system's row now carries an `error` counting the members out of scope,
while a named reducer, the aggregate the rule's state shows, is still previewed. A rule's inputs read
`observed` and `calculated` values only, as the calculations page always said: the read also folded an
operator's `declared` value, which marks no rule when it changes, and a cleared declaration's `null`
tombstone failed the read and deferred the mark every 30s. Proven by `TestCalcRuleStateAndPreviewAPI` (the
withheld expression, east viewer and fleet), `TestCalcIgnoresDeclaredInputs`, and a delete in
`TestCalcDerivesComponentSamples` that keeps the deleted rule's calculated rows and their lineage.
