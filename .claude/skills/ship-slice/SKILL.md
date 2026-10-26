---
name: ship-slice
description: Use when a vertical-slice PR is built and about to be proposed for merge. Runs the pre-ship validation (fresh make test, make gen drift check, em-dash and attribution scan, a reviewer pass, the docs-with-everything check, the demo every PR carries) and emits the standard ship-review report the architect approves from. Invoke at PR-ready, before asking for approval; re-run after addressing review findings.
---

# Ship a slice

A slice is shippable only after a fixed validation pass, presented as one **ship-review** the
architect can approve in seconds. This skill is the gate between "code green" and "request
merge." It does not merge; it produces the approval artifact.

## When

- A vertical-slice PR is built, committed, and pushed.
- Before asking the architect to merge.
- Again after addressing review findings.

## Pre-ship checklist (run it, do not assume)

Each is a gate; a red one blocks the ship.

1. **Tests, fresh.** `go test -count=1 ./...` (no cache). Never trust a cached pass or a
   subagent's "it's green": the integration and e2e tiers must actually execute, and `-short`
   or a cache hides the DB-backed behavior. Note *which* behaviors passed, not just `ok`.
2. **API-first drift.** `make gen`, then `git diff --exit-code` on the generated artifacts. A
   non-empty diff means the committed spec or clients drifted from the Go; commit the regen.
3. **House style.** No em dashes and no AI/assistant attribution in any changed file (grep the
   diff; scan the commit messages and the PR body).
