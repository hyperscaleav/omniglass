# Local dev loop + the build/run flow for the single binary. Production deploy
# is BYO Postgres; tests use ephemeral testcontainer Postgres.

.PHONY: build build-web web gen gen-web test test-short tidy up down dev

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
# embedded. Mints (idempotently) a dev owner token, prints it once, then serves
# the API and console at http://localhost:8080/web. Ctrl-C stops the server;
# `make down` stops Postgres.
dev: up build-web
	@./bin/omniglass bootstrap dev >/dev/null 2>&1 || true
	@./bin/omniglass token dev
	@echo "console: http://localhost:8080/web   (paste the token above to sign in)"
	./bin/omniglass server
