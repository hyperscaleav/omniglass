---
title: "An MCP client may name itself by a metadata document URL, fetched through egress"
date: 2026-11-16
status: "Accepted"
summary: "An MCP client may use an https URL serving its metadata as its client_id instead of registering. The server fetches it through the egress contract, 5 KB at most, requires the document to name its own URL, keeps it ten minutes, and refuses a client whose refetch fails. The consent page names the host serving the document"
refs: [2074, 2048, 1179]
---

- **Date:** 2026-11-16 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/#mcp-clients-sign-in-with-oauth-21), [connect an agent](/guides/agents/#sign-in-from-the-client)
- **Decision:** (#2074, extending
  [ADR-2048](#adr-2048-an-mcp-client-signs-in-with-oauth-21-for-an-agent-token-linked-to-it).)
  1. **A URL client id is a document to fetch.** An https `client_id` with a path is fetched at
     authorize; the authorization server metadata advertises `client_id_metadata_document_supported`.
     Dynamic registration stays for clients that do not publish a document.
  2. **The fetch is the egress contract's.** It goes through the same outbound primitive a webhook
     does, with no allow list of its own: private and loopback addresses are always refused at dial
     time (the install's allow list governs only its own notification sends, never this fetch), and
     the body is capped at 5 KB. The document is never reflected to the caller; a
     failure is a 400 with no redirect.
  3. **The document must name itself.** Its `client_id` must equal the URL it was fetched from, and
     its redirect URIs obey the registration rules (https, or http on loopback), compared exactly
     except for the port of an http loopback URI. A published document cannot know the port a native
     client will listen on (Claude Code's lists `http://127.0.0.1/callback`), so a loopback redirect
     matches on any port, as OAuth 2.1 requires; this applies to registered clients too.
  4. **Cached ten minutes, fail closed.** The client is stored keyed by its URL, so the consent
     page, the credential link and the session list work as for a registered client. After ten
     minutes authorize fetches again; a refetch that fails refuses the client rather than trusting a
     copy its publisher may have withdrawn. The token and refresh grants resolve the URL without
     fetching. The client is named by its URL alone: its stored id, which the consent page carries,
     is no client at authorize, token or refresh, and consent refuses a document past its ten
     minutes, so no path reaches the stored redirect URIs without the freshness check.
  5. **The host is shown.** The consent page names the host serving the document beside the
     client's name, which the client chooses freely.
  6. **The client id may arrive as a Basic username.** The go-sdk's client sends it that way (RFC
     6749 2.3.1); the server reads it there, ignores the empty password, and refuses a form value that
     differs.
- **Context:** The MCP authorization spec prefers client metadata documents to dynamic registration:
  a client is identified by a URL its publisher controls, with no registration row per install to
  prune.
- **Limits:**
  - An unauthenticated caller can make the server fetch a public URL. The egress contract bounds where
    and how much, and the response is never echoed, but there is no rate limit of its own.
  - A document withdrawn by its publisher keeps working for up to ten minutes.
- **Not chosen:**
  - **A direct HTTP client.** It would bypass the private-address guard that makes an
    attacker-named URL safe to fetch.
  - **Honoring HTTP cache headers.** A fixed window is simpler to reason about and test.
  - **Using a stale copy when a refetch fails.** It would keep a withdrawn client signing in.
