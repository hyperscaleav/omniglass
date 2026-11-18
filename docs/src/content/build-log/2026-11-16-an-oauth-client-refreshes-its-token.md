---
title: "An OAuth client refreshes its token (#2073)"
date: 2026-11-16
refs: [2073, 2048]
---

An MCP client that signed in with OAuth 2.1 now holds an access token good for an hour and a refresh token good for a sliding 30 days, so it stays connected while in use without asking its person again. Each refresh rotates the client's one credential row in place and spends the refresh token; presenting a spent one again ends the grant, and a refusal for another client, an expired token or a disabled person spends nothing. The authorization server's metadata and the registration both admit the refresh grant, and the session list keeps a client whose access token has expired while its refresh token is good. ADR-2073 records the rotation.
