---
title: "Container image"
description: "The published Omniglass container image: what it contains, where it ships, and how to run it."
---

Omniglass ships as a single container image. The operator console is compiled
into the binary (`-tags web` plus `go:embed`), so one image is the whole app:
every run mode (`server`, `node`, `migrate`, `bootstrap`, `token`) is the same
binary with a different first argument. The final image is distroless and runs
as an unprivileged user; the binary is statically linked (CGO disabled), so the
image is a few tens of megabytes with no shell or libc.

## Where it ships

The `image` workflow builds and pushes to the GitHub Container Registry on every
push to `main` and every pull request:

```
ghcr.io/hyperscaleav/omniglass
```

Tags:

| Tag | When | Mutable? |
|-----|------|----------|
| `latest` | push to `main` | yes |
| `sha-<short>` | every build | no, pins an exact commit |
| `pr-<n>` | head of open PR #n | yes, moves on each push |

The preview pipeline pins a deploy to `sha-<short>` so a rollout follows the
exact build.

## Configuration

The binary is configured by environment, BYO Postgres:

| Variable | Default | Purpose |
|----------|---------|---------|
| `OMNIGLASS_DSN` (or `DATABASE_URL`) | local dev DSN | Postgres connection string the Storage Gateway dials |
| `OMNIGLASS_ADDR` | `:8080` | HTTP listen address |

## Running it

Apply the schema once, then run the server:

```bash
DSN='postgres://user:pass@host:5432/omniglass?sslmode=require'

docker run --rm -e OMNIGLASS_DSN="$DSN" \
  ghcr.io/hyperscaleav/omniglass:latest migrate

docker run -d -p 8080:8080 -e OMNIGLASS_DSN="$DSN" \
  ghcr.io/hyperscaleav/omniglass:latest server
```

The console is then at `http://localhost:8080/web`, and `GET /api/v1/healthz`
reports `status: ok` once the database leg passes. To sign in, mint an owner and
a token:

```bash
docker run --rm -e OMNIGLASS_DSN="$DSN" \
  ghcr.io/hyperscaleav/omniglass:latest bootstrap alice
docker run --rm -e OMNIGLASS_DSN="$DSN" \
  ghcr.io/hyperscaleav/omniglass:latest token alice
```

## Where this fits

This image is the unit the Helm chart and the per-PR preview environments
deploy. See [Scaling and deployment](/architecture/scaling/) for the deployment
shape.
