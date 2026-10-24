---
title: "The test harness migrates once per binary, not once per test"
date: 2026-08-09
refs: []
form: bullet
---

- **The test harness migrates once per binary, not once per test**
  ([#649](https://github.com/hyperscaleav/omniglass/issues/649)). `storagetest.NewDSN` created a
  database per test and then replayed the entire migration chain into it, so a run of
  `internal/storage` applied roughly 6,600 migrations and spent most of its wall clock in setup:
  the per-test distribution was not a spread of test costs but a spike sitting on a fixed
  provisioning floor. The chain now runs once per test binary into a template database, and each
  test is provisioned with `CREATE DATABASE ... TEMPLATE`, a file-level copy. `schema_migrations`
  is copied with everything else, so a provisioned database stays indistinguishable from a
  migrated one, including to dbmate, and the rollback tests that drive dbmate against a harness
  database are unaffected. Isolation is unchanged and now has a test of its own: a row written in
  one database is absent from the next one provisioned, which also proves nothing leaks into the
  template. Postgres refuses to copy a template that has live connections, and `internal/migrate`
  builds a `dbmate.DB` it never explicitly closes, so the template is sealed rather than assumed
  clean: stray backends are terminated and `allow_connections` is set false, making the copy
  deterministic instead of timing-dependent (a retry on the in-use error would have converted a
  deterministic bug into an intermittent one). Review caught the one regression the speedup
  smuggled in: per-binary work behind a `sync.Once` caches its error permanently, so a single
  transient container hiccup failed every remaining test in the binary where the per-test replay it
  replaced would have cost exactly one test. Success is cached and failure is not, so the next test
  retries. `internal/storage` fell from 152s to 65s (`b661c4b`, `2a02307`).
