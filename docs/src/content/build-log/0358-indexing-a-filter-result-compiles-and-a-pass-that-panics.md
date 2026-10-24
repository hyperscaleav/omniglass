---
title: "Indexing a filter result compiles, and a pass that panics says which (#1003)"
date: 2026-10-22
refs: [1003]
---

`filter(members, .has.a)[0].value.a` was refused at compile time with a recovered nil pointer
surfaced as a type error, while `members[0].value.a` compiled. The fault is upstream, in Expr
v1.17.8: its optimizer rewrites `filter(list, p)[0]` and `[-1]` into `find` and `findLast` and
copies the old node's type onto the rewrite without the record's field table, so its compiler
panicked on the field read. Expressions now compile with Expr's optimizer off until upstream is
fixed (#1006). The typed checks no longer read folded constant literals (an unoptimized literal
list is still its own node), and charging is unchanged, since every charge wraps its operand
before Expr would have folded it: a 50-construct corpus decides identically with the optimizer on
and off, and the lowering oracle still compares 21,324 decisions identically. The filter's list is
still charged its full length (`TestIndexedFilterResultCharged`). Each compile pass, the engine's
and Expr's, now runs under a guard: a panic in one becomes a positioned type error naming the
stage and the construct it was on, never a nil pointer and a stack (`TestPassPanicNamesThePass`).
