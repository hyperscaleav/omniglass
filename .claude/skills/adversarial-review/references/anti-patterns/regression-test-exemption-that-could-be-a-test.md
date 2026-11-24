## regression-test-exemption-that-could-be-a-test (open)

- **Cue:** a PR body closes an issue with the `bug` or `flaky-test` label, or the native Bug
  type, and carries `regression-test: none (<reason>)` while the diff adds or changes no test file.
- **Failure:** the exemption is the only thing standing between a bug fix and the rule that
  it ships a regression test, and the `regression-test` check accepts any non-empty reason.
  A fix with a reproducible failure and no test regresses silently.
- **Fix:** treat the reason as a claim to refute. If the bug can be reproduced in a unit,
  integration or e2e test, the exemption is a blocking finding: ask for the test that fails
  before the fix. Accept it only when the reason holds (a docs-only or config-only fix, a
  failure only real hardware shows).
