# Local dev loop + the build/run flow for the single binary. Production deploy
# is BYO Postgres; tests use ephemeral testcontainer Postgres.

.PHONY: build build-web web image gen gen-proto gen-web test test-web test-short test-e2e bench clean-testcontainers tidy up down dev release-plan release-apply release-snapshot docs-shots docs-shots-check docs-shots-verify docs-walkthroughs dev-sim bench-sim test-netadmin walkthrough-stack dev-sim-container test-walkthrough-stack walkthroughs test-walkthroughs

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
# command tree (internal/cli/api_gen.go), the CLI reference page (from that
# command tree, into docs/), the protobuf telemetry wire, the typed SPA
# client (web/src/api/schema.gen.ts), and the schema facts the docs render
# storage tables from (docs/src/generated/schema.json, via erdgen). The spec is
# the seam every downstream client is generated from. Run after any API change;
# the results are committed and reviewed like code.
gen: gen-proto
	go run ./cmd/openapigen
	go run ./cmd/cligen
	go run ./cmd/docsgen
	go run ./cmd/erdgen
	go run ./cmd/seedgen
	go run ./cmd/labelgen
	go run ./cmd/configgen
	go run ./cmd/identitygen
	cd web && npm install && npm run gen:api

# Regenerate the protobuf telemetry wire (proto/og/v1/telemetry.pb.go) from its
# .proto source. protoc 34.1 must be on PATH and protoc-gen-go v1.36.11 installed
# (go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11). The
# TelemetryBatch is a NATS message, not a gRPC service, so no protoc-gen-go-grpc.
# Output is committed and reviewed like code; keep protoc + plugin pinned so the
# bytes are stable (.github/workflows/gen-drift.yml pins the same versions).
gen-proto:
	PATH="$(shell go env GOPATH)/bin:$$PATH" protoc --go_out=. --go_opt=paths=source_relative proto/og/v1/telemetry.proto

# Regenerate just the typed SPA client from the committed OpenAPI. Requires node.
gen-web:
	go run ./cmd/openapigen
	cd web && npm install && npm run gen:api

# Full gate: every test, including the testcontainer-backed integration and
# end-to-end tests (real Postgres) plus the SPA (vitest). Green before commit and
# before merge.
#
# TEST_PARALLEL caps how many package test binaries run at once (go test -p). It
# bounds both concurrent linking and, because each container-backed package spins
# its own ephemeral Postgres, the number of live testcontainers. The Go default
# (GOMAXPROCS, e.g. 16) lets every container-backed package start a Postgres at
# once alongside up to that many link jobs, which saturates memory on smaller
# machines (notably WSL2).
#
# The default is 1 because 4 took this box down: four concurrent Postgres
# containers plus four link jobs exhausted the WSL2 VM's memory and killed the
# whole VM, not just the run. A gate that can take the machine with it is not a
# gate, and a serial run that finishes is worth more than a parallel one that
# has to be restarted. It is a floor, not a ceiling: a machine with the memory
# for it should raise this, and the run gets faster with no other change.
# Override per invocation: make test TEST_PARALLEL=8.
TEST_PARALLEL ?= 1
test: test-web
	go test -p $(TEST_PARALLEL) ./...
	$(MAKE) test-netadmin

# The SPA typecheck + unit and theming-ban tests. Part of the local gate so
# web-only regressions (an illegible badge class, a broken page descriptor, a
# type error) are caught before merge, not just by the Go suite.
#
# typecheck runs FIRST and is a gate, not advice. vitest transpiles via esbuild
# and never typechecks, so a type error passes the suite silently: that is how
# three unresolved `node:` imports sat on main long enough to become background
# noise everybody scrolled past (#252).
test-web:
	cd web && npm install && npm run typecheck && npm run test

# Fast iteration: unit/pure tests only. -short skips anything that needs a
# Postgres container or builds the binary.
test-short:
	go test -short ./...

