## unscoped-gateway-list (retired 2026-08-01 by #458; watch for regressions)

- **Cue:** a Storage Gateway read that takes a scope/actor and does not bind it into the
  SQL WHERE clause; per-row filtering in Go after an unscoped SELECT.
- **Failure:** the ABAC invariant ("scope injected on every applicable query") is
  violated; rows the actor must not see are fetched and can leak through a later refactor
  or a miscounted filter; N+1 cost besides.
- **Fix:** inject the scope in the query itself. Exemplars: the scoped-CRUD primitive
  (`scopedListSQL`) for single-tier trees, and the arc-scope primitive
  (`arcScopeCTEs` / `arcScopePredicate` in `internal/storage/scopetree.go`, consumed by
  `ListSecrets`) for rows owned on the exclusive arc. The last known instance
  (`ListSecrets`'s per-row Go filter) was fixed by #458; new gateway reads must start
  from one of the two primitives.
