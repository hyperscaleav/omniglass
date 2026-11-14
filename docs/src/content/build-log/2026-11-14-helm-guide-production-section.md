---
title: "The Helm guide gains a production section (#1296)"
date: 2026-11-14
refs: [1296, 1287, 1291, 1286, 1288, 1289, 1290, 1293, 1294, 1295, 1231, 1230]
---

The Helm guide's `## Production` section frames what each member of epic #1287 documents: profile choice, demo-only Postgres warning, managed Postgres via `externalDsn`, durable state, upgrades, sizing, hardening, origin TLS, and trusted proxies. This PR ships the frame and the parts already true on main: the advisory lock and schema guard from the boot sequence, `externalDsn` recommended for production, the single-replica thin cut with a pointer to scaling.md, and the demo-only Postgres warning. Member PRs replace their placeholders; the Helm guide status badge remains unchanged, as a frame does not move past Design.

The chart's `Server environment` section is rewritten to describe what it does today (sets `OMNIGLASS_DSN` and `OMNIGLASS_SECURE_COOKIES` only) instead of promising `extraEnv` support; #1230 names when that capability lands. The deployment guide now points at the Production section for production-specific guidance.
