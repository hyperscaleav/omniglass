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

# ---- stage 1: build the console (Vite -> internal/webui/dist) --------------
# Arch-independent JS bundle: build on the native host regardless of target.
FROM --platform=$BUILDPLATFORM node:22-bookworm-slim AS web
WORKDIR /src/web
# Install against the lockfile first so the deps layer caches across source-only
# changes.
COPY web/package.json web/package-lock.json ./
RUN npm ci
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
RUN go mod download
# Full source, then overlay the freshly built SPA from the web stage so
# `//go:embed all:dist` (under -tags web) has real content to embed.
COPY . .
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
# version is stamped into main.version; CI passes the commit SHA (or a tag).
ARG VERSION=dev
# TARGETOS/TARGETARCH are provided by buildx per target in the platform list.
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -tags web \
      -ldflags "-X main.version=${VERSION} -s -w" \
      -o /out/omniglass ./cmd/omniglass

# ---- stage 3: minimal runtime ----------------------------------------------
# distroless/static carries CA certs (for a TLS Postgres DSN) and runs as an
# unprivileged user; the binary is fully static (CGO disabled), so no libc.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/omniglass /omniglass
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/omniglass"]
CMD ["server"]
