---
title: "Agents act on GitHub as their own App, not the architect (#1281)"
date: 2026-10-28
refs: [1281]
---

Agent sessions held the architect's own token. Every agent PR was the architect's, which the
architect could not approve, and an unattended session had an org owner's full reach. Agent
work is now authored by the `hyperscale-agent` GitHub App. The identity tooling is shared by
every product and lives in [hyperscaleav/agent-harness](https://github.com/hyperscaleav/agent-harness):
`agent-token` mints an installation token narrowed to this repo and is the clone's only git
credential helper, and a `gh` shim hands every `gh` call a fresh token, so the skills and
`scripts/board.sh` work unchanged. With `AGENT_APP_ID` set, the SessionStart check proves the
wiring at minute zero. Testing #1281's open question showed that the cloud sandbox's egress
proxy replaces any GitHub credential with the connected account's, so agent identity is a
property of the self-hosted worker only. The [autopilot page](/contributing/autopilot/#who-acts-as-whom)
records who acts as whom. The `main` ruleset tightening (code-owner approval required) follows
once the first bot-authored PR lands.
