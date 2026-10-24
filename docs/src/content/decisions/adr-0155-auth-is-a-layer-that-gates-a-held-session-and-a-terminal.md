---
title: "Auth is a layer that gates a held session, and a terminal refusal reads rejected"
date: 2026-10-23
status: "Accepted"
summary: "Auth is a layer that gates a held session (#1049, the architect's ruling): each attempt runs dial, then the auth step (a driver's login dialog or a transport's own auth), then the arm, then `up`, and nothing borrows the connection before the step passes; its verdict is the existing `endpoint-authenticated` rung. A terminal refusal moves the session to `rejected` (amending ADR-0150's state set), which dials no more and fails polls and commands fast with no throwaway fallback, until the endpoint's session identity changes, an operator's audited `:retry` (gated by `endpoint:retry`) moves the retry counter in the connection fingerprint (ADR-0158), or the driver's slow re-probe comes due; an unrelated edit does nothing. Three silent hang-ups after the final secret are an `auth` refusal (the login budget). `session-reason` carries the reason as a property. One login dialog serves raw tcp, telnet and ssh keyboard-interactive"
---

- **Date:** 2026-10-23 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [nodes](/architecture/nodes/), [glossary](/architecture/glossary/), [drivers](/guides/admin/drivers/)
- **Decision:** The architect's ruling, made in session on 2026-10-23 (#1049), applies the ladder
  the stateless probes climb (dialed, responds, authenticated) to held sessions:
  1. **Auth gates the session.** Every attempt runs dial, then the **auth step**, then the
     listeners' arm, then `up`. The step is declared by the driver (a login dialog in the spec's
     `session` block) or by the transport (one that authenticates on its own); a dialer that cannot
     run a declared step is a `protocol` refusal. Nothing (a poll, a command, an arm) borrows a
     connection whose step has not passed: the dialer is staged (`Connect`, then `Authenticate`,
     then `Open`), so the line reader does not exist until the step passes. The verdict lands as
     the existing `endpoint-authenticated` property (`yes`/`no`, on change), no parallel vocabulary.
     A login that is silent or dropped decides nothing and is an ordinary failed attempt.
  2. **One login dialog.** Steps wait for a prompt (matched on a partial line once the component
     pauses on it, since a prompt ends no line) and send a line that may name a secret input's field
     (`${secret.<input>.<field>}`); `ready` says it passed; a prompt for a step already answered is
     an `auth` refusal. The same primitive answers ssh keyboard-interactive questions. A credential
     is written only to the wire: render faults name the reference, never the value, and text the
     component sends is scrubbed of the sends before it reaches an error or a reason.
  3. **`rejected` joins the state set** (amending ADR-0150's establishing, up, recovering, down).
     A terminal refusal ([ADR-0156](#adr-0156-a-driver-declares-its-terminal-replies-and-a-transport-classifies-its-own-refusals))
     moves the session there at once, without spending the budget; it closes the connection and
     dials no more. Polls and commands over it fail fast with the refusal, and neither falls back
     to a throwaway connection (a command delivery is `session_only` when its driver declares a
     session or its transport has no stateless line).
  4. **Leaving `rejected`** (amended in this PR by the #1056 ruling). A session leaves `rejected`
     only when its connection fingerprint changes ([ADR-0158](#adr-0158-one-connection-fingerprint-gates-every-connection-on-a-ladder-the-driver-declares)): the attached
     secret rotated in the store, the address, port or transport, the driver's login or terminal
     declaration, a pin, or an operator's `POST /endpoints/{id}:retry` (gated by a new
     `endpoint:retry` action the operator role holds, audited), which moves an explicit
     `session_retry` counter on the endpoint (a migration) that is part of the fingerprint. The
     endpoint's `updated_at`, first used as the retry signal, moved on every edit, a label
     included, and pulled a rejected session out; it no longer does. A driver may declare
     `session.reprobe` (at least 5m; default never). A component that hangs up after the final
     secret and before ready, with no refusal text, charges a **login budget** of its own: three
     in a row are an `auth` refusal; a drop before the secret step stays an ordinary failure, and
     a login that passes resets it.
  5. **Visibility.** Each transition lands `session-reason` beside `session-state`, the reason as a
     property an event rule can read (labels do not persist); the node logs a rejection once, at
     warn, at the transition.
- **Context:** Every session failure was retryable (ADR-0150): after the budget a session redialed
  at the capped 30s pace forever, about 2,880 attempts a day, harmless for the tcp line protocol and
  an account lockout for any transport with a login. The retry signal needed a home the node could
  see that nothing else moves; a component that drops a wrong password silently needed a bound no
  driver rule could give it (#1056).
