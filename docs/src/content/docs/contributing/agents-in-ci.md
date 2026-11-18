---
title: "Agents in CI"
description: "How Claude Code agents build, review and fix Omniglass work in GitHub Actions, who can start them, and what they can and cannot touch."
---

Three workflows run Claude Code agents (Anthropic's `claude-code-action`) on GitHub-hosted
runners, the same runners the [test gate](/contributing/release-ci/) uses, so testcontainers work
and no self-hosted runner ever runs code from a public repo. They are the only builder: the
homelab harness dispatcher they replace is retired. None of them changes a required check or a
ruleset, and none can merge ([ADR-2175](/architecture/decisions/#adr-2175-agent-work-runs-in-github-actions-picked-by-label)).

| Workflow | Starts on | Does |
| --- | --- | --- |
| `agent-build.yml` | `run:dev-gha` on an issue | builds the ticket into a draft PR that closes it, and marks it ready when done |
| `agent-review.yml` | every same-repo PR that is opened, pushed, reopened or marked ready | runs the [adversarial review](https://github.com/hyperscaleav/omniglass/tree/main/.claude/skills/adversarial-review) read-only, posts findings, and passes its check only when it says ship |
| `agent-fix.yml` | `@claude` on a PR, a review that requests changes, or agent-review's own Changes requested | pushes a fix to the PR's branch |

## The flow

Work moves through five stages, and the architect has three gates in them: starting the work,
merging it, and releasing it. Everything between the gates runs without a person.

1. **Backlog.** Ideas land as issues from anywhere: exploration, scoping, bug reports, the
   agents' own follow-ups.
2. **Todo.** The architect and the assistant groom the backlog about daily into tickets in the
   standard format (`/define-work`), with a `tier:*` and `risk:*` label. A ticket that needs
   another one first says so with GitHub's **blocked by** link, and a build refuses to start
   while any blocker is open. **Gate 1:** applying `run:dev-gha` is the
   approval to start.
3. **Build and review.** The builder opens a draft PR, and marks it ready once `make test`
   passes and `/ship-slice` is clean. Ready starts agent-review; its Changes requested starts
   agent-fix, whose push starts agent-review again. The loop ends when the reviewer says ship,
   which turns its check green, or after five fix rounds, when the PR parks with `needs-fred`.
4. **Merge.** **Gate 2:** the architect approves a PR whose checks are all green, the reviewer's
   verdict among them, and it squash-merges to `main`. There is no staging branch: `main` is
   the integration branch, a feature that is not ready ships dark behind a flag, and a bad merge
   is reverted on `main`.
5. **Release.** A `release/x.y` branch is cut from `main` and tagged `rc.N`
   ([release CI](/contributing/release-ci/)). Only fixes are cherry-picked onto it, never
   features. **Gate 3:** the architect approves the final tag.

## Building a ticket

Apply `run:dev-gha` to an approved issue. The run starts only when all of these hold, and
otherwise takes the label off and comments why:

- **You are on the `maintainers` team.** GitHub lets anyone with triage access apply any label,
  so the workflow checks the team itself.
- **The issue was written by the `maintainers` or `contributors` team.** A ticket from outside
  the project is rewritten by a maintainer first.
- **No open PR already closes it.**
- **Nothing it is blocked by is still open.** Mark the order with GitHub's issue dependencies
  (**blocked by** on the issue). If the list cannot be read, the build is refused, not guessed.

The label comes off as the build starts, so applying it again is the retry. The run locks the
issue's conversation to collaborators while it works, moves the ticket to In Progress, and
follows CLAUDE.md like any session: test first, docs with the change, a draft PR with the
template that says `Closes #<n>`, then `/ship-slice`, then ready for review. Linking the PR
moves the ticket to In Review. The model follows the ticket's tier: sonnet for `tier:jr` and
`tier:mid`, opus for `tier:sr`, with a turn cap per tier and a 240 minute limit. A run that
stops early comments on the issue with its link; one that runs out of room leaves the PR a
draft saying what is left.

## Review on every PR

Every same-repo PR that is not a draft gets one adversarial review: inline findings, each with
its failure scenario, and one summary review that requests changes or comments. It never
approves. Its check, `agent-review`, is the verdict: red while its latest review on the PR's
head requests changes, green when it says ship. The verdict is read with the default branch's
`scripts/agent-trust.sh`, so a PR cannot rewrite its own check. The check is not in either main
ruleset yet; making it required is the architect's call. Opus reviews PRs that close a
`risk:high` ticket, sonnet the rest. The reviewer reads the diff and the code, never the PR's conversation.

## Asking for a fix

Mention `@claude` in a PR comment or a review comment, or submit a review that requests changes,
and an agent addresses that feedback and pushes to the PR's branch. It reads only the comment or
review that started it. Write the ask the way you would to a colleague:

```text
@claude the retry in poller.go never backs off; add the backoff and a test for it
```

When agent-review requests changes, agent-fix answers it automatically, up to five rounds per
PR. After the fifth the PR parks: it gets the `needs-fred` label and a comment, and waits for a
person to request changes, mention `@claude` or take it over. Fork PRs
are skipped.

## Seeing an agent at work

An agent run says so where it was started. agent-build posts one comment on the issue and
agent-fix posts one on the PR, each saying the agent is working, with a link to the run. When
the run stops, the same comment is edited to say what happened: the PR a build opened, the commit
a fix pushed, or that nothing was pushed, followed by the agent's own few lines on why. A run that
breaks says so in that comment too.

agent-fix also puts an `agent-fix` check on the PR's head. It stays in progress while the agent
works, then finishes as success when the run pushed, neutral when it pushed nothing, and failure
when the run broke. The check is not required; it is there to be seen.

## Who can start what

| Who | Can start |
| --- | --- |
| `maintainers` team | builds (`run:dev-gha`), fixes |
| `contributors` team | fixes, and their comments reach a building agent |
| anyone else | nothing; their comments never reach a model |

A team member also needs write access to the repo for `@claude` to run, because the action
itself refuses anyone without it. The gate is `scripts/agent-trust.sh`, tested in
`go test ./scripts/`.

## What keeps outside text away from the model

The repo is public, so anyone can open an issue or comment. Labels are not the risk (only
triage access can set them); text a model reads while holding a write token is. Layers, each
enforced in the workflow rather than asked of the model:

1. **The ticket arrives as a file.** The build reads a file the workflow writes: the issue body
   and only the two teams' comments. The fix reads only the feedback that started it.
2. **The conversation is locked** to collaborators while a build runs.
3. **No outside code runs next to a token.** The workflows use `pull_request`, never
   `pull_request_target`, and skip forks. No bot is admitted but the agents' own App.
4. **The token is narrow.** The agents' App has no Workflows permission, so an agent cannot edit
   these files, and both main rulesets still need the architect's approval to merge. The
   reviewer's token is scoped down to reading code and writing reviews.

The action also strips hidden content (HTML comments, invisible characters, image alt text,
hidden attributes) from what it hands the model.

## Setup

The workflows do nothing until the repo variable `AGENT_CI_ENABLED` is `true`. Before that, an
org owner:

- adds the secret `CLAUDE_CODE_OAUTH_TOKEN` (from `claude setup-token`; switching to an API key
  or Bedrock later is one input per workflow);
- adds the variable `AGENT_APP_CLIENT_ID` and the secret `AGENT_APP_PRIVATE_KEY` for the agents'
  GitHub App, which needs Contents, Pull requests and Issues write, Checks write (for the
  `agent-fix` check), Projects write (for the board), org Members read (for the team checks),
  and no Workflows permission;
- creates the `contributors` team (`maintainers` exists), and creates the `run:dev-gha` label.

Every run draws on the subscription the token belongs to, and parallel runs share its limits. A
run that hits a limit fails and is retried by applying the label again.
