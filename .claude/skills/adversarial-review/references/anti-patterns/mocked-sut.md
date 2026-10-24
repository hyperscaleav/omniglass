## mocked-sut (live)

- **Cue:** a test that mocks the system under test, mocks the database in an integration
  tier, or proves a DB-backed behavior only under `-short` or a cached run.
- **Failure:** a green gate that verifies the mock, not the behavior; the exact class the
  capability-wrapping carve-out exists to prevent.
- **Fix:** real Postgres via testcontainers for integration; fakes only at the
  environment-risky capability seam, with the real-implementation test before merge.
