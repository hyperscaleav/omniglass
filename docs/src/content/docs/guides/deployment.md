---
title: Deployment
description: "Standing an Omniglass platform up: the single binary, the container image, the Helm chart, and per-PR preview environments."
---

This is the how-to for **standing the platform up**, a different job from
[operating the estate](/guides/operator/) or [administering it](/guides/admin/). Omniglass is a
single self-contained Go binary with the operator console compiled in and a BYO PostgreSQL
database, so a deployment is that one binary in one of its run modes (`server`, `node`, `migrate`)
plus a database. How it scales and the deploy model behind these pages is
[scaling and deployment](/architecture/scaling/).

Pick the path that fits where you are running it:

- **[Install](/guides/install/)** downloads a prebuilt binary for your OS and architecture, the
  simplest way to run the server or drive it from the command line.
- **[Container image](/guides/container-image/)** is the published distroless image: what it
  contains, where it ships, and how to run it with the `migrate` then `server` flow.
- **[Deploying with Helm](/guides/helm/)** is the chart that serves both production (BYO Postgres)
  and disposable previews (bundled Postgres) from one set of values.
- **[PR preview environments](/guides/pr-previews/)** stand up a live, isolated copy of the console
  for any pull request, so a change is reviewed against a running system.

Once a server is up, the first owner is minted with the trusted direct-database lane
(`omniglass bootstrap`), covered in [the CLI guide](/guides/cli/#authentication); from there,
managing accounts and access moves to the [admin guide](/guides/admin/).