# Wall-clock benchmarks over the real Storage Gateway, at two estate sizes.
# DIAGNOSTIC, never a gate: nothing here runs in `make test` (a Go benchmark is
# inert without -bench), nothing asserts a duration, and no baseline is stored.
# The instrument that DOES gate is round-trip counting, which is an ordinary test
# (internal/storage/list_cost_test.go). Two instruments, different jobs: counting
# catches the N+1 exactly and deterministically, benchmarks catch what counting is
# blind to, a dropped index, a plan flip, a recursive CTE that stops being bounded.
#
# Run it deliberately, before and after a change that claims a performance
# effect, and COMPARE the two runs rather than reading either one alone:
#
#	make bench > /tmp/before.txt
#	# ...make the change...
#	make bench > /tmp/after.txt
#	benchstat /tmp/before.txt /tmp/after.txt
#
# benchstat is a separate install and is NOT on this box by default:
#
#	go install golang.org/x/perf/cmd/benchstat@latest
#
# -count=5 is what gives benchstat enough samples to say whether a difference is
# real; -run '^$$' keeps the tests from running alongside; -benchmem adds the
# allocation columns, which are the one part of the output the host cannot make
# noisy. Compare on an otherwise idle machine: a concurrent `make test` in
# another worktree moves these numbers more than most regressions would.
bench:
	go test -run '^$$' -bench . -benchmem -count=5 ./internal/storage/...

# The tests that need NET_ADMIN (impairments rewrite the firewall and tc of the
# network they run in) run in a container of their own, never on the host:
# NET_ADMIN, its own network namespace, and the ping sysctl the collector's
# unprivileged pinger needs. A Postgres container on a private network serves
# the full-stack test through the harness's OMNIGLASS_TEST_ADMIN_DSN. Packages
# run one at a time (-p 1): they share the one network namespace, and the
# simulator owns one firewall table and one qdisc in it. Docker is already a
# requirement of make test. Names carry the recipe shell's pid, so concurrent
# runs never share a network, and the traps stop this run's containers on every
# exit path, interrupts included (the same contract as web/e2e/lib/stack.sh);
# the test container is waited on in the background so a signal is handled at
# once rather than after the tests finish. The two module/build cache volumes
# are shared across runs on purpose.
NETADMIN_IMG ?= golang:1.25
NETADMIN_PKGS ?= ./internal/sim/impair/ ./internal/e2e/
NETADMIN_RUN ?= Netlink|Impair|Validate|PlanFor
test-netadmin:
	@set -e; net=omniglass-netadmin-$$$$; pg=$$net-pg; go=$$net-go; \
	trap 'docker stop $$go $$pg >/dev/null 2>&1 || :; n=0; until docker network rm $$net >/dev/null 2>&1; do \
		n=$$((n+1)); [ $$n -lt 10 ] || { echo "network $$net could not be removed" >&2; break; }; sleep 1; done' EXIT; \
	trap 'exit 130' INT; trap 'exit 143' TERM; trap 'exit 129' HUP; \
	docker network create $$net >/dev/null; \
	docker run -d --rm --name $$pg --network $$net -e POSTGRES_PASSWORD=netadmin postgres:18 >/dev/null; \
	n=0; until docker exec $$pg pg_isready -h 127.0.0.1 -U postgres >/dev/null 2>&1; do \
		n=$$((n+1)); [ $$n -lt 60 ] || { echo "postgres did not come up"; exit 1; }; sleep 1; done; \
	docker run --rm --name $$go --network $$net --cap-add NET_ADMIN --sysctl net.ipv4.ping_group_range="0 2147483647" \
		-v "$(CURDIR):/w" -w /w -v omniglass-netadmin-gomod:/go/pkg/mod -v omniglass-netadmin-gocache:/root/.cache/go-build \
		-e GOFLAGS=-buildvcs=false -e OMNIGLASS_NETADMIN=1 -e OMNIGLASS_TEST_ADMIN_DSN="postgres://postgres:netadmin@$$pg:5432/postgres?sslmode=disable" \
		$(NETADMIN_IMG) go test -tags netadmin -p 1 -count=1 -run '$(NETADMIN_RUN)' $(NETADMIN_PKGS) & wait $$!

# One simulator process serving a 1,000-device fleet: startup time, goroutines
# and heap while serving, and goroutines left behind after it stops.
bench-sim:
	go test -run '^$$' -bench BenchmarkServeThousandDevices -benchtime 3x ./internal/sim/

