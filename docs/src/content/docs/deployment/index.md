---
title: Deployment
description: "Installing Omniglass and running it in production: the binary, the container image, Helm, and operating it."
sidebar:
  label: Overview
  order: 0
---

**For:** the people who install Omniglass and keep it running: the platform or IT team that owns
the server, its PostgreSQL database and the outposts that collect from the fleet.

Omniglass is a single self-contained Go binary with the operator console compiled in, plus a
bring-your-own PostgreSQL database. A deployment is that one binary in one of its run modes (`server`,
`outpost run`, `migrate`) and a database; how it scales is
[scaling and deployment](/architecture/scaling/). To try it first with nothing but Docker, [the lab](/developer/lab/)
brings up a working omniglass with a simulated fleet; it is for learning, not production.

**In this section:** pick the path that fits where you are running it.

- **[Install](/deployment/install/)** downloads a prebuilt binary for your OS and architecture.
- **[Container image](/deployment/container-image/)** is the published distroless image, its tags,
  its configuration and the `migrate` then `server` flow.
- **[Deploying with Helm](/deployment/helm/)** is the chart that serves production (BYO Postgres) and
  disposable previews (bundled Postgres) from one set of values.
- **[Running in production](/deployment/production/)** covers sizing PostgreSQL, backups, upgrades and
  what is still to come.
- **[Running a simulator](/deployment/simulator/)** serves a simulated fleet to an outpost.
- **[PR preview environments](/developer/pr-previews/)** give a pull request a live copy of the console.

Once a server is up, managing accounts and access moves to the [Admin](/admin/) section.
