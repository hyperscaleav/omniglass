# Local dev loop + the build/run flow for the single binary. Production deploy
# is BYO Postgres; tests use ephemeral testcontainer Postgres.

.PHONY: build build-web web gen gen-web test test-short tidy

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
