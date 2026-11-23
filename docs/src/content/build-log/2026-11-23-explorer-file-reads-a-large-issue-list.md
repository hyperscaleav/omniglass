---
title: "explorer.mjs file reads a large issue list (#1777)"
date: 2026-11-23
refs: [1777, 961]
---

`explorer.mjs file` died with `spawnSync gh ENOBUFS` once the open-issue listing (bodies, limit
1000) passed Node's 1 MiB default `maxBuffer` for `execFileSync`, so the nightly synthetic user
could not file or plan anything. The `run` helper now lives in `web/e2e/explorerrun.mjs` with a
256 MiB buffer. Regression test: "returns child output larger than Node's 1 MiB default" in
`web/src/explorer.test.ts`.
