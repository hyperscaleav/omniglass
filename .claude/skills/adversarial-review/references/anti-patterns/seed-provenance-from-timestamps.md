## seed-provenance-from-timestamps (fixed; found by the #504 review)

- **Cue:** a boot seed that repairs or reasserts rows it judges "untouched" from `updated_at =
  created_at`, over a table operators also insert into.
- **Failure:** an operator's own insert that was never revised carries equal timestamps too, so the
  seed takes it for its own and rewrites it on the next boot (a role declared to accept any type
  starts refusing components when a release ships a role of the same name).
- **Fix:** decide provenance from a fact only the seed's write lacks: every operator write is
  audited in its transaction and the seed never is, so a row no audit row names is the seed's; or
  carry an explicit provenance mark (`repairSeededSlot`, internal/storage/role_declarations.go,
  ADR-0508).
