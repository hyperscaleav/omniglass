---
title: "The console installs with pnpm, one package manager for the repo (#2705)"
date: 2026-11-25
refs: [2705, 2170]
---

`web/` moved from npm to pnpm 10, as `docs/` already was. `pnpm import` carried `web/package-lock.json`'s resolved
versions into `web/pnpm-lock.yaml` unchanged, and `package.json`'s `packageManager` pins the exact pnpm version.
A dependency's install script runs only when `web/pnpm-workspace.yaml` names it under `allowBuilds` (esbuild, today),
which replaces npm 11's warning about scripts it was about to run anyway. The workflows, the Dockerfile's web stage,
the Makefile and the e2e scripts install with `pnpm install --frozen-lockfile`, and `scripts/web_pnpm_test.go` fails
on any npm install or script left for `web/`. Bun was weighed and set aside: vitest with jsdom, Solid's Vite
transform, Astro and Playwright all target Node, and pnpm brings the install-script hardening on its own.