# Browser-driven e2e of the console against the built binary: brings up the dev
# Postgres, embeds + serves the console, mints a token, and drives it with
# Playwright. Needs the browser once: (cd web && npx playwright install chromium).
test-e2e:
	bash web/e2e/run.sh

# The walkthrough stack (web/e2e/walkthrough-stack.sh, recipe in
# web/e2e/lib/stack.sh): Postgres, the server seeded with the dev and simulated
# fleets, the node edge-hq enrolled with a one-time token, and the simulator
# with --impairments in the node's network namespace (NET_ADMIN there only).
# All in Docker, built from this tree with the Dockerfile, nothing on a fixed
# host port, unique per-run names, torn down on exit. walkthrough-stack holds
# it up for authoring a walkthrough and prints how a Playwright container
# reaches it; dev-sim-container is the same stack for exploring by hand, with
# the console published on an ephemeral 127.0.0.1 port (make dev-sim, in
# containers, impairments applied). test-walkthrough-stack proves the recipe:
# the node collects, and an impairment set through the API applies and repairs.
# Not part of make test: it builds the release image (minutes after a Go
# change), and the impairment loop itself is gated there by test-netadmin.
walkthrough-stack:
	bash web/e2e/walkthrough-stack.sh up

dev-sim-container:
	STACK_PUBLISH=1 bash web/e2e/walkthrough-stack.sh up

test-walkthrough-stack:
	bash web/e2e/walkthrough-stack.sh check

# Walkthroughs (#924): web/e2e/walkthroughs/<id>.yaml, recorded on the
# walkthrough stack with a visible cursor, captions and a title card, to
# web/e2e/walkthroughs/out/<id>/ (an mp4, an animated preview.webp, poster.png,
# a still per step; gitignored). A walkthrough is a test: a step whose result
# never appears, or a recording over the budget (walkthroughlib.mjs), fails the
# run.
# make walkthroughs ID=<id> records one (ID="<a> <b>", several), listing any
# that failed in out/failed.txt. test-walkthroughs is the recorder's own test
# (the failure and budget paths). Neither is part of make test: both build
# the release image.
walkthroughs:
	bash web/e2e/walkthroughs.sh record $(ID)

test-walkthroughs:
	bash web/e2e/walkthroughs.sh selftest

# Backstop cleanup: force-remove any orphaned Postgres test containers left by
# an interrupted run (a hard kill before the harness TestMain or the ryuk reaper
# could reclaim them). Scoped by the testcontainers label AND the postgres:18
# image so it never touches the compose dev stack (`make up`) or a running ryuk.
# Destructive by design; run it deliberately.
clean-testcontainers:
	docker ps -aq --filter "label=org.testcontainers=true" --filter "ancestor=postgres:18" | xargs -r docker rm -f

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

# Local dry build of the release matrix (all OS/arch archives + checksums), with
# no tag and no publish, to validate .goreleaser.yaml. SBOM is skipped (needs
# syft); CI runs the full path. Requires goreleaser
# (go install github.com/goreleaser/goreleaser/v2@latest).
release-snapshot:
	goreleaser release --snapshot --clean --skip=publish,sbom

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

# Capture the docs screenshots against the real console (dev stack + Playwright),
# writing PNGs into docs/public/screenshots/ from each page's `screenshots`
# frontmatter. Commit the regenerated images like any other generated resource.
# Needs the Playwright browser once: (cd web && npx playwright install chromium).
docs-shots:
	bash docs/screenshots/capture.sh

