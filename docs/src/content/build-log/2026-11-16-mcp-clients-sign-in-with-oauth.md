---
title: "MCP clients sign in with OAuth 2.1 (#2048)"
date: 2026-11-16
refs: [2048, 1179, 2073, 2074]
---

An MCP client added with just the server's `/mcp` URL now signs its person in: the `/mcp` challenge
points at the protected-resource metadata, the client registers itself, the person approves roles on
the console's new Connect page, and the client exchanges the code (S256 PKCE, exact redirect) for an
agent token named after it, on the `mcp` channel, linked to the client
([ADR-2048](/architecture/decisions/#adr-2048-an-mcp-client-signs-in-with-oauth-21-for-an-agent-token-linked-to-it)).
Before, `TestMCPClientSignsInWithOAuth` (the go-sdk's own OAuth client against the server),
`TestOAuthConsentNeverIssuesARoleThePersonLacks` and `TestOAuthProtocolRefusals` failed with no
endpoints to reach; they pass now, with revocation refusing the client's next call and the audit
naming the client. Refresh tokens are #2073 and client metadata documents #2074.
