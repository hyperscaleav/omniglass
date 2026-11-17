---
title: "omnibot is an MCP client on its own agent token, confirming in its client"
date: 2026-11-17
status: "Accepted"
summary: "omnibot drives the server's MCP tools as a one-day agent token it mints from the person's own credential, held to the omnibot channel and revoked when the run ends; the tool's resolved agent policy is enforced in the client (always runs, ask is confirmed by the person, never is not offered), with no pending action on the server, and the omnibot channel is stamped from the credential, never declared"
refs: [1166, 1161, 1179, 2048, 2063]
---

- **Date:** 2026-11-17 | **Status:** Accepted | **Pages:** [AI](/architecture/ai/), [audit](/architecture/audit/), [connect an agent](/guides/agents/#omnibot-the-assistant-in-the-cli)
- **Decision:** (#1166, slice 1 of the re-scoped split under #1161.)
  1. **omnibot is a client of the MCP server, not a path beside it.** It lists and calls the same
     generated tools any agent does, so every call is authorized, scoped, refused and audited by the
     route it lands on. The loop decides only what to ask the person.
  2. **Its identity is a minted agent token, not an OAuth sign-in.** At the start of a run the
     person's token mints an agent token named `omnibot`, held to the `omnibot` channel, for one day;
     at the end the person's token revokes it, however the run ended. OAuth 2.1
     ([ADR-2048](#adr-2048-an-mcp-client-signs-in-with-oauth-21-for-an-agent-token-linked-to-it))
     is for a third-party client the person has to consent to; omnibot ships in the binary the person
     already signed in with, so a consent screen would add a step and no assurance, and a token that
     lives one run is narrower than a client's standing grant. Only a person can mint, so omnibot
     cannot run on an agent token.
  3. **Confirmation is in the client.** Each listed tool carries its resolved agent policy
     ([ADR-2063](#adr-2063-an-install-sets-always-ask-or-never-per-class-of-operation-for-every-agent)): `always` runs, `ask` runs once the person says yes, `never`
     is not offered, and a tool with no readable policy is `ask`. The server holds no pending action
     and no approval queue: a declined call is never sent, and the model is told it was declined.
  4. **The channel is the credential's.** A token held to the `omnibot` channel alone is accepted
     only on the MCP server (and `/auth/me`), and `authn` stamps its rows `channel = 'omnibot'`. What
     the MCP client declares about itself stays in `client`, as asserted. Only the `omnibot` agent may
     be minted for that channel, and only for it alone.
- **Why:** the security rule that an agent never sees a credential holds by construction (the
  agent token cannot reveal plaintext, ADR-1179), and confirmation in the client is the same place
  every other MCP client confirms, so omnibot is held to no weaker and no stronger contract than
  Claude or any other agent.
- **Consequences:** a server-side approval for an unattended agent does not exist; an operator who
  wants a tool never run by an agent sets it `never`. A live model provider is a later slice; until
  then the model is a recording (`--replay`).
