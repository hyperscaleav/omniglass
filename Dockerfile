# Multistage build for the single Omniglass binary with the operator console
# embedded. Stage `web` builds the Vite SPA into internal/webui/dist; stage
# `build` compiles the Go binary with -tags web so go:embed picks the SPA up;
# the final stage is a distroless static image running the binary as nonroot.
# One image is the whole app: server, node, migrate, bootstrap, and token modes.

# ---- stage 1: build the console (Vite -> internal/webui/dist) --------------
FROM node:22-bookworm-slim AS web
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
FROM golang:1.25.3-bookworm AS build
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
RUN CGO_ENABLED=0 go build -trimpath -tags web \
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
