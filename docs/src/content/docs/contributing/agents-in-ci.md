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
| `agent-review.yml` | every same-repo PR that is opened, pushed, reopened, marked ready or labeled, every merge-queue entry, and a manual run | runs the [adversarial review](https://github.com/hyperscaleav/omniglass/tree/main/.claude/skills/adversarial-review) read-only, as one call or as parallel lenses, posts its findings as one review, and passes its check only when it says ship |
| `agent-fix.yml` | `@hyperscale-agent` (or `@claude`) on a PR, a review that requests changes, or agent-review's own Changes requested | pushes a fix to the PR's branch |

A fourth, `agent-open-pr.yml`, runs no agent: it opens a PR as the agents' App from a branch a
Claude session pushed, as described in [PRs from a session](#prs-from-a-session).

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
3. **Build and review.** The builder opens a draft PR, and marks it ready once `make preflight`
   passes and `/ship-slice` is clean. Ready starts agent-review; its Changes requested starts
   agent-fix, whose push starts agent-review again. The loop ends when the reviewer says ship,
   which turns its check green, or after five fix rounds, when the PR parks with `needs-fred`.
4. **Merge.** **Gate 2:** the architect approves a PR whose checks are all green, the reviewer's
   ruling among them, and queues it; the [merge queue](/contributing/release-ci/#merging-through-the-queue)
   reruns the required checks on it on top of `main` and squash-merges it when they pass. There is
   no staging branch: `main` is the integration branch, a feature that is not ready ships dark
   behind a flag, and a bad merge is reverted on `main`.
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
moves the ticket to In Review. The model follows the ticket's tier: Sonnet 5.5 (`claude-sonnet-5-5`) for `tier:jr` and
`tier:mid`, opus for `tier:sr`, with a turn cap per tier and a 240 minute limit. A run that
stops early comments on the issue with its link; one that runs out of room leaves the PR a
draft saying what is left.

The App's token lasts an hour, and a build can run longer. When the agent's push or `gh` call
fails for that, it leaves the branch name, and the PR's title and body if none is open yet, in
files under the runner's temp folder and keeps working. After it stops, the workflow mints a
fresh token, pushes that branch (never with force, and only a branch named for this issue), opens
or readies the PR as the agent asked, and does its own closing steps with the new token. agent-fix
does the same for the PR's own branch.

## Review on every PR

Every same-repo PR that is not a draft or from dependabot gets one adversarial review: inline findings, each with
its failure scenario, and one summary review that requests changes or comments. It never
approves. Its check, `agent-review`, is the ruling: red while its latest review on the PR's
head requests changes or opens with a blocking count, green when it says ship. The review is composed and the ruling read
with the default branch's `scripts/agent-review.sh` and `scripts/agent-trust.sh`, so a PR
cannot rewrite its own check. The one exception was the PR that added `agent-review.sh`
(#2211): main had no copy yet, so its own review used the PR's. The check is not in either main ruleset yet; making it required
is the architect's call. The lens review uses Opus on PRs that close a `risk:high` ticket and
sonnet on the rest; the [one-shot review](#the-one-shot-review) uses Sonnet 5.5 on every PR. The
reviewer reads the diff and the code, never the PR's conversation.

The lens review runs as lenses side by side, so a large PR takes minutes, not the twenty one agent
took sweeping it alone (#2211). Each lens is its own job, `lens (<name>)`, with its own context
and a read-only token:

| Lens | Sweeps |
| --- | --- |
| `catalog-1`, `catalog-2`, ... | the [anti-pattern catalog](https://github.com/hyperscaleav/omniglass/tree/main/.claude/skills/adversarial-review/references/anti-patterns), dealt into even shards in name order (`SHARDS` in `scripts/agent-review.sh` sets how many), so a new entry joins a shard without a workflow change |
| `invariants` | the skill's invariants: routes gated, queries scoped, audits in-transaction, migrations idempotent, no fixed test ports, no mocked system under test |
| `neighbor` | the docs claims the diff falsifies |

A lens posts nothing. It returns its surviving findings as JSON, and the `agent-review` job,
which waits for every lens, posts them as one review: a finding on a diff line becomes an inline
comment, any other goes in the review's body. One blocking finding from any lens makes it
request changes (on a PR its own App wrote, GitHub allows only a comment, so the review
comments and opens with the blocking count), so agent-fix still sees one review per round and the five-round cap still
counts rounds, not lenses. A lens that fails, times out or runs out of its turns fails the check rather than passing
without its part; rerunning the failed jobs retries it.

The review is short to read (#2224). Its body opens with one ruling line, `✅ Ship · nothing found`,
`❌ Changes requested · N blocking · M advisory`, or `⚠️ N advisory` when every finding is advisory,
then one strip with each lens's mark, then one line per finding that is not on a diff line. Each
inline comment opens with its finding's title in bold. The failure, the fix and each lens's notes
sit in collapsed `<details>` blocks under them.

A re-review starts from what changed. After the first review, a push is reviewed from the head the
reviewer last reviewed: the PR's own commits since, with what a merge from `main` brought in left
out. That diff is where the lenses look first, not a fence: they read any code, pull up the whole
PR diff, and run `go build`, `go vet` and `go test` when a finding turns on what the code does, and
they raise a finding wherever the new changes cause or expose a problem. A force push that
rewrites that history, or a last review that left a blocking finding in its body rather than on a
line, gets a full review instead. The diff under review also picks the lenses and their turns. A
diff that touches no code (Go, web, shell, SQL or the workflows) runs only the `neighbor` lens,
plus the `recheck` lens when earlier findings are still open, and the turn limit grows with the
diff, from 40 for up to 100 changed lines to 120 past 2,000.

Open findings carry forward, and each one is rechecked. Every unresolved thread the reviewer
opened, outdated ones included, still counts in the next ruling. When any are open, the re-review
adds a `recheck` lens that goes through them one by one against the PR as it is now and says fixed
or open for each. The reviewer then replies on each thread (`✅ Verified fixed at <sha>` or
`❌ Still open at <sha>`, with what it saw) and resolves the fixed ones. A thread stops counting only
when the recheck lens says fixed and no other lens raised it again, or when a person resolves it,
so a blocker nobody fixed keeps the check red. A finding a lens raises again on an open thread is
not posted twice.

### The reviewer on its threads

The reviewer talks on the threads it opened, and its newest review is the one that counts.

- **Replies.** A person's reply on a thread the reviewer opened starts `agent-review` on
  `pull_request_review_comment: created`; a top-level comment, an App's reply and a reply on a person's
  thread do not (`agent-review.sh reply-gate`). One model call, no agent and no tools, sees only the
  thread, the lines it points at and the finding's catalog entry (`reply-prompt`). The reviewer answers on
  the thread and resolves it when the reply settles the point, because the finding was wrong or the reason
  given holds. Otherwise it says what is still missing and leaves the thread open. A reply that also
  mentions `@claude` still starts agent-fix.
- **Advisories are resolved as they are posted (#2222).** "Protect main" counts every unresolved review
  thread, so an advisory that stayed open would block the merge on each rerun. The reviewer posts an
  advisory finding inline as usual, then resolves its own advisory threads once the review is up
  (`advisory-threads`): they stay readable on the diff, collapsed as resolved, and a review with only
  advisory findings leaves no unresolved thread. A blocking finding keeps its thread open and the review
  still requests changes. A resolved advisory is no longer carried forward or rechecked, which is the
  point: it never made the review request changes.
- **Verified fixed resolves.** A thread the reviewer replies "Verified fixed" on is resolved in the same run,
  advisory threads included; `thread-of` finds a thread from its comment when the open list has no id.
- **Supersede.** Each review's first line carries the head it covers (`... · head <sha>`). After posting,
  the reviewer minimizes its earlier reviews as outdated (`minimizeComment`, OUTDATED), edits one GitHub
  will not minimize down to `Superseded by review <id>.`, and dismisses each earlier Changes requested
  review, so the PR's review state is the newest review's: a clean review no longer leaves the PR blocked
  (`supersede`). The verdict still reads the reviewer's latest review on the head.
- **Round count.** Dismissed reviews no longer say CHANGES_REQUESTED, so agent-fix counts the
  `<!-- agent-review round blocking=N ... -->` marker in each review body instead (`rounds`), and the
  five-round park still holds.

The review posts as a second GitHub App, `hyperscale-reviewer`, when one is set up. GitHub lets
no App request changes on a PR it wrote, and the agents' App writes most PRs here, so without
the second App a blocking review on one of those can only comment. The check still fails, since
the ruling reads the blocking count at the top of the comment, but agent-fix does not start.

In the merge queue the check reports without reviewing again: it reads the ruling already
given on the queued PR's head, so `agent-review` can be made required without holding the queue.
A label other than `review:deep` does the same on the PR, so adding one never turns the check
green.
A draft, a fork PR and a dependabot PR are not reviewed, and their check passes with the reason
in its log, as a skipped check would.

### The one-shot review

The lens review costs about $8 a PR, mostly in agent turns that re-read the same skill and
catalog (#2250). The one-shot review does the same sweep in one model call. The repo variable
`AGENT_REVIEW_MODE` picks the reviewer: `lens` (the default) or `oneshot`.

- **One call.** `scripts/agent-review.sh context` lays the whole review into one prompt: the
  skill, every catalog entry and, for a diff under `web/src`, the `solidjs` skill first (the same
  on every run, so it is cached), then the issue the PR closes, the diff, each changed file with
  line numbers (the lines around each hunk for a file past 800 lines), the docs lines that name
  what the diff changes, and each open thread with its lines as they are now. One Claude Code
  call on the plan's token, with no tools, returns the findings and a recheck of every open
  thread in the lenses' format. It runs as the `oneshot` job, outside the checkout, so nothing
  in the PR's own settings loads.
- **Verify only when unsure.** A blocking finding the call could not trace to the end from the
  code it was shown is marked `verify`. Only then does a short read-only agent session (15 turns)
  read the code and run `go build`, `go vet` and `go test` to settle it. A finding it refutes is
  dropped. One it gives no word on still counts.
- **Same ruling.** Its findings go through the same review, carry-forward, ruling and
  five-round cap as the lenses'. A finding it raises on a thread it judged fixed in the same
  call is dropped, so it never blocks on what it just called fixed.
- **A deep review on request.** A maintainer adds the label `review:deep` to run the lens
  review on that PR, in either mode.
- **Shadow mode.** Run agent-review by hand (Actions, agent-review, Run workflow) with a PR
  number, and a past head of it if you like. It runs the one-shot review on that head, writes
  its findings and cost to the run's summary and an artifact, and posts nothing. That is how
  the replay on past PRs is run before `oneshot` becomes the default.

### Red or conflicted PRs wait

A push to a PR that conflicts with its base, or whose other checks already failed, gets no new
review. The `plan` job waits for the PR's other checks, 15 minutes at most, then reviews, or
holds: the check goes red and says why. Push a fix that turns the PR green, and that push is
reviewed. A `review:deep` label is never held.

## Asking for a fix

Mention `@hyperscale-agent` in a PR comment or a review comment, or submit a review that requests
changes, and an agent addresses that feedback and pushes to the PR's branch. `@claude` still works
too. The agent reads only the comment or review that started it. Write the ask the way you would to a colleague:

```text
@hyperscale-agent the retry in poller.go never backs off; add the backoff and a test for it
```

agent-fix answers each point on one line in its status comment, `✅ <point> · Fixed in <sha>`
or `↩️ <point> · Not changed`, with its reasons collapsed under them, and replies
`✅ Fixed in <sha>` on each review thread a commit it pushed fixed. A point counts as fixed only
when its commit is one the run pushed. It never resolves a thread: the reviewer's recheck does
that once it has seen the fix.

When agent-review requests changes, agent-fix answers it automatically, up to five rounds per
PR. After the fifth the PR parks: it gets the `needs-fred` label and a comment, and waits for a
person to request changes, mention `@hyperscale-agent` or take it over. Fork PRs
are skipped.

## Green before it pushes

Both agents run `make preflight` before their last push, and before an agent-build PR is marked ready, and push the finished work only when it passes (agent-build's earlier pushes, [below](#work-survives-an-early-stop), need the step's own tests). It runs what
the PR's required checks run for the files the change touches: it pipes them through
`scripts/ci-changes.sh`, the rule CI's changes job uses, and runs `make gen` with a drift check
(gen area), the web tests (web), the Go and netadmin tests and the migration check against
`origin/main` (go), and the docs build (docs). A docs-only change runs no Go or web tests; a
path no area claims, or anything under `scripts/`, runs everything. The agent-config guard runs
on every push. The e2e tier and the image smoke stay CI-only.
`scripts/preflight_test.go` fails if the target and the workflows it mirrors drift apart.
Run it yourself before you push a PR by hand; it needs Docker and a fetched `origin/main`.

agent-fix merges `main` into the PR's branch before the agent starts, with
`scripts/agent-sync.sh merge`, and hands the agent any conflicts as its first job.
claude-code-action puts main's copies of the agents' own config (`CLAUDE.md`, `.claude/`,
`.mcp.json` and the like) into the work tree, and an agent PR must not commit them: `make
preflight` fails an agent's run that does, and after the run agent-fix puts main's copies
back in one commit if any slipped through. A ticket labeled `area:ci` may change that config;
a PR a person opens is never held to it.

## Work survives an early stop

The agent runs as `claude -p`, which exits the moment the model ends its turn, and the runner
is thrown away with whatever was not pushed. Run 44743523903 on #2223 ended at turn 142 of 200
with a commit and a worktree of edits, nothing pushed, and a green job. So the work is kept two
ways:

- **The agent commits as it goes.** After each completed step (code with its tests green, docs,
  screenshots) it commits, each commit ending `Refs #<issue>`, never `Fixes` or `Closes`.
  agent-build also pushes after each step: there is no PR yet, so no review starts. agent-fix
  pushes once, at the end, after its last commit and a green `make preflight`, because its PR
  exists and every push to it starts an agent-review. agent-build opens its draft PR once, after
  the last push. Neither ends its turn with work outstanding: they wait in the foreground, and
  `Monitor` is disallowed.
- **The workflow saves what is left.** After the agent, `scripts/agent-rescue.sh` reads the
  checkout in a step with no token (no git in the agent's tree beyond `ls-files`, with a scrubbed
  environment) and builds a bare repo holding the branch: its commits, plus any uncommitted files
  as one `wip: agent stopped before finishing, Refs #<issue>` commit. main's agent config is taken
  from the branch, never from the checkout. The next step pushes it with the fresh token, never
  with force, and refuses a branch that touches `.github/workflows`. agent-build also opens a draft
  PR that closes the issue when none exists, its body saying the agent stopped early and quoting
  its last message. agent-fix only pushes, once: the PR exists, that push is the only extra review, and a run whose
  only unpushed commit is the merge of `main` pushes nothing.

A rescued branch has not passed `make preflight`. Continue it with `@hyperscale-agent` on the PR.
A push to a branch with no PR starts no workflow: agent-review runs on `pull_request` only, and the
others run on pushes to `main` and `release/**` only (`scripts/agent_rescue_test.go` holds that).

## Seeing an agent at work

An agent run says so where it was started. agent-build posts one comment on the issue and
agent-fix posts one on the PR, each saying the agent is working, with a link to the run. When
the run stops, the same comment is edited to say what happened: the PR a build opened, the commit
a fix pushed, or that nothing was pushed, followed by the agent's own few lines on why. A run that
breaks says so in that comment too, with the reason its job gave (all its turns used, say) and how
to retry.

agent-fix also puts an `agent-fix` check on the PR's head. It stays in progress while the agent
works, then finishes as success when the run pushed, neutral when it pushed nothing, and failure
when the run broke. A run that pushed shows the finished check on the new head as well, since a
PR lists only its head's checks. The check is not required; it is there to be seen.

Every agent-build and agent-fix run also keeps what the agent did, as an artifact on the run
named `agent-transcript-<issue or PR>-<run id>`, for 14 days. It holds the action's transcript
and `agent-commands.json`: each shell command the agent ran with its exit code, then whether
`make preflight` and `make test` ran and how each last ended. This is how a red push is told apart
from a flake: an agent that never ran preflight shows it. The repo is public, so its artifacts
are too, so `scripts/agent-transcript.sh` keeps no tool output (each result is cut to its exit
code line) and masks secrets in what is left: the run's own tokens by value, with their base64
forms, then private keys, tokens, JWTs, Authorization headers and URL credentials by shape.

## PRs from a session

A Claude session that opens its PR with a person's token makes that person the author, and GitHub
never counts an author's approval, so the architect could only merge it by bypassing the rules.
Instead the session pushes its work to a `claude/` branch and dispatches `agent-open-pr.yml` with
the branch, a title and a body. The workflow opens the PR with the agents' App token, so the
author is `hyperscale-agent[bot]` and the architect's approval counts. A PR the App opens starts
agent-review and the required checks like any other.

Only a maintainer may dispatch it, only for a `claude/` branch that is already pushed and has no
open PR. The gate is `scripts/agent-trust.sh open`; a refusal fails the run with its reason.

## Who can start what

| Who | Can start |
| --- | --- |
| `maintainers` team | builds (`run:dev-gha`), fixes, PRs from a session's branch |
| `contributors` team | fixes, and their comments reach a building agent |
| anyone else | nothing; their comments never reach a model |

A team member also needs write access to the repo for a mention to run, because the action
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
   `pull_request_target`, and skip forks. No bot is admitted but the agents' own App and the
   reviewer App.
4. **The token is narrow.** The agents' App has no Workflows permission, so an agent cannot edit
   these files, and both main rulesets still need the architect's approval to merge. The
   reviewer's lenses hold read-only tokens; only the `agent-review` job, which runs no model,
   can write a review.

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
- adds the variable `REVIEWER_APP_CLIENT_ID` and the secret `REVIEWER_APP_PRIVATE_KEY` for the
  reviewer App, `hyperscale-reviewer`, which needs only Contents read and Pull requests write;
  without them agent-review posts as the agents' App, as above;
- creates the `contributors` team (`maintainers` exists), and creates the `run:dev-gha` label.

Every run draws on the subscription the token belongs to, and parallel runs share its limits. A
run that hits a limit fails and is retried by applying the label again.