4. **Docs with everything, status, and decisions.** The teaching docs ship in this PR, and the
   architecture-of-record stays consistent. Three status surfaces move with the code, never
   silently: the build-log entry is added as **its own file**,
   `docs/src/content/build-log/<YYYY-MM-DD>-<slug>.md` (frontmatter `title`, `date`, `refs`;
   the body is the entry's prose, the title becomes its heading); **each architecture page the slice
   advances has its status badge flipped to its new floor** (`Design` to `Partial` to `Built`); and
   if the build diverges from a page's present-tense design, the page carries an inline note **and**
   a [decision-log](/architecture/decisions/) entry (an ADR) lands in the same PR, as its own file
   via `/record-decision`. Never append to `build-log.md` or `decisions.md`: they hold no entries,
   and one file per entry is what keeps two PRs from conflicting (#1095). A page that
   gained a built capability but kept a `Design` badge is a red gate.
   Beyond status, **every operator surface the diff moves takes its guide in the same PR**. Grep
   the diff and walk the surfaces, mapping each to its home; a surface that moved while its guide
   did not is a red gate:
   - a new or changed **API route / Huma op** (`internal/api`, `api/openapi.*`) to the API
     reference (`architecture/api.md`);
   - a new or changed **CLI command** to the CLI guide (`guides/cli.md`), **including a generated
     command**: if `make gen` touched `internal/cli/api_gen.go`, the CLI guide is in scope (this
     is how the password and profile commands shipped undocumented);
   - a changed **console surface** (`web/src`) to the console guide (`guides/console.md`) plus the
     live screenshots (item 9).

   Mechanically, run the docs lint suite; it must be green:
   `go test ./internal/docslint/ ./internal/cli/ -run 'TestVocabulary|TestDecisionsFormat|TestBuildLogFormat|TestLogsStayFragments|TestDocsOnlyNameRealCommands' -count=1`.
   Then sweep the architecture pages the slice touches for `Still Design`, `later slice`,
   `not yet`, `soon`, and `deferred` fences and confirm none of them describes what the slice
   just built; a fence still hedging a shipped capability is a red gate. And if the slice
   renames or retires an identifier, the `internal/docslint` denylist entry and the docs sweep
   land in the same PR (the rename ripple, like `/storage-schema-change`'s gateway ripple).
5. **The neighbor check.** A slice that flips a behavior greps the docs for the claim it
   falsified, not just its own page: search the whole docs tree for the key nouns of the
   changed behavior and list the hits in the ship-review's Docs line. The two shipped
   lessons: the alarms-actions cycle-safety premise died on a health slice, and the vendors
   guide died on the products slice; in both, a neighboring page kept teaching the falsified
   claim because only the slice's own page was updated.
6. **Dev seed.** If the slice adds a new operator entity (a Storage Gateway create plus a surface
   for it), it adds example rows for that entity to the dev seed (`internal/devseed/fixtures.yaml`),
   so `make dev` comes up populated and nobody hand-creates locations, users, or grants to exercise
   the feature. The seed stays idempotent: a re-run of `make dev` changes nothing. `n/a` when the
   slice adds no new entity. This is the dev-only example fleet, never the boot seed
   (`internal/seed`, ship-with reference data that also runs in production).
7. **Review.** A reviewer pass over the diff (`/adversarial-review` for loop-built slices and
   any diff wanting the anti-pattern sweep; `code-review` or `cavecrew-reviewer` otherwise),
   findings addressed. Add a `security-review` lens if the slice touches authz, secrets, the
   edge, or an invariant. Verify behavior to the outcome line, not just call sites.
8. **Scope honesty.** Every thin cut is documented; every deferral is a filed issue.
9. **Evidence in the PR.** Paste the *actual* fresh test output (the tail of `make test`, plus
   web tests if touched) into the PR body, not a "they pass" claim. For any operator-facing
   change, include **screenshots driven live** (e.g. against `make dev`). Capture them headless
   with `node web/e2e/shot.mjs <url> <out.png> [--token <og-token>] [--click <sel>]...
   [--select "<sel>||<value>"]...` (bundled chromium, writes to the host FS, drives interactive
   states like an open menu or a chosen option). **Decide how to host them with a check,
   not a guess: run `gh image check-token` first.**

   If it passes, host them with the `gh image` extension (`node web/e2e/shot.mjs ... &&
   gh image <out.png>` prints the markdown to paste). It works by extracting a logged-in
   browser's session cookie, which exists only on a workstation you browse GitHub from.

   **If the check fails, embed the clean docs render the slice already committed**, by
   **immutable commit SHA**
   (`https://raw.githubusercontent.com/<owner>/<repo>/<sha>/docs/public/screenshots/<id>.png`),
   so the link survives the branch being deleted on squash-merge. This costs the repo
   nothing: a UI slice re-runs `make docs-shots` and commits both sets anyway, and a re-run
   rewrites the same filenames rather than adding new ones. This is the normal path on any
   headless or remote machine: an unattended run, a worker VM, a container, CI. Do not treat
   a failed check as a blocker and do not skip the screenshots.

   Only where the surface has **no docs shot id** (an interactive state the docs do not
   capture), commit under `.github/screenshots/` and embed by SHA the same way. Every file
   there is a new path that stays in git history permanently, so keep that path to what the
   docs renders genuinely do not cover.

   Do not set `GH_SESSION_TOKEN` on an unattended machine to work around this. A
   `user_session` cookie is a full-account credential rather than a scoped token, it rotates
   and expires, so it fails silently mid-run, and committed screenshots are versioned and
   reviewable where uploaded attachments are neither.

   **Docs screenshots are a generated resource, in two renders.** The images embedded *on
   the docs pages* are declared in each page's `screenshots` frontmatter and captured by
   `make docs-shots` from the real console, never hand-added; each capture writes the CLEAN
   shot (`docs/public/screenshots/`, every region live) and its masked BASELINE twin
   (`docs/screenshots/baseline/`), and `make docs-shots-check` recaptures and diffs the
   baselines at zero tolerance, the visual sibling of the `make gen` drift check, so a stale
   screenshot cannot merge. A slice that changes an operator-facing surface **re-runs
   `make docs-shots` and commits both sets**. Adding a new shot is a frontmatter entry plus a
   `::screenshot{#id}` directive in the prose, not a code change. **PR bodies, comments, and
   issues embed the clean renders, never the baselines** (a masked strip teaches nothing);
   the baselines exist only for the gate.
10. **Demo, for every PR.** Every PR demos its feature live, as a recording on the PR the
   architect watches rather than a transcript (#1132, the [demos](/contributing/demos/) page). A
   demo is one concept with two recorders; decide which from the diff, not from intent:
   - **CLI or API behavior** (a new or changed route, command, flag or answer) takes a **terminal
     demo**: a VHS tape, `scripts/demo/<id>.tape`, that drives it through the CLI.
   - **An operator flow** (a form, a multi-step path, a drawer action, a route, or the steps
     between them: anything an operator now does in a different order or with different input)
     takes a **walkthrough** (ruled on #884). A visual-only change (a restyle, a new column, a moved
     badge) keeps screenshots alone.
   - Both, when the PR has both.
   - **n/a only with a reason**, in one clause: test-only, docs-only, a refactor with no behavior
     an operator or a client can observe.

   **The PR body's demo tag is required, and the verdict cannot be `ready` without it** (#1180): a
   line of its own in the Demo section naming the demo or demos the PR is about, `demo: <id>
   [<id>...]`, or `demo: none (<reason>)`. `run:demo` records exactly the tagged demos (plus any
   tape or walkthrough the PR adds or changes), and nothing else, so the tag is the selection. A PR
   with no tag, `none` with no reason, `none` for a diff that changes CLI, API or console behavior,
   or a tag naming a demo the tree lacks (`demo-tag.yml` fails it; check locally with
   `PR_BODY="$(cat body.md)" node web/e2e/walkthrough-pr.mjs tag`) is a red gate, exactly like a UI
   PR with no screenshots. Refuse it; build and tag the demo.

   How:
   - **Write it.** A tape opens with its title and description as a comment block, then types the
     real commands an operator or integrator would (narrated with typed `# ...` lines), waiting on
     each (`Enter`, then `Wait`). Address by name or dotted address, never a uuid (the rules refuse
     one; look an id up by name inside the tape). Show only what holds still (pick stable fields
     with `jq`), and hide the waits in a `Hide` block with `demo_until` and `demo_hush`, which erase
     themselves. The rules and an annotated example are the header of `web/e2e/tapelib.mjs`. A
     walkthrough is `web/e2e/walkthroughs/<id>.yaml` (steps of `goto`, `click`, `type`, `select`,
     `press`, `wait`, a caption each; the format is the header of `web/e2e/walkthroughlib.mjs`),
     ending each action on a `wait` for its visible result. `make test-web` validates every tape
     and script in the tree.
   - **Prove it locally.** `make demo ID=<id>` (Docker only) records a demo of either kind: a tape
     to `scripts/demo/out/<id>/` (the GIF, the MP4, `demo.json`), a walkthrough to
     `web/e2e/walkthroughs/out/<id>/` (the mp4, `poster.png`, a still per step). Look at it before
     asking CI: read a walkthrough's stills with `/assess-ui` (its walkthrough rubric), and a
     terminal demo's frames (ffmpeg pulls them from the MP4); the agent cannot watch video. A demo
     is a test: a failing command, a wait that runs out or a result that never appears fails it.
   - **Tag it.** The PR body's Demo section carries the tag on a line of its own (`demo: <id>`),
     the demo this PR is about; a PR touching a recorder or the stack tags one demo of each kind it
     could break, since nothing fans out on its own.
   - **Record it on the PR.** Resolve any merge conflicts (GitHub runs no `pull_request` workflow
     on a conflicted PR), push, then apply the `run:demo` label. CI records the branch tip: exactly
     the tagged demos plus any tape or walkthrough script the PR adds or changes, each on a runner
     of its own, in parallel. It commits each demo's files to the PR's evidence ref
     (`refs/walkthroughs/pr-<n>`, never the PR branch, so there is nothing to pull and the head's
     checks are untouched), upserts **one comment listing every demo** (the full record: a terminal
     demo's GIF with its MP4 linked, a walkthrough's scrubbable video, preview, poster and stills),
     and **writes each demo's clip into the PR body** under the tag, between its own markers, so
     the body plays its video. A failed demo fails its job and is named in the comment and the clip
     (a stack that never came up says so instead); fix it and re-run.
   - **Leave the clip alone.** Edit the body around the marked block freely; a re-run replaces only
     the block. Do not paste the clip markers anywhere else in the body.

11. **Audit coverage.** Every privileged **mutation** and every **auth event** the slice adds
   writes an `audit_log` row: a fleet or IAM mutation through `writeAuditRes` **in the same
   transaction** as the change (a committed change without its audit row is a red gate), and an
   auth event (login, logout, a denied sign-in) through `WriteAuthEvent` on the read/no-tx path.
   Grep the diff for new gateway writes and new handlers; each names an actor (and, under
   impersonation, carries the real actor via the request context). A new privileged write with no
   audit row, or an auth event that is silently unlogged, is a red gate. Reads are not audited
   (except secret decrypts, which always are).
12. **Generate first.** Scan the diff for hand-written artifacts that restate a fact the code
   already knows: a table of routes, columns, env vars, or seeded rows; a copied schema; an
   asset a generator could emit. Each is either replaced by a generated render in this PR or
   has a filed issue named in the ship-review's Docs line. Hand-written narrative is fine;
   hand-written facts are the drift class the 2026-07-30 audit proved out.

## The ship-review (emit this, in chat and as the PR body)

```
SHIP REVIEW - <type>: <slice>   (PR #N, closes #M)

Outcome:   <one user-observable line>
Verdict:   ready | ready-pending-your-call

Scope
  In:        <bullets>
  Thin cut:  <deliberate simplifications>
  Deferred:  <#issue refs>

Surfaces:  API <ops> / CLI <commands, generated> / UI <view, live | stub>
Proof (ran fresh)
  make test: green, N packages   <pasted output in the PR body>
  Behaviors: <the RED->GREEN and the load-bearing assertions>
  Tiers:     unit X / integration Y / e2e Z
  make gen:  clean
Visual:    <screenshots in the PR for any UI surface | n/a>
Demo:      <demo: <id> [<id>...], the clip in the body and the run:demo comment link | demo: none (why: test-only, docs-only)>
Dev seed:  <new-entity example rows added to internal/devseed | n/a>

Docs:      <what shipped; arch-of-record consistent | divergence note; lint: green;
            neighbor-check hits and how resolved | none>
Status:    <pages advanced (page: Design->Partial); build-log entry file; ADR-#### file if diverged;
            fences swept: <pages | n/a>>
Review:    <reviewer findings + how addressed; security: n/a | note>

Decisions I made (your veto window): <judgment calls that bound the design>
Decisions I need from you:           <open forks | none>

Diff:      N files, ~M LOC   <PR link>
Risk:      <outward-facing? invariant-changing? reversible?>
```

`ready` is not available while a gate is red: a PR body with no demo tag, `demo: none` with no
reason, or `none` for a diff that changes CLI, API or console behavior, ships as neither `ready` nor
`ready-pending-your-call` until the tag is there and its clip has landed in the body.

The two lines the architect reads first are **Decisions I need from you** and **Risk**.
Approval means squash-merge; a redirect adjusts the slice.

## When the PR opens

Move each issue the PR closes to In Review on the project board:

```bash
scripts/board.sh set <M> "In Review"
```

Until the board has an "In Review" status the script lands it on In Progress with one warning;
that is expected, not a failure. Never set Done by hand: merging closes the issue and GitHub's
built-in workflow moves it. The script never touches Priority, which is the architect's
([the board](../../../docs/src/content/docs/contributing/labels.md#the-board)). Where it cannot
run (no `gh`, or a token without the `project` scope; it says which), say so in the ship-review.

## After approval

Squash-merge (the conventional-commit PR title drives the release), remove the worktree, then
`logwork`.
