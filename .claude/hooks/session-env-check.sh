#!/usr/bin/env bash
# SessionStart environment check for unattended loop runs (#500).
#
# Verifies the pieces a long agent session needs before work starts, so a loop
# fails loudly at minute zero instead of stalling mid-slice on a phantom:
# a missing Docker daemon (integration/e2e tiers, erdgen), a protoc or
# protoc-gen-go version off the gen-drift.yml pin (make gen reads as drift no
# local regeneration can fix), or a Node major off the CI pin (web tests, SPA
# build). Warn-only: always exits 0; findings land in the session context.

set -u

# Keep these in step with .github/workflows/gen-drift.yml (the same pins CI
# regenerates with) and setup-node's node-version.
PROTOC_PIN="34.1"
PROTOC_GEN_GO_PIN="v1.36.11"
NODE_MAJOR_PIN="22"

warn() { printf 'env-check: %s\n' "$1"; }

if ! docker info >/dev/null 2>&1; then
  warn "Docker daemon unreachable: the integration and e2e test tiers and erdgen (make gen) will fail. Start Docker before running make test or make gen."
fi

if command -v protoc >/dev/null 2>&1; then
  got="$(protoc --version 2>/dev/null | awk '{print $2}')"
  if [ "$got" != "$PROTOC_PIN" ]; then
    warn "protoc is $got, pin is $PROTOC_PIN (gen-drift.yml): make gen output will differ from CI and read as phantom drift."
  fi
else
  warn "protoc not installed (pin $PROTOC_PIN): make gen cannot regenerate the proto wire."
fi

if command -v protoc-gen-go >/dev/null 2>&1; then
  got="$(protoc-gen-go --version 2>/dev/null | awk '{print $2}')"
  if [ "$got" != "$PROTOC_GEN_GO_PIN" ]; then
    warn "protoc-gen-go is $got, pin is $PROTOC_GEN_GO_PIN (gen-drift.yml): make gen output will differ from CI and read as phantom drift."
  fi
else
  warn "protoc-gen-go not installed (pin $PROTOC_GEN_GO_PIN): make gen cannot regenerate the proto wire."
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
