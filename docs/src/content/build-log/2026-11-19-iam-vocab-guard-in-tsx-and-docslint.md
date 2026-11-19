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
source file in one Vitest run. Patterns and allow lists mirror `internal/vocab/terms.yaml`
exactly, so the two lints agree: the English adjective ("the principal reason") keeps the
word; the noun in a label string does not. A pinned test proves the scanner fires before
the corpus sweep runs.

`internal/docslint/docslint.go` adds the identity admin surfaces (users, user groups,
roles, profile, and the IdentitiesPanel component) to `operatorStrings`. These pages were
absent from the list while the rename (#2075) was in flight; now that no identifier says
principal the scan carries no carve-out.
