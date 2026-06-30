# Local dev loop + the build/run flow for the single binary. Production deploy
# is BYO Postgres; tests use ephemeral testcontainer Postgres.

.PHONY: build build-web web image gen gen-web test test-short test-e2e tidy up down dev release-plan release-apply

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

# ---- release ---------------------------------------------------------------
# Releases are cut MANUALLY (not on merge to main). `release-plan` is the
# dry-run preview: it prints the next version and the generated notes and
# publishes nothing. `release-apply` performs the release: it pushes the git tag
# and creates the GitHub Release. Run both from an up-to-date `main`.
# semantic-release reads the conventional-commit subjects since the last tag
# (.releaserc.json drives the plugins).
SEMANTIC_RELEASE := npx --yes -p semantic-release@24 \
	-p @semantic-release/commit-analyzer \
	-p @semantic-release/release-notes-generator \
	-p @semantic-release/github semantic-release

# The token is resolved inside the recipe shell at runtime (prefer an exported
# GITHUB_TOKEN, else the gh CLI), never stored in a make variable, so it is
# never echoed into the build log. The recipe is `@`-silenced and fails fast
# with an actionable message when no token is available.
release-plan:
	@token="$${GITHUB_TOKEN:-$$(gh auth token)}"; \
	  [ -n "$$token" ] || { echo "release: no GITHUB_TOKEN set and gh is not authenticated (run: gh auth login)"; exit 1; }; \
	  GITHUB_TOKEN="$$token" $(SEMANTIC_RELEASE) --dry-run --no-ci

release-apply:
	@token="$${GITHUB_TOKEN:-$$(gh auth token)}"; \
	  [ -n "$$token" ] || { echo "release: no GITHUB_TOKEN set and gh is not authenticated (run: gh auth login)"; exit 1; }; \
	  GITHUB_TOKEN="$$token" $(SEMANTIC_RELEASE) --no-ci

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
