## uuid-as-address (retired 2026-08-02 by the #432 console slices #466/#467/#469/#470; watch for regressions)

- **Cue:** an operator-facing surface (console cell, filter, delete confirm, CLI output)
  rendering or matching a uuid where ADR-0062 says the `name` is the operator address;
  a `_name`-suffixed column or field holding a uuid.
- **Failure:** typing the human handle finds nothing; confirms print opaque ids; created
  rows lose focus because the form keys by handle while rows key by uuid.
- **Fix:** uuid is identity, name is the address: render, filter, and confirm on `name`.
