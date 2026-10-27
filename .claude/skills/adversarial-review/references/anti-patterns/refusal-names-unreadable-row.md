## refusal-names-unreadable-row (fixed; found by the #504 review and #1216)

- **Cue:** a storage error type that carries another entity's name, id or a figure about it (a
  count, a staffing level), produced by a query that is not filtered by the caller's scope: a
  catalog write (a standard, a type) checked against every conforming row fleet-wide.
- **Failure:** the catalog permission can be held beside a narrow grant that reads none of those
  rows, so the refusal tells that caller which row exists, what it declares, or how it is staffed.
  A count is a leak too: probing successive inputs tallies the hidden rows.
- **Fix:** keep the check fleet-wide (weakening it would break the invariant), carry the ids on the
  error, and redact in the API against the caller's read scope before mapping it: name a readable
  row or none, never a count (`redactRefusalSystem`, internal/api/roles.go).
