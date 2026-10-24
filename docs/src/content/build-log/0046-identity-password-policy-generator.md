---
title: "Identity: password policy + generator"
date: 2026-07-09
refs: []
form: bullet
---

- **Identity: password policy + generator.** A single pure validator (`auth.ValidatePassword`) enforces a
  password policy on the API password surfaces: **create a user** and **self-service change-password**. The
  direct-DB break-glass lanes (`bootstrap`, `set-password`) are exempt (fully trusted, DB-access-gated, the
  recovery path). The policy is **at least 12 characters, not on an embedded common-password denylist, and not
  containing the username** (NIST 800-63B style: length and a blocklist over composition rules). The API bodies
  raise their `minLength` to 12 and map a violation to 422 with a specific message; the console mirrors the
  length and username rules inline (a new `passwordError`) and gates the submit, while the denylist stays
  server-side (a 422 on a manually typed common password). A shared **`PasswordField`** component adds a
  show/hide toggle and, on the New user and change-password forms, a crypto-strong **Generate** action
  (`generatePassword`, a readable charset with no look-alike characters, kept masked and copied or revealed on
  demand) plus a **Copy** button. Proven by an `internal/auth` unit test (short / common / contains-username / valid), an API
  test (a weak, common, or username-containing password is a 422 on create, a strong one is accepted), and web
  tests (the generator's length / charset / policy-compliance, `passwordError`, and the Generate-fills-and-enables
  flow). Completes [#104](https://github.com/hyperscaleav/omniglass/issues/104) and
  [#151](https://github.com/hyperscaleav/omniglass/issues/151); an operator-configurable policy and a HIBP
  breached-password check remain deferred.
