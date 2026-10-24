## scoped-owner-unscoped-member (fixed; found by the #525 rollup review)

- **Cue:** a route scopes the *owners* it computes over (a system the caller may read) but
  evaluates a caller-authored expression over that owner's parts (a system's members) read
  without the component scope, or returns a run-time error that formats a part's id.
- **Failure:** the aggregate is in scope while its parts are not, and an expression can
  address one part by position: `members[1].value.t` in a calc preview returned an
  out-of-scope member's own value to a `rule:read` holder of the system, though the same
  response counted that member as hidden.
- **Fix:** before evaluating an authored expression on an owner, check its parts against
  the part's read scope and withhold the owner when any is out of it (`hiddenCalcMembers`,
  internal/storage/calc_state.go). A named fold over every part (an average) is the
  aggregate the owner's state already shows, and may stay.
