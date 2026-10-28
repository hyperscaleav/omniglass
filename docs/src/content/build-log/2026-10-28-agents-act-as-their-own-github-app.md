---
title: "Agents act on GitHub as their own App, not the architect (#1281)"
date: 2026-10-28
refs: [1281]
---

Agent sessions held the architect's own token, so every agent PR was the architect's, which
the architect could not approve, and an unattended session had an org owner's full reach.
`scripts/agent-token.sh` now mints the `omniglass-agent` GitHub App's identity: it signs a
JWT with the App's private key, trades it for an installation token narrowed to this repo,
and caches it privately until ten minutes before expiry. `setup` makes the App's bot user
the clone's commit author and the script the only credential helper for github.com,
clearing the architect's inherited helper rather than outranking it. The `scripts/agent-bin/gh`
shim runs the real `gh` with a fresh token on every call, so the skills and `board.sh` work
unchanged. With `AGENT_APP_ID` set, the SessionStart check proves the wiring at minute zero.
Tests drive both scripts against a fake GitHub API that verifies each JWT with the key's
public half. Testing #1281's open question showed that the cloud sandbox's egress proxy
replaces any GitHub credential with the connected account's, so agent identity is a property
of the self-hosted worker only; the [autopilot page](/contributing/autopilot/#who-acts-as-whom)
records who acts as whom. The `main` ruleset tightening (code-owner approval required)
follows once the first bot-authored PR lands.
