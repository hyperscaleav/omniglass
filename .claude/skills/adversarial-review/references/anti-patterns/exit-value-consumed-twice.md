## exit-value-consumed-twice (fixed; found by the #1077 review)

- **Cue:** a single-send result channel (`ch <- cmd.Wait()`, a buffered `chan error` written once)
  read both by a failure branch (a wait that gives up when the process dies) and by a cleanup or
  teardown that waits for the same exit.
- **Failure:** whichever reader sees the exit first takes the only value; when the failure branch
  wins, the cleanup blocks forever, so one fast boot failure becomes a suite-wide timeout panic that
  aborts every later test and never prints the error that started it.
- **Fix:** broadcast the exit by closing a channel and keep the result in a field read after it
  closes (`process` and `watch`, internal/e2e/server_e2e_test.go; `TestADeadServerFailsTheWaitAndFreesItsCleanup`).
