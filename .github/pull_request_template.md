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

## Docs and review

- **Docs:** <!-- what shipped; architecture-of-record consistent, or a divergence note -->
- **Review:** <!-- reviewer findings and how addressed; security note if it touches authz/secrets/edge -->

## Decisions

- **Made (your veto window):** <!-- judgment calls that bound the design -->
- **Need from you:** <!-- open forks, or "none" -->

## Risk

<!-- Outward-facing? Invariant-changing? Reversible? -->

## Checklist

- [ ] Tests failed before, pass after; `make test` green run fresh
- [ ] Docs ship in this PR (or `no-docs` label with justification); status build-progress noted
- [ ] Operator-facing surface teaches its concept (learning-tool), if applicable
- [ ] `make gen` clean; no em dashes; no AI attribution
- [ ] Linked to a feature issue under an epic
