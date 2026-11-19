---
title: "IAM vocabulary guard in tsx source and operator strings (#2105)"
date: 2026-11-19
refs: [2105, 2036, 2075, 2076]
---

The vocabulary denylist already blocked retired IAM words in docs prose and in the seed
files the console upserts at boot. This slice extends the guard to operator-visible
`.tsx` source and to the identity admin pages the Go operator-strings lint was not yet
scanning.

A new `describe("IAM vocabulary (ADR-2036)")` block in
`web/src/identity-vocabulary-guard.test.ts` applies the four retired terms (principal,
principal group, service account, human as a kind) to every non-test `tsx` and `ts`
source file in one Vitest run. The patterns and the English-adjective allow arm mirror
`internal/vocab/terms.yaml`: the adjective ("the principal reason") keeps the word; the
noun in a label string does not. The yaml's backtick code-span allow arms are left out,
because a backtick in tsx starts a template literal, not a code span, so a retired word
inside a template literal fails this test even where the Go lint would pass it. A pinned test proves the scanner fires before
the corpus sweep runs.

`internal/docslint/docslint.go` adds the identity admin surfaces (users, user groups,
roles, profile, and the IdentitiesPanel component) to `operatorStrings`. These pages were
absent from the list while the rename (#2075) was in flight; now that no identifier says
principal the scan carries no carve-out.
