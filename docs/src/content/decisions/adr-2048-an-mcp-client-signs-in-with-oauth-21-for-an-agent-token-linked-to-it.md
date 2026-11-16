---
title: "An MCP client signs in with OAuth 2.1 for an agent token linked to it"
date: 2026-11-16
status: "Accepted"
summary: "Omniglass is its own OAuth 2.1 authorization server for MCP clients: discovery from the /mcp challenge (RFC 9728, RFC 8414), open dynamic client registration for public clients (RFC 7591), the code grant with S256 PKCE and the resource indicator, consent on a console page, and an exchange that mints an opaque agent token linked to the client. Refresh tokens and client metadata documents are deferred"
refs: [2048, 1179, 2047, 2073, 2074]
---

- **Date:** 2026-11-16 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/#mcp-clients-sign-in-with-oauth-21), [connect an agent](/guides/agents/#sign-in-from-the-client)
- **Decision:** (#2048, ruled 2026-11-16, completing the OAuth item of
  [ADR-1179](#adr-1179-an-agent-token-is-a-token-type-on-its-persons-credential-and-never-reveals).)
  1. **The server is its own authorization server.** `/mcp` answers a missing or bad token with a
     challenge naming its protected-resource metadata; that names the server's own issuer, whose
     metadata lists the register, authorize and token endpoints. No external identity provider.
  2. **Registration is dynamic and open** (RFC 7591), for public clients only: no client secret, the
     code grant only, redirect URIs that are https or http on a loopback host. A registration nobody
     consented to within a day is pruned on the next registration.
  3. **The grant is the authorization code with S256 PKCE and the resource indicator.** Redirects match
     exactly; an unknown client or unregistered redirect is never redirected to. There is no implicit
     grant, no password grant and no plain PKCE. A code lives five minutes, is stored as a hash, and is
     spent by any attempt to use it.
  4. **Consent is the person's, in the console.** The authorize endpoint hands a valid request to a
     console page, behind the person's own sign-in, which offers the roles whose capabilities the
     person holds. The approval route refuses an agent token and an impersonation, so no agent
     consents for a client.
  5. **The token is an opaque agent token.** The exchange mints a credential row like any agent
     token (named after the client, `mcp` channel only, the approved roles, 30 days), linked to its
     client. A client holds one token per person; signing in again replaces it in one transaction.
     Everything ADR-1179 and ADR-2047 say of an agent token holds unchanged: the intersection, the
     plaintext ceiling, the audit name, revocation.
- **Context:** Minting a token and pasting it into a client's config was the only way to connect an
  agent. MCP clients now speak the MCP authorization spec, which is OAuth 2.1, so the person can
  connect one from the client with a browser approval and never handle the token.
- **Limits:**
  - No refresh token: after 30 days the client signs the person in again (#2073).
  - No client ID metadata documents; a client registers dynamically (#2074).
  - Registration is open, so anyone reaching the server can create a client row; it grants nothing
    until a signed-in person approves it, and the row is pruned if nobody does.
  - The client name a person sees is the one the client declared; the redirect host is shown beside
    it so a loopback client reads as one.
- **Not chosen:**
  - **A JWT access token.** An opaque credential row is revocable at the next call and reuses every
    check an agent token already has; a self-contained token would need its own revocation list.
  - **An external authorization server.** It adds an operator dependency for a flow the server can
    serve from the credentials it already keeps.
  - **Granting the person's full set by default.** The consent asks for roles explicitly, so the
    client holds no more than the person chose.
