## deadline-cut-effect-rolls-back-its-record (fixed)

- **Cue:** a batch or pass deadline is the parent context of an outbound effect (an HTTP send)
  whose outcome is recorded in a transaction that the same cancel rolls back.
- **Failure:** the effect happened and its record did not, so the next pass repeats it: a
  duplicate page with no crash, on every pass that runs into its deadline under a backlog to a
  slow receiver. Found in the #1321 review of the notification tick.
- **Fix:** stop admitting work that could outlive the deadline, and run each effect and its
  record on a bound of its own detached from the pass's cancel (`internal/notifier` `admits`,
  `RowBound`), with a test that a pass takes no row it could not finish.
