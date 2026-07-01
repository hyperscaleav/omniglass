---
title: "Deploying with Helm"
description: "The Omniglass Helm chart: install, values, and the bundled-Postgres preview mode versus BYO-Postgres production."
---

The chart at `deploy/chart/` is the deploy primitive: one chart serves both
disposable per-PR previews and production. It runs the
[container image](/guides/container-image/); the per-PR preview environments (a
later slice) are its first consumer, and production reuses it unchanged by
flipping two values.

## Two modes

| Mode | `postgres.enabled` | `bootstrap.enabled` | Database |
|------|--------------------|---------------------|----------|
| Preview (default) | `true` | `true` | bundled, ephemeral (emptyDir) |
| Production | `false` | `false` | external, via `externalDsn` |

The bundled Postgres is deliberately ephemeral: its data is an `emptyDir` and
does not survive a pod restart, which is what you want for a throwaway preview.
Production points at a real, managed Postgres.

## Install

Preview (bundled Postgres, an auto-created owner):

```bash
helm install og-pr-42 deploy/chart -n og-pr-42 --create-namespace \
  --set image.tag=sha-<short>
```

Production (BYO Postgres, no auto-owner):

```bash
helm install omniglass deploy/chart -n omniglass --create-namespace \
  --set postgres.enabled=false \
  --set externalDsn='postgres://user:pass@db:5432/omniglass?sslmode=require' \
  --set bootstrap.enabled=false \
  --set image.tag=<release>
```

## How it boots

The server Deployment gates startup with init containers rather than Helm
pre-install hooks, because a bundled Postgres in the same release is not up
during the pre-install phase:

1. `wait-for-db` (only when `postgres.enabled`) blocks until Postgres answers.
2. `migrate` applies the schema (idempotent: dbmate runs each migration once).
3. `bootstrap` (only when `bootstrap.enabled`) creates the first owner and mints
   its bearer token, printed once to this container's logs. Idempotent: a
   restart mints no second token.

Both `migrate` and `bootstrap` are safe to re-run on every rollout. At
`server.replicas` > 1 in production, run `migrate` as a separate one-shot Job
instead, to avoid concurrent migrators.

## Values

| Key | Default | Purpose |
|-----|---------|---------|
| `image.repository` | `ghcr.io/hyperscaleav/omniglass` | image to run |
| `image.tag` | `latest` | pin to `sha-<short>` for an exact build |
| `image.pullPolicy` | `IfNotPresent` | |
| `server.replicas` | `1` | server pod count |
| `server.resources` | small requests/limits | |
| `service.port` | `8080` | ClusterIP service port |
| `bootstrap.enabled` | `true` | auto-create an owner (previews) |
| `bootstrap.username` | `preview` | owner username |
| `bootstrap.email` / `bootstrap.displayName` | empty / `Preview Owner` | optional owner fields |
| `postgres.enabled` | `true` | bundle an ephemeral Postgres |
| `postgres.image` / `user` / `password` / `database` | `postgres:18` / `omniglass` x3 | bundled Postgres settings |
| `externalDsn` | empty | required when `postgres.enabled` is false |

## Getting in

There is no ingress in this chart (the per-PR preview pipeline adds the
Cloudflare exposure). Reach a fresh install by port-forward:

```bash
kubectl -n og-pr-42 port-forward svc/og-pr-42 8080:8080
# open http://localhost:8080/web
kubectl -n og-pr-42 logs deploy/og-pr-42 -c bootstrap   # the one-time token
```
