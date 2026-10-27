---
title: "make only builds, and the console loop proxies to a lab (#1196)"
date: 2026-10-27
refs: [1196, 1193]
---

The bare `make` needs only Docker: from one Dockerfile it builds `omniglass:dev` and exports this
machine's binary to `bin/omniglass` (the `hostbin` stage), both stamped with one build id, with
BuildKit cache mounts. `make dev` builds that and runs `npm install` in `web/`, and starts nothing.
`make dev-sim`, `make up`, `make down`, `docker-compose.yml`, `SIM_PORT_OFFSET` and its test retire;
`seed-dev --sim-port-offset` defaults to 0, the lab's real ports. The Vite dev server proxies `/api`
to `http://127.0.0.1:8080`, moved by `OMNIGLASS_LAB_URL`, so the console's fast loop is a lab in one
terminal and `npm run dev` in another. The docs that named the retired targets point at the
[lab](/guides/lab/#the-console-fast-loop).
