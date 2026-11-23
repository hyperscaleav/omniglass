---
title: "A storage step re-judges a component's event rules when its selection changes (#2475)"
date: 2026-11-23
refs: [2475, 941, 1920, 1921]
---

An event rule settles when the rule changes (a narrowed selector ends its conditions, a material
edit primes it), but nothing settled when the component changed type under a rule that stayed the
same. `reselectComponentRules` in `internal/storage/rule_selection.go` is the primitive for that
move, the event rules' analogue of `markComponentCalcRules`. It takes a component's selection (its
id and its type's lineage, read before and after the write) and reads only the enabled rules naming
a type in either lineage. It primes a rule the component came into from that component's newest
samples (`primeSeries`, now shared with `primeRule`). It ends the conditions and drops the streaks
of a rule the component left (`endFiringsOn`, `endRuleFirings` on one component). A rule that held
the component on both sides is left alone.

Nothing calls the step yet: #941 wires it into a component's product edit, #1920 into a product's
type, #1921 into a type's parent move. Tests: `TestReselectComponentRules`,
`TestReselectComponentRulesBack` and `TestReselectCandidatesBounded` (testcontainers), and
`TestComponentSelectionSelectedBy` (unit).
