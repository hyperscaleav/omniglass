#!/usr/bin/env bash
# SessionStart environment check for unattended loop runs (#500, remedies #514).
#
# Verifies the pieces a long agent session needs before work starts, so a loop
# fails loudly at minute zero instead of stalling mid-slice on a phantom.
# Fix-first where a fix is proven, warn-with-remedy where it is not: the #502
# pilot showed the remote container's Docker daemon starts fine when asked, the
# pinned protoc toolchain installs cleanly from the release URLs, and a blocked
# Docker Hub CDN is worked around by a mirror pull plus retag. Warn-only:
# always exits 0; findings land in the session context.

set -u

# Keep these in step with .github/workflows/gen-drift.yml (the same pins CI
# regenerates with) and setup-node's node-version.
PROTOC_PIN="34.1"
PROTOC_GEN_GO_PIN="v1.36.11"
NODE_MAJOR_PIN="22"

warn() { printf 'env-check: %s\n' "$1"; }

# Docker: try to start the daemon before crying wolf. Bounded to a few seconds
# so a genuinely docker-less environment does not slow session start.
if ! docker info >/dev/null 2>&1; then
  if command -v dockerd >/dev/null 2>&1; then
    service docker start >/dev/null 2>&1 || (dockerd >/tmp/session-env-dockerd.log 2>&1 &)
    for _ in 1 2 3 4 5 6; do
      docker info >/dev/null 2>&1 && break
      sleep 1
    done
  fi
fi
if ! docker info >/dev/null 2>&1; then
  warn "Docker daemon unreachable (start attempt failed): the integration and e2e test tiers and erdgen (make gen) will fail. See /tmp/session-env-dockerd.log if dockerd was tried."
else
  # Daemon up: if the egress policy blocks Docker Hub's CDN, testcontainers
  # pulls fail at run time; name the proven workaround now instead.
  missing=""
  docker image inspect postgres:18 >/dev/null 2>&1 || missing="postgres:18"
  docker image inspect testcontainers/ryuk:0.13.0 >/dev/null 2>&1 || missing="$missing testcontainers/ryuk:0.13.0"
  if [ -n "$missing" ]; then
    warn "images not cached ($missing); if Docker Hub pulls 403 through the proxy, pull via mirror and retag, e.g.: docker pull mirror.gcr.io/library/postgres:18 && docker tag mirror.gcr.io/library/postgres:18 postgres:18 (same pattern for mirror.gcr.io/testcontainers/ryuk:0.13.0)"
  fi
fi

if command -v protoc >/dev/null 2>&1; then
  got="$(protoc --version 2>/dev/null | awk '{print $2}')"
  if [ "$got" != "$PROTOC_PIN" ]; then
    warn "protoc is $got, pin is $PROTOC_PIN (gen-drift.yml): make gen output will differ from CI and read as phantom drift. Remedy: install the pinned release zip from github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_PIN}/ (bin/protoc plus include/ into /usr/local)."
  fi
else
  warn "protoc not installed (pin $PROTOC_PIN): make gen cannot regenerate the proto wire. Remedy: install the pinned release zip from github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_PIN}/ (bin/protoc plus include/ into /usr/local)."
fi

if command -v protoc-gen-go >/dev/null 2>&1; then
  got="$(protoc-gen-go --version 2>/dev/null | awk '{print $2}')"
  if [ "$got" != "$PROTOC_GEN_GO_PIN" ]; then
    warn "protoc-gen-go is $got, pin is $PROTOC_GEN_GO_PIN (gen-drift.yml): make gen output will differ from CI and read as phantom drift. Remedy: go install google.golang.org/protobuf/cmd/protoc-gen-go@${PROTOC_GEN_GO_PIN} (and PATH the Go bin dir)."
  fi
else
  warn "protoc-gen-go not installed (pin $PROTOC_GEN_GO_PIN): make gen cannot regenerate the proto wire. Remedy: go install google.golang.org/protobuf/cmd/protoc-gen-go@${PROTOC_GEN_GO_PIN} (and PATH the Go bin dir)."
fi

if command -v node >/dev/null 2>&1; then
  got="$(node -v 2>/dev/null | sed 's/^v//' | cut -d. -f1)"
  if [ "$got" != "$NODE_MAJOR_PIN" ]; then
    warn "node major is $got, CI pins $NODE_MAJOR_PIN: web tests and the SPA build may differ from CI."
  fi
else
  warn "node not installed (CI pins major $NODE_MAJOR_PIN): web tests and make gen's client regeneration will fail."
fi

exit 0
