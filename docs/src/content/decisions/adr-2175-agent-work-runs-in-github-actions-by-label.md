---
title: "Agent work runs in GitHub Actions, picked by label"
date: 2026-11-18
status: "Accepted"
summary: "Claude Code agents build, review and fix Omniglass work as GitHub Actions jobs on GitHub-hosted runners. The architect picks the builder per ticket with a label (run:dev-gha for Actions, run:dev-hah for the harness dispatcher), only the maintainers team's label starts a build, and only the maintainers' and contributors' words reach a model. Required checks and rulesets are unchanged, and no agent can merge"
refs: [2175]
---

- **Date:** 2026-11-18 | **Status:** Accepted | **Pages:** [agents in CI](/contributing/agents-in-ci/), [labels](/contributing/labels/#runtask-triggers)
- **Decision:** (#2175, decided by the architect.)
  1. **Hosted runners, the off-the-shelf action.** Builds, reviews and fixes run Anthropic's
     `claude-code-action` on GitHub-hosted runners, authenticated with the architect's Claude
     subscription and switchable to an API key or Bedrock by one input. No self-hosted runner
     runs code from this public repo.
  2. **The label picks the builder.** `run:dev-gha` sends a ticket to `agent-build`, `run:dev-hah`
     to the harness dispatcher; both labels build nowhere. The label is the approval and is taken
     off as work starts, so applying it again is the retry. The board shows status only.
  3. **Teams gate the triggers.** A build starts only from a `maintainers` member's label on an
     issue the `maintainers` or `contributors` team wrote. A fix starts only from those teams'
     `@claude` or review, or from agent-review's Changes requested, twice per PR at most. The
     decisions are one tested script, `scripts/agent-trust.sh`.
  4. **Outside text stays out of the model.** The build reads the ticket from a file holding the
     body and the two teams' comments; the fix reads only the feedback that started it; the
     conversation is locked while a build runs; no `pull_request_target`, no forks, no bot but
     the agents' own and the harness's.
  5. **Nothing changes the merge gate.** `agent-review` is advisory, the agents' App has no
     Workflows permission, and both main rulesets still need the architect's approval.
- **Not chosen:** self-hosted runners (they would run public-repo code on the architect's
  hardware); triggering on board moves (Actions cannot listen for them, so it would mean polling,
  and it would not keep comments out of the model); the official Claude App as identity (it holds
  Workflows write).
