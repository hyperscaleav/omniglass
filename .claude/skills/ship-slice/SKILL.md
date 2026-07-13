---
name: ship-slice
description: Use when a vertical-slice PR is built and about to be proposed for merge. Runs the pre-ship validation (fresh make test, make gen drift check, em-dash and attribution scan, a reviewer pass, the docs-with-everything check) and emits the standard ship-review report the architect approves from. Invoke at PR-ready, before asking for approval; re-run after addressing review findings.
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
   silently: the `status.mdx` build-progress entry is added; **each architecture page the slice
   advances has its status badge flipped to its new floor** (`Design` to `Partial` to `Built`); and
   if the build diverges from a page's present-tense design, the page carries an inline note **and**
   a [decision-log](/architecture/decisions/) entry (an ADR) lands in the same PR. A page that
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
     live screenshots (item 8).
5. **Dev seed.** If the slice adds a new operator entity (a Storage Gateway create plus a surface
   for it), it adds example rows for that entity to the dev seed (`internal/devseed/fixtures.yaml`),
   so `make dev` comes up populated and nobody hand-creates locations, users, or grants to exercise
   the feature. The seed stays idempotent: a re-run of `make dev` changes nothing. `n/a` when the
   slice adds no new entity. This is the dev-only example estate, never the boot seed
   (`internal/seed`, ship-with reference data that also runs in production).
6. **Review.** A reviewer pass over the diff (`code-review` or `cavecrew-reviewer`), findings
   addressed. Add a `security-review` lens if the slice touches authz, secrets, the edge, or an
   invariant. Verify behavior to the outcome line, not just call sites.
7. **Scope honesty.** Every thin cut is documented; every deferral is a filed issue.
8. **Evidence in the PR.** Paste the *actual* fresh test output (the tail of `make test`, plus
   web tests if touched) into the PR body, not a "they pass" claim. For any operator-facing
   change, include **screenshots driven live** (e.g. against `make dev`). Capture them headless
   with `node web/e2e/shot.mjs <url> <out.png> [--token <og-token>] [--click <sel>]...
   [--select "<sel>||<value>"]...` (bundled chromium, writes to the host FS, drives interactive
   states like an open menu or a chosen option). Host them with the `gh image` extension
   (`node web/e2e/shot.mjs ... && gh image <out.png>` prints the markdown to paste). `gh image`
   **auto-extracts the browser session cookie by default** (no `GH_SESSION_TOKEN`, no setup);
   `gh image check-token` verifies it is valid. `GH_SESSION_TOKEN` / `--token` are optional
   overrides for a machine with no logged-in browser. Otherwise commit them
   under `.github/screenshots/` and embed by **immutable commit SHA**
   (`https://raw.githubusercontent.com/<owner>/<repo>/<sha>/.github/screenshots/...`), so the
   link survives the branch being deleted on squash-merge.

   **Docs screenshots are a generated resource.** The images embedded *on the docs pages*
   (not the PR body) are declared in each page's `screenshots` frontmatter and captured by
   `make docs-shots` from the real console, never hand-added. A slice that changes an
   operator-facing surface **re-runs `make docs-shots` and commits the refreshed PNGs**;
   `make docs-shots-check` recaptures and fails if a shot drifts beyond a small tolerance
   (the dev seed's random UUIDs move a fraction of a percent; a real change moves far more),
   the visual sibling of the `make gen` drift check, so a stale screenshot cannot merge.
   Adding a new one is a frontmatter entry plus a `::screenshot{#id}` directive in the prose,
   not a code change.
8. **Audit coverage.** Every privileged **mutation** and every **auth event** the slice adds
   writes an `audit_log` row: an estate or IAM mutation through `writeAuditRes` **in the same
   transaction** as the change (a committed change without its audit row is a red gate), and an
   auth event (login, logout, a denied sign-in) through `WriteAuthEvent` on the read/no-tx path.
   Grep the diff for new gateway writes and new handlers; each names an actor (and, under
   impersonation, carries the real actor via the request context). A new privileged write with no
   audit row, or an auth event that is silently unlogged, is a red gate. Reads are not audited
   (except secret decrypts, which always are).

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
Dev seed:  <new-entity example rows added to internal/devseed | n/a>

Docs:      <what shipped; arch-of-record consistent | divergence note>
Status:    <pages advanced (page: Design->Partial); status.mdx entry; ADR-#### if diverged>
Review:    <reviewer findings + how addressed; security: n/a | note>

Decisions I made (your veto window): <judgment calls that bound the design>
Decisions I need from you:           <open forks | none>

Diff:      N files, ~M LOC   <PR link>
Risk:      <outward-facing? invariant-changing? reversible?>
```

The two lines the architect reads first are **Decisions I need from you** and **Risk**.
Approval means squash-merge; a redirect adjusts the slice.

## After approval

Squash-merge (the conventional-commit PR title drives the release), remove the worktree, then
`logwork`.
