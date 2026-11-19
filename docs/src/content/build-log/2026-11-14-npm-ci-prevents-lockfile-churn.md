---
title: "npm ci prevents lockfile churn (#1278)"
date: 2026-11-14
refs: [1278]
---

`make test`, `make gen`, `make gen-web`, `make web`, and `make dev` now use `npm ci` (clean install) instead of `npm install` so the lockfile is never rewritten. `npm ci` respects the pinned versions in `web/package-lock.json` and fails on mismatch, making version drift explicit instead of silent. The web console's `package.json` now declares a minimum npm version (`engines.npm` and `packageManager`), so a contributor running an unsupported npm sees a clear error immediately. A gate check ensures the lockfile stays clean after all build targets, green under both npm 10 and npm 11, so no unrelated churn lands on `main`.
