# Local dev loop + the build/run flow for the single binary. Production deploy
# is BYO Postgres; tests use ephemeral testcontainer Postgres.

.PHONY: build gen test test-short tidy

# Build the single binary.
build:
	go build -o bin/omniglass ./cmd/omniglass

# Regenerate the OpenAPI 3.1 document from the Huma API (the source of truth),
# server-less, into api/openapi.{json,yaml}. The committed spec is the seam the
# typed SPA client and CLI are generated from in later slices; for now it is
# one route. Run after any API change.
gen:
	go run ./cmd/openapigen

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
