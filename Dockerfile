# Multistage build for the single Omniglass binary with the operator console
# embedded. Stage `web` builds the Vite SPA into internal/webui/dist; stage
# `build` compiles the Go binary with -tags web so go:embed picks the SPA up;
# the final stage is a distroless static image running the binary as nonroot.
# One image is the whole app: server, node, migrate, bootstrap, and token modes.
#
# Multi-arch: the two builder stages pin to $BUILDPLATFORM so they always run
# natively on the host (no QEMU), and the Go stage cross-compiles to the target
# via $TARGETOS/$TARGETARCH. The SPA bundle is arch-independent, so the web
# stage builds it once and both target images embed the same dist. CGO is off,
# so the cross-compile is a plain GOARCH switch with no C toolchain. The final
# distroless/static base is itself a multi-arch manifest, resolved per target.
#
# The bare `make` builds this image AND exports the host's own binary from the
# `hostbin` stage (#1196), so a machine with only Docker gets both, from one
# tree, stamped with one build id (VERSION): `omniglass lab run` checks the
# image's org.opencontainers.image.version label against the binary's build id
# before it runs the image. BuildKit cache mounts keep the npm and Go caches
# across builds, so a Go-only change rebuilds in seconds.

# ---- stage 1: build the console (Vite -> internal/webui/dist) --------------
# Arch-independent JS bundle: build on the native host regardless of target.
FROM --platform=$BUILDPLATFORM node:22-bookworm-slim AS web
WORKDIR /src/web
# Install against the lockfile first so the deps layer caches across source-only
# changes.
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
# Then the sources. vite.config.ts writes the build to ../internal/webui/dist,
# i.e. /src/internal/webui/dist, which the Go stage copies into its context.
COPY web/ ./
RUN npm run build

# ---- stage 2: compile the binary with the console embedded -----------------
# Runs natively on the host ($BUILDPLATFORM); cross-compiles to the target arch.
FROM --platform=$BUILDPLATFORM golang:1.25.3-bookworm AS build
WORKDIR /src
# Module graph first for a cached download layer.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
# Full source, then overlay the freshly built SPA from the web stage so
# `//go:embed all:dist` (under -tags web) has real content to embed.
COPY . .
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
# version is stamped into main.version; CI passes the commit SHA (or a tag),
# make passes the build id it also labels the image with.
ARG VERSION=dev
# TARGETOS/TARGETARCH are provided by buildx per target in the platform list.
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -tags web \
      -ldflags "-X main.version=${VERSION} -s -w" \
      -o /out/omniglass ./cmd/omniglass && \
    mkdir -p /out/data

# ---- the host binary, exported by the bare `make` ----------------------------
# The same tree and build id, compiled for the machine running make (HOSTOS,
# HOSTARCH), so a contributor needs no Go toolchain to get ./bin/omniglass:
#   docker build --target hostbin --output type=local,dest=bin ...
FROM build AS hostbuild
ARG VERSION=dev
ARG HOSTOS=linux
ARG HOSTARCH=amd64
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    ext=""; [ "${HOSTOS}" = windows ] && ext=".exe"; \
    CGO_ENABLED=0 GOOS=${HOSTOS} GOARCH=${HOSTARCH} go build -trimpath -tags web \
      -ldflags "-X main.version=${VERSION} -s -w" \
      -o /out/host/omniglass${ext} ./cmd/omniglass

FROM scratch AS hostbin
COPY --from=hostbuild /out/host/ /

# ---- stage 3: minimal runtime ----------------------------------------------
# distroless/static carries CA certs (for a TLS Postgres DSN) and runs as an
# unprivileged user; the binary is fully static (CGO disabled), so no libc.
FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION=dev
LABEL org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.source=https://github.com/hyperscaleav/omniglass
COPY --from=build /out/omniglass /omniglass
# The data directory, owned by the runtime user, so a named volume mounted
# here (the lab's) starts out writable by it: the server keeps its generated
# encryption key and the bus's store under it.
COPY --from=build --chown=65532:65532 /out/data /var/lib/omniglass
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/omniglass"]
CMD ["server"]
