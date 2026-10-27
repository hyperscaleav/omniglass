---
title: "omniglass lab brings up a collecting simulated fleet in Docker with one command (#1194)"
date: 2026-10-27
refs: [1194, 1193]
---

`omniglass lab run` (and `make lab`) brings up a working omniglass on Docker as the compose project
`omniglass-lab`: Postgres, the server, and the node `edge-hq` running `node run --simulate
--impairments`, seeded with the dev fleet through a one-off `lab seed`, with only the console
published on `127.0.0.1:8080` and `dev`/`dev` to sign in. `internal/lab` is a pure planner (options
to a project spec) and a runner over the Docker Engine API (the moby client, now a direct
dependency) that stamps compose's labels, resolves the daemon the way the docker CLI does, runs
preflight before creating anything, waits on readiness (the console, the login, every simulated
component running, every collected endpoint up but the seed's planted faults), tears down on Ctrl-C
keeping the volumes, and reaps a killed run. A dev binary runs only the `omniglass:dev` image whose
version label is its own build id; a release binary runs `ghcr.io/hyperscaleav/omniglass:v<version>`,
which `image.yml` now publishes on a release tag, and its boot smoke is a lab smoke. The guide's
topology and walkthrough list render from the planner and the walkthrough manifests
(`docs/src/generated/lab.json`, via `cmd/labgen`). `make test-lab` is the integration test against a
real daemon. The guide is [try omniglass in five minutes](/guides/lab/).
