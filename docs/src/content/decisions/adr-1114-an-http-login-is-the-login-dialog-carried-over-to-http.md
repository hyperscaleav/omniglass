---
title: "An http login is the login dialog carried over to http, and its session lives in node memory alone"
date: 2026-10-25
status: "Accepted"
refs: [1114, 1113]
summary: "An http session may declare `session.login` (#1114, slice 2 of epic #1113, its decision 3): the #1061 login dialog with request steps naming only secret fields, a capture of the last answer's values through the one extraction primitive, the headers that send them, a declared expiry, a login path and an expired match telling a lapse. The node keeps one auth state per http endpoint (a cookie jar, the captured values, a digest challenge) in memory alone, dropped when the fingerprint moves, on a terminal refusal, and on restart. A lapse logs in once and replays once; a lapse right after a fresh login, or any refusal of the login itself, is a terminal `auth` tried once per fingerprint. `digest` (RFC 7616, MD5 and SHA-256, `stale`) is a scheme on `basic-auth`. Shaped so an OAuth2 client-credentials grant (#1119) needs no new primitive"
---

- **Date:** 2026-10-25 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [nodes](/architecture/nodes/), [glossary](/architecture/glossary/), [drivers](/guides/admin/drivers/), [simulation](/architecture/simulation/)
- **Decision:** (#1114, under the architect's approval of epic #1113, its decision 3.)
  1. **One login primitive.** An http session's `login` is the login dialog a line session runs
     (#1061), its steps requests rather than prompts. A step's references are only
     `${secret.<input>.<field>}` of declared secret inputs, validated at write, rendered
     injection-safe as a command's are. A login declares a scheme or a login, never both, and no
     re-probe, pty or exec.
  2. **What a login opens.** `capture` names values the last answer carries, each located by the one
     extraction primitive bound to that answer's format; `headers` send them on every later request
     (`${session.<capture>}` only, never a secret, never `Cookie` or the headers the client owns, a
     poll's or a command's own headers never shadowing one); `expiry` reads the session's lifetime in
     seconds, held to 10 seconds to 30 days, and one missing, zero or unreadable declares none. The
     component's cookies ride a jar, kept for a login session alone, at most 64 cookies of 4 KiB. A
     captured value with a control character, or past 8 KiB, refuses. A step's path opens with a
     literal `/`, so no secret renders into the URL's authority.
  3. **Memory alone** (decision 3). The node holds one auth state per http endpoint, shared by its
     polls and its commands: the jar, the captured values and their headers, the digest challenge.
     Never logged, published or persisted; dropped whole when the endpoint's connection fingerprint
     moves (every login input is in it, through the session block and the secrets' sealed versions),
     on a terminal refusal, and with the process. A login runs on a fresh jar.
  4. **A lapse logs in once.** A credentialed answer that is a 401, a redirect to the declared login
     `path`, or the declared `expired` text has lapsed: the node logs in once more and replays the
     request once. A lapse right after a fresh login (the request's own, or another request's that
     opened the session meanwhile) is a terminal `auth` refusal, never a second login. A declared
     expiry renews the session with a tenth of its lifetime to spare.
  5. **A refused login is one attempt.** A login answer that is a declared terminal rule, a 401, a
     redirect back to the login page, any other 4xx, or a 2xx at the last step without the `ready`
     text or a value it captures, is the component refusing the login: a terminal `auth` at the session
     rung, through the gate and its reason (#1110). The state holds the refusal, so concurrent
     requests cannot try twice; the gate holds every request beneath it across a restart. A login
     drawing no verdict after its secrets were sent (a 5xx, a 408, a 429, a connection dropped after
     the request was written) is retryable, but three in a row spend the login budget and read
     `rejected`, as a line session's silent hang-up does; a connection never made charges nothing. A
     redirect elsewhere passes.
  6. **Digest is a scheme on basic-auth.** `scheme: digest` answers RFC 7616: MD5 and SHA-256 and
     their `-sess` forms, `qop=auth` or the legacy form, the strongest challenge offered. The
     challenge is held and answered preemptively with a rising count; a challenge marked `stale`, or
     under a nonce rotated without saying so, is answered afresh (a count once sent under a nonce is
     never sent again, and a `-sess` key keeps one client nonce per challenge), and a 401 to an
     answer of the nonce still offered is the credential's refusal. A 401 with no challenge the node
     answers is the request's own fault: the credential was never sent. The password never crosses
     the wire.
  7. **Never logged.** Everything the state holds (the secret fields a login sent, the captured
     values, the cookies) is scrubbed from every reason and error in every form a component could echo,
     and from every URL the node names; what a login would capture is scrubbed even from the reason
     refusing it. An exchange prints as its method and path alone. The node logs a login's reason
     (`opening`, `renewing before expiry`, `the session lapsed`), never a value. A command whose
     delivery names an address the worklist does not is never sent, so a session never rides to
     another component.
  8. **Shaped for OAuth2.** A client-credentials grant (#1119) is a request step posting the client's
     secret fields, a captured `access_token`, its `expires_in` and a bearer header: no reshaping, only
     a secret type. A client authenticating with a Basic header over its id and secret needs a
     template function this slice does not add.
  9. **The simulated faces.** The simulator's http gate gains `form` (a sign-in page and a session
     cookie), `token` (a JSON login answering a token and its lifetime) and `digest` (MD5 or SHA-256,
     a nonce that goes stale on a replay or a rotation); sessions live in the device's memory, and
     the console expires them all through the face's `expire_path`, as a reboot would. The Newtron
     panel's https API takes the form login and the Boreal display's web admin the digest, read by
     the seeded `newtron-panel-web` and `boreal-edge-web`.
- **Context:** slice 1 (#902) authenticated every http request by a per-request scheme, but much
  gear's web API signs in once and expects the session back. The epic's decision 3 ruled the session
  memory-only, so a stolen database or a log line never yields a live session, and the login dialog
  #1061 had already settled the shape and the one-attempt discipline for line sessions; carrying it
  over kept one primitive and one fingerprint input. Digest rides basic-auth because it is the same
  credential presented another way.
