## unvalidated-param-value (fixed; found by the #1061 in-place edit review)

- **Cue:** a patch path that allow-lists a key but never parses its value.
- **Failure:** the API answers 200 and the node refuses the value at run time (a `timeout` of
  "five seconds"), so the endpoint stops collecting while the operator saw success.
- **Fix:** parse the value where it is accepted and refuse it naming the field
  (`patchedConnection`, internal/storage/endpoints.go).
