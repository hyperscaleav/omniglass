## missing-audit-row (live)

- **Cue:** a new privileged mutation (fleet or IAM write) whose handler does not write
  the audit row in the same transaction; a new auth event path with no `WriteAuthEvent`.
- **Failure:** a committed change with no accountable actor; under impersonation, the
  real actor is lost. The audit page's promise ("who changed this?") silently breaks.
- **Fix:** `writeAuditRes` in the mutation's transaction; `WriteAuthEvent` on the
  read/no-tx auth path. Ship-slice gate 10 is the rollup backstop; catch it per slice.
