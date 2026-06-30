# Local dev loop + the build/run flow for the single binary. Production deploy
# is BYO Postgres; tests use ephemeral testcontainer Postgres.

.PHONY: build build-web web image gen gen-web test test-short test-e2e tidy up down dev

# Build the single binary (no console embedded; serves the build-the-console
# placeholder under /web).
build:
	go build -o bin/omniglass ./cmd/omniglass

# Build the binary WITH the operator console embedded: run the Vite build into
# internal/webui/dist, then compile with -tags web so go:embed picks it up.
build-web: web
	go build -tags web -o bin/omniglass ./cmd/omniglass

# Build the SPA into the embed target (internal/webui/dist).
web:
	cd web && npm install && npm run build

# Build the container image CI publishes: the multistage Dockerfile (Vite build
# then `go build -tags web`) on a distroless base. VERSION stamps main.version;
# it defaults to the short commit sha. Override TAG/IMAGE/VERSION as needed.
IMAGE   ?= omniglass
TAG     ?= dev
VERSION ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
image:
	docker build -t $(IMAGE):$(TAG) --build-arg VERSION=$(VERSION) .

# Regenerate the derived artifacts from the Huma API (the source of truth): the
# OpenAPI 3.1 document (server-less, into api/openapi.{json,yaml}), the cobra
# command tree (internal/cli/api_gen.go), and the typed SPA client
# (web/src/api/schema.gen.ts). The spec is the seam every downstream client is
# generated from. Run after any API change; the results are committed and
# reviewed like code.
gen:
	go run ./cmd/openapigen
	go run ./cmd/cligen
	cd web && npm install && npm run gen:api

# Regenerate just the typed SPA client from the committed OpenAPI. Requires node.
gen-web:
	go run ./cmd/openapigen
	cd web && npm install && npm run gen:api

# Full gate: every test, including the testcontainer-backed integration and
# end-to-end tests (real Postgres). Green before commit and before merge.
test:
	go test ./...

# Fast iteration: unit/pure tests only. -short skips anything that needs a
# Postgres container or builds the binary.
test-short:
	go test -short ./...

# Browser-driven e2e of the console against the built binary: brings up the dev
# Postgres, embeds + serves the console, mints a token, and drives it with
# Playwright. Needs the browser once: (cd web && npx playwright install chromium).
test-e2e:
	bash web/e2e/run.sh

# Sync go.mod / go.sum to the actual import graph.
tidy:
	go mod tidy

# ---- local dev stack -------------------------------------------------------
# Bring up the dev Postgres (docker compose) and wait until it is ready. It
# matches config.DefaultDSN, so the server needs no env override.
up:
	docker compose up -d db
	@echo "waiting for postgres..."
	@until docker compose exec -T db pg_isready -U omniglass -d omniglass >/dev/null 2>&1; do sleep 1; done
	@echo "postgres ready on localhost:5432"

# Stop the dev stack. The named volume persists data across runs; add `-v`
# (docker compose down -v) to wipe it and re-mint a token on the next `make dev`.
down:
	docker compose down

# Full stack for a browser session: Postgres + the server with the console
# embedded. Creates (idempotently) a dev owner with password "dev", also mints a
# bearer token, then serves the API and console at http://localhost:8080/web.
# Ctrl-C stops the server; `make down` stops Postgres.
dev: up build-web
	@./bin/omniglass bootstrap dev --password dev >/dev/null 2>&1 || true
	@./bin/omniglass set-password dev dev >/dev/null 2>&1 || true
	@echo "console: http://localhost:8080/web"
	@echo "  sign in with username 'dev' and password 'dev', or paste a bearer token:"
	@./bin/omniglass token dev
	./bin/omniglass server
