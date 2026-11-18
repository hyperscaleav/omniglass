---
title: "An OAuth client refreshes a one-hour token with a rotating refresh token"
date: 2026-11-16
status: "Accepted"
summary: "An OAuth-connected MCP client's access token lives an hour and a refresh token renews it for a sliding 30 days. Each refresh rotates the same credential row in place and spends the refresh token; a spent token presented again ends the grant. A refusal for another client, an expired token or a disabled person spends nothing"
refs: [2073, 2048, 1179]
---

- **Date:** 2026-11-16 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/#mcp-clients-sign-in-with-oauth-21), [connect an agent](/guides/agents/#sign-in-from-the-client)
- **Decision:** (#2073, amending point 5 of
  [ADR-2048](#adr-2048-an-mcp-client-signs-in-with-oauth-21-for-an-agent-token-linked-to-it).)
  1. **An hour, renewed for a sliding 30 days.** The exchange issues an access token good for an hour
     and a refresh token good for 30 days. A refresh issues a new pair and restarts the 30 days, so a
     client in use stays connected and one idle for 30 days signs its person in again.
  2. **One credential row per grant, rotated in place.** A refresh replaces the row's secret and
     expiry, so the session list keeps one stable row per client, and revoking it there ends the
     refresh chain (the refresh rows go with the credential).
  3. **A refresh token is single use, and reuse ends the grant.** A refresh token is stored as a hash
     and spent by its refresh. A spent token presented again by the same client means a copy exists,
     so the credential and its chain are deleted, audited as a revoke taken by the client.
  4. **A refusal spends nothing.** A token presented by another client, an expired token, or one whose
     person is disabled is refused with `invalid_grant` and stays as it was, so a person enabled again
     has a client that still connects.
  5. **The rotation is audited.** Each refresh writes an `update` row on the credential, as the
     person's, taken by the client (the audit `agent` name).
- **Context:** ADR-2048 shipped a 30-day access token with no refresh. A long-lived bearer is the
  costliest thing an agent holds; an hour bounds what a leaked access token is worth, and rotation
  with reuse detection bounds a leaked refresh token, as OAuth 2.1 asks of public clients.
- **Limits:**
  - Two refreshes racing with the same token look like a replay to the loser, which ends the grant;
    the client signs its person in again. This is the strict OAuth 2.1 reading, chosen over a grace
    window that would also admit a thief's replay.
  - Each refresh writes an audit row, about 24 a day for a client in constant use.
- **Not chosen:**
  - **A new credential row per refresh.** It would churn the session list and make a revoke chase a
    chain of rows.
  - **A long-lived access token with no refresh.** Simpler, but a leak is good for its whole life.
  - **Not auditing a rotation.** A credential's secret changes; the trail should say when and by whom.