# Freshness gate for the screenshots, the visual sibling of `make gen`: recapture
# and diff the masked BASELINES only (#398/#623/#789): the nondeterministic
# regions a capture renders (v7-uuid text, seed-time stamps, strip weights) are
# masked in the baseline through each page's frontmatter, so the pinned
# browser's rasters are byte-stable and a nonzero diff is a real,
# un-recaptured UI change. The clean shots the docs embed are not diffed;
# their masked regions legitimately move every capture.
# The temp dirs are repo-relative so they land inside the capture container's mount.
docs-shots-check:
	@tmp=.docs-shots-tmp; mkdir -p $$tmp/clean $$tmp/baseline; \
	  DOCS_SHOTS_OUT=$$tmp/clean DOCS_SHOTS_BASELINE=$$tmp/baseline bash docs/screenshots/capture.sh && \
	  node web/e2e/docs-shots-diff.mjs $$tmp/baseline docs/screenshots/baseline; \
	  rc=$$?; if [ $$rc -eq 0 ]; then rm -f $$tmp/clean/*.png $$tmp/baseline/*.png; rmdir $$tmp/clean $$tmp/baseline $$tmp 2>/dev/null || true; fi; exit $$rc

# Structural gate for the screenshots (no browser, fully deterministic): every
# `screenshots` frontmatter entry has a committed PNG, every PNG is declared, and
# ids are unique. Fast enough to run on every PR (see .github/workflows/docs-shots.yml).
docs-shots-verify:
	node web/e2e/docs-shots-verify.mjs

# Walkthroughs in the docs (#926): copy each walkthrough a page declares in its
# `walkthroughs` frontmatter (manifest, per-step stills, animated preview) from
# web/e2e/walkthroughs/out/<id>/ into docs/public/walkthroughs/<id>/, committed
# like the screenshots. Record first (make walkthroughs ID=<id>). The mp4 stays
# out of git; DOCS_WALKTHROUGH_VIDEO=1 copies it too, for a local build that
# plays it (gitignored).
docs-walkthroughs:
	node docs/scripts/copy-walkthroughs.mjs

# Full stack for a browser session: Postgres + the server with the console
# embedded. Creates (idempotently) a dev owner with password "dev", also mints a
# bearer token, then serves the API and console at http://localhost:8080/web.
# Ctrl-C stops the server; `make down` stops Postgres.
dev: up build-web
	@./bin/omniglass bootstrap dev --password dev >/dev/null 2>&1 || true
	@./bin/omniglass set-password dev dev >/dev/null 2>&1 || true
	@./bin/omniglass seed-dev || true
	@echo "console: http://localhost:8080/web"
	@echo "  sign in with username 'dev' and password 'dev', or paste a bearer token:"
	@./bin/omniglass token dev --description "dev login token"
	./bin/omniglass server

# The simulated dev fleet, collected for real: run it in a second terminal while
# `make dev` serves. It runs the simulator and the node side by side as edge-hq,
# the dev node the seed placed every simulated device's endpoints on, so samples
# land across the fleet. The first run enrolls edge-hq with the dev owner's token
# (the dev seed keeps no node secret) and keeps the node token in
# bin/edge-hq.token; later runs reuse it. Empty the file to enroll afresh.
# SIM_PORT_OFFSET lifts the simulated devices' ports below 1024 so this runs
# unprivileged; the dev seed aims its endpoints at the lifted ports, and a test
# holds this default to devseed.SimPortOffset.
SIM_PORT_OFFSET ?= 10000
dev-sim: build
	@if [ ! -s bin/edge-hq.token ]; then \
		owner=$$(./bin/omniglass token dev --description "dev sim enroll" | sed -n 's/^  \([^ ][^ ]*\)$$/\1/p'); \
		./bin/omniglass node enroll edge-hq --server http://localhost:8080 --token "$$owner" \
			| sed -n 's/.*"token": *"\([^"]*\)".*/\1/p' > bin/edge-hq.token; \
	fi; \
	test -s bin/edge-hq.token || { echo "dev-sim: could not enroll edge-hq; is make dev serving?"; exit 1; }; \
	test "$(SIM_PORT_OFFSET)" = "10000" || echo "dev-sim: SIM_PORT_OFFSET=$(SIM_PORT_OFFSET), but the dev seed aims its endpoints at ports lifted by 10000; the node will not reach the devices"; \
	tok=$$(cat bin/edge-hq.token); \
	./bin/omniglass node run --server http://localhost:8080 --name edge-hq --token "$$tok" & node=$$!; \
	trap 'kill $$node 2>/dev/null' EXIT INT TERM; \
	sleep 2; kill -0 $$node 2>/dev/null || { echo "dev-sim: the node exited at startup (see above); empty bin/edge-hq.token to enroll afresh"; exit 1; }; \
	./bin/omniglass sim run --server http://localhost:8080 --name edge-hq --token "$$tok" --port-offset $(SIM_PORT_OFFSET)
