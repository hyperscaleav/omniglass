---
title: "The browser e2e tier and the docs screenshots ride the lab (#1267)"
date: 2026-10-29
refs: [1267, 1193, 1197]
---

The follow-up the lab epic filed: `make test-e2e`, `make docs-shots` and `make docs-shots-check` now
bring up a throwaway lab through `web/e2e/lib/lab.sh`, the same way the demo pipeline and the probe
already did, and `web/e2e/lib/stack.sh`, the last hand-built Postgres-and-server recipe, is deleted.
Each target builds first (`make`), since the lab is the binary and the image one build makes; the CI
jobs for both drop their Go toolchain step accordingly.

The e2e tier's lab is seeded with the `empty` scenario, so the CRUD round-trips start from a fresh
install as before, signed in as the lab's `dev` login. The screenshots needed a lab whose pages hold
still: `lab run` gains a fourth hidden script flag, `--no-node`, allowed only on a throwaway lab, which
seeds the dev fleet without the simulated fleet's wiring and enrolls and runs no node. That is exactly
the fleet the old capture seeded (`seed-dev --sim-fleet=false`, no node), and 22 of the 23 masked
baselines came back byte-identical. The capture hides the lab's banner with a style rule on its
`data-testid`, since the docs show the product an operator runs, not a lab.

The one baseline that moved is the audit page's, and for a reason worth knowing: the lab proves its
login works by signing in, and mints the script's token through the API with that session, so the
trail now carries a `login` row by `dev` and the token's `credential` create is by `dev` rather than
`system`. A second capture reproduced the new baseline exactly.
