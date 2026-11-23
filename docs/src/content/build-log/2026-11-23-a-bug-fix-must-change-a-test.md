---
title: "A PR that closes a bug must change a test (#2380)"
date: 2026-11-23
refs: [2380, 2183]
---

The bug-fix-ships-a-regression-test rule had no enforcement. `web/e2e/regression-test.mjs` now
reads a PR's closing keywords, looks up each issue's labels, and fails when one is `bug` or
`flaky-test` and the diff changes no test file, unless the body has `regression-test: none (<reason>)`
with a reason. The parser and test-file matcher (`regressiontest.mjs`) are unit-tested. The workflow
that runs it (`regression-test.yml`, modeled on `demo-tag.yml`) needs a maintainer to add it, and to
make the check required afterwards.
