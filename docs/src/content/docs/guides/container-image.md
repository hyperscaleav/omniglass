---
title: "Container image"
description: "The published Omniglass container image: what it contains, where it ships, and how to run it."
---

Omniglass ships as a single container image. The operator console is compiled
into the binary (`-tags web` plus `go:embed`), so one image is the whole app:
every run mode (`server`, `node run`, `migrate`, `bootstrap`, `token`) is the same
binary with a different first argument. The final image is distroless and runs
as an unprivileged user; the binary is statically linked (CGO disabled), so the
image is a few tens of megabytes with no shell or libc.

The published tag is a multi-arch manifest covering `linux/amd64` and
`linux/arm64`; a host pulls the variant that matches its architecture
automatically. See [Platforms](#platforms) for how that is built.

## Where it ships

The `image` workflow builds and pushes to the GitHub Container Registry on every
push to `main` and every same-repo pull request. A fork PR builds the image but
does not push it (a fork run has no registry credential), so a fork branch never
gets a published tag:

```
ghcr.io/hyperscaleav/omniglass
```

Tags:

| Tag | When | Mutable? |
|-----|------|----------|
| `latest` | push to `main` | yes |
| `sha-<full sha>` | every build | no, pins an exact commit |
| `pr-<n>` | head of open PR #n | yes, moves on each push |

The sha tag carries the full 40-character commit sha (`type=sha,format=long`).
The preview pipeline pins a deploy to `sha-<full sha>` so a rollout follows the
exact build.

## Platforms

The image is published for `linux/amd64` and `linux/arm64` (Graviton, Ampere,
Apple silicon, Raspberry Pi class hardware). Both come from one cross-compile,
not native arm64 runners: because the binary is pure Go with CGO disabled, the
build stages run on the native CI host and `go build` retargets the arch with a
`GOARCH` switch, no emulation and no C toolchain. The arch-independent SPA
bundle is built once and embedded in both variants. The distroless base is
itself a multi-arch manifest, so the runtime layer resolves per target.

Cross-compiled artifacts get one emulated check before they ship: CI builds the
arm64 image, loads it under QEMU, and boots the binary (`--help`), so a broken
arm64 build fails the workflow before the manifest is pushed. There is no native
arm64 hardware in the loop; this boot test is the substitute.

## Configuration

The binary is configured by environment, BYO Postgres:

| Variable | Default | Purpose |
|----------|---------|---------|
| `OMNIGLASS_DSN` | local dev DSN | Postgres connection string the Storage Gateway dials |
| `DATABASE_URL` | (none) | conventional DSN fallback; `OMNIGLASS_DSN` wins when both are set |
| `OMNIGLASS_ADDR` | `:8080` | HTTP listen address |
| `OMNIGLASS_SECURE_COOKIES` | off | set `true` behind TLS to mark the session cookie `Secure` (https only) |
| `OMNIGLASS_NATS_ADDR` | `127.0.0.1:4222` | listen address (host:port) of the embedded NATS server |
| `OMNIGLASS_NATS_STORE_DIR` | temp dir | JetStream store directory |
| `OMNIGLASS_NATS_URL` | `nats://127.0.0.1:4222` | bus URL the node-claim exchange advertises to nodes |
| `OMNIGLASS_DATA_DIR` | `.omniglass` | local non-Postgres server state (notably the fallback secret key) |
| `OMNIGLASS_SETTINGS_FILE` | (none) | optional operator settings file (JSON or YAML), the layer between code defaults and the DB override |

## Running it

Apply the schema once, then run the server. (The server also applies pending
migrations itself on boot, so the separate `migrate` run is optional; it is
still useful to surface a schema problem before the server starts.)

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
  ghcr.io/hyperscaleav/omniglass:latest token alice --description "alice laptop"
```

## Building it locally

`make image` builds the image for your host architecture only:

```bash
make image                 # omniglass:dev, VERSION = short commit sha
make image TAG=v1 IMAGE=ghcr.io/hyperscaleav/omniglass
```

To reproduce the published multi-arch manifest, drive `buildx` directly (a
`docker-container` builder is required, since the default driver cannot emit a
manifest list). A manifest list cannot be `--load`ed into the local daemon, so
build with `--push` to a registry, or with `--platform linux/arm64 --load` to
inspect a single foreign arch:

```bash
docker buildx create --use --name omni            # once
docker buildx build --platform linux/amd64,linux/arm64 \
  -t ghcr.io/hyperscaleav/omniglass:dev --push .
```

## Where this fits

This image is the unit the Helm chart and the per-PR preview environments
deploy. See [Scaling and deployment](/architecture/scaling/) for the deployment
shape.
