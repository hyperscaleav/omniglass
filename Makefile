# Local dev loop + the build/run flow for the single binary. Production deploy
# is BYO Postgres; tests use ephemeral testcontainer Postgres.

.PHONY: build gen test test-short tidy

# Build the single binary.
build:
	go build -o bin/omniglass ./cmd/omniglass

# Regenerate the derived artifacts from the Huma API (the source of truth):
# first the OpenAPI 3.1 document (server-less, into api/openapi.{json,yaml}),
# then the cobra command tree (internal/cli/generated.go) from that spec. The
# spec is the seam every downstream client is generated from. Run after any API
# change; the result is committed and reviewed like code.
gen:
	go run ./cmd/openapigen
	go run ./cmd/cligen

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
