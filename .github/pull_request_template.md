<!--
PR title MUST be a conventional commit: "<type>: <description>"
type = feat | fix | docs | ci | chore | refactor | test | perf
The title becomes the squash commit on main and drives the release.
This body is the ship-review the architect approves from (see the /ship-slice skill).
-->

## Outcome

<!-- One user-observable line: what this slice delivers. -->

Closes #

## Scope

- **In:**
- **Thin cut:** <!-- deliberate simplifications this slice -->
- **Deferred:** <!-- #issue refs for work moved to later slices -->

## Proof (ran fresh)

<!-- The behaviors the tests prove, not just "they pass". -->

- `make test`: green, _N_ packages (no cache)
- Behaviors:
- Tiers: unit _x_ / integration _y_ / e2e _z_
- `make gen`: clean (spec and code in sync)

<!-- PASTE the actual fresh test output (the tail of `make test`, and web tests
     if touched). Evidence, not a claim. -->

```
$ make test
... (paste) ...
```

## Visual confirmation

<!-- REQUIRED for any operator-facing (UI) change: screenshots of the surface,
     driven live (e.g. against `make dev`). Upload via the gh-image extension
     (`gh image <path>` auto-extracts the browser session cookie; `gh image
     check-token` verifies it); otherwise commit them under
     .github/screenshots/ and embed by immutable commit SHA
     (https://raw.githubusercontent.com/<owner>/<repo>/<sha>/.github/screenshots/...).
     Write "n/a (no UI surface)" for non-UI slices. -->

![](...)

## Demo

<!-- REQUIRED: every PR demos its feature live, as a recording on the PR.
     - CLI or API behavior: a terminal demo, a VHS tape at scripts/demo/<id>.tape.
     - An operator flow in the console (a form, a multi-step path, a navigation
       change): a walkthrough, a script at web/e2e/walkthroughs/<id>.yaml.
     - Both, when the PR has both.
     Prove it locally with `make demo ID=<id>`, then apply the `run:demo` label: CI
     records it and posts one comment listing every demo (a terminal demo's GIF
     inline with its MP4 linked; a walkthrough's scrubbable video, preview, poster
     and stills). Link that comment here. "n/a" only with a reason, e.g.
     "n/a (test-only change)". /ship-slice refuses a PR with no Demo line. -->

- **Demo:**

## Docs and review

- **Docs:** <!-- what shipped; architecture-of-record consistent, or a divergence note -->
- **Review:** <!-- reviewer findings and how addressed; security note if it touches authz/secrets/edge -->

## Decisions

- **Made (your veto window):** <!-- judgment calls that bound the design -->
- **Need from you:** <!-- open forks, or "none" -->

## Risk

<!-- Outward-facing? Invariant-changing? Reversible? -->

## Checklist

- [ ] Tests failed before, pass after; `make test` green run fresh, **output pasted above**
- [ ] **Screenshots** of any operator-facing change, driven live (or "n/a")
- [ ] **Demo** comment from `run:demo`: a terminal demo for CLI or API behavior, a walkthrough for a console flow (or "n/a" with a reason)
- [ ] Docs ship in this PR (or `no-docs` label with justification); status build-progress noted
- [ ] Operator-facing surface teaches its concept (learning-tool), if applicable
- [ ] `make gen` clean; no em dashes; no AI attribution
- [ ] Linked to a feature issue under an epic
