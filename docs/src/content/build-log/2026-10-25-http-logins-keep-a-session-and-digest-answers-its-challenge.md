---
title: "http logins keep a session and digest answers its challenge (#1114)"
date: 2026-10-25
refs: [1114, 1113]
---

The second slice of the second protocol wave (#1113), under its decision 3
([ADR-1114](/architecture/decisions/#adr-1114-an-http-login-is-the-login-dialog-carried-over-to-http-and-its-session-lives-in-node-memory-alone)).
An http session declares `session.login`: the login dialog #1061 shipped, its steps requests naming
only secret fields, a `capture` of the last answer's values through the one extraction primitive,
the `headers` that send them, an `expiry`, a login `path` and an `expired` match
(`internal/driver/httplogin.go`, every field validated at write and a fingerprint input). `digest`
joins the schemes on `basic-auth`.

The collection engine (`internal/collection/httpsession.go`) runs the login on a fresh cookie jar,
keeps what it opened in an `HTTPAuthState`, renews before a declared expiry, and on a lapse (a 401,
a redirect to the login page, the expired text) logs in once more and replays once; a lapse right
after a fresh login, or a refused login, is a terminal `auth` held for the life of the state, so it
is tried once, and three logins in a row that draw no verdict after their secrets were sent spend
the login budget. Digest (`internal/collection/digest.go`) answers MD5, SHA-256 and their `-sess`
forms, holds the challenge and answers a stale or quietly rotated nonce afresh; a 401 it cannot
answer never sends the credential. The node keeps one state per http
endpoint, in memory alone, shared by its polls and commands and dropped when the fingerprint moves
or a terminal refusal lands; a command never rides to an address its worklist does not name.
Everything the state holds is scrubbed from reasons, errors and URLs, a jar keeps cookies for a
login session alone (bounded), and an adversarial security review's findings shaped the bounds on
an expiry, a cookie, a challenge and a response's headers.

The simulator's http gate gains form, token and digest faces, with sessions the console can expire
through a face's `expire_path`; the Newtron panel's https API now signs in with a form and the Boreal
display's web admin answers digest. The seed ships `newtron-panel-web` and `boreal-edge-web`.

Proven in the driver (validation, refusals, rendering, headers, expiry), in collection against the
RFC 7616 vectors and over real sockets (`httpsession_integration_test.go`: one session across
requests, one re-login on a lapse, a wrong password tried once, a renewal before expiry, concurrent
requests sharing one login, digest with a stale nonce, nothing held in any reason or URL), in the
simulator (each face, the console's trust and expiry), in the fingerprint (every login field moves
it), and end to end (`run_http_login_test.go`: form and token logins keep one session across many
polls and log in exactly once more on expiry; digest answers its challenge, a stale nonce and a
command; a wrong password reads `rejected` with its reason after exactly one attempt across a
restart, and the secret fixed in place brings each back).
