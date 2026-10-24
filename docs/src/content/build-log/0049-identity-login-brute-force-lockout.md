---
title: "Identity: login brute-force lockout"
date: 2026-07-09
refs: []
form: bullet
---

- **Identity: login brute-force lockout.** Failed password logins are now throttled. Two additive columns
  on `human` (`failed_login_count`, `locked_until`) count consecutive misses on a real account and, on the
  5th, lock it for 15 minutes. Inside the window `AuthenticatePassword` refuses every attempt (even the
  correct password) via a new `ErrAccountLocked`, which the login handler maps to the **same generic 401**
  as a bad credential so the lock is not an enumeration oracle (only the `login_locked` audit records it).
  The lock is decided after the argon2 verify (a locked account is not a faster probe), and a correct
  password below the threshold clears the counter. The decision itself is a pure, unit-tested function;
  proven end to end by a storage integration test (5 misses lock, the correct password is refused while
  locked, an expired window lets it through and resets) and a real-endpoint test (the lockout as seen over
  `POST /auth/login`). Per-IP throttling and a configurable threshold are deferred.
