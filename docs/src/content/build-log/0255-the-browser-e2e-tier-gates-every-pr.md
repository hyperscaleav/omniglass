---
title: "The browser e2e tier gates every PR"
date: 2026-08-14
refs: []
form: bullet
---

- **The browser e2e tier gates every PR.** A second job in `test.yml` runs `make test-e2e` on
  every pull request and on `main`: the already-containerized runner (`web/e2e/run.sh`) needs
  only Go, Node, and the runner's Docker daemon, so CI executes exactly what a laptop does. With
  the route smoke spec in the same tier, a change that breaks any console route in a real browser
  cannot merge; `make test` stays unchanged (the local loop keeps the explicit `make test-e2e`
  target, the Playwright image being too heavy to fold into the default gate) (#758, #761).
