---
title: "Sessions authenticate before they act, and a refusal stops the retrying (#1049)"
date: 2026-10-23
refs: [1049]
---

The first slice of the protocol backlog under epic #603, under the architect's ruling of 2026-10-23:
auth is a layer that gates a held session, and non-retryable errors are defined in drivers. The
session machine (`internal/collection/session.go`) runs each attempt as dial, then the **auth step**,
then arm, then `up`, over a staged dialer (`Connect`, `Authenticate`, `Open`) so the line reader does
not exist until the step passes; the real line dialer (`linesession.go`) is staged, running a declared
**login dialog** (`login.go`, one primitive for raw tcp, telnet and ssh keyboard-interactive) over the
raw connection, with prompts matched on a partial line. A **terminal error**
(`collection.TerminalError`, `terminal.go`), declared by the driver's `session.terminal` rules
(`internal/driver/session.go`) or classified by a transport, moves the session to the new
**`rejected`** state without spending the budget: no dialing, polls and commands failing fast with
the reason, never a throwaway connection. It leaves on an identity change (a fixed credential
recycles it), an operator's audited `POST /endpoints/{id}:retry` (the new `endpoint:retry` action,
moving the endpoint's revision, which the worklist now carries), or the driver's slow
`session.reprobe`. The auth verdict lands as `endpoint-authenticated` and every transition as
`session-reason` beside `session-state` (both seeded), and a poll fault that is terminal waits its
interval instead of the failure backoff. Proven over fakes with a fake clock (a wrong credential dials
once over an hour, a retry once more, a 20m re-probe three times an hour, a transient failure still
spends the budget, nothing arms before the auth step), over a real socket (a raw tcp line protocol
behind a password prompt: the right password logs in before the first poll, the wrong one is dialed
exactly once and never arms), against Postgres (the worklist carries the session block and revision,
`:retry` moves it and is audited, a login driver's command delivery is session-only), and over the API
(a viewer is refused `:retry`, an out-of-scope operator gets a non-disclosing 404). Recorded as
[ADR-0155](/architecture/decisions/#adr-0155-auth-is-a-layer-that-gates-a-held-session-and-a-terminal-refusal-reads-rejected)
and [ADR-0156](/architecture/decisions/#adr-0156-a-driver-declares-its-terminal-replies-and-a-transport-classifies-its-own-refusals).
