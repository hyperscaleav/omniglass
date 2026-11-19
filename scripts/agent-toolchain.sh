#!/usr/bin/env bash
# What make preflight needs on a GitHub runner beyond Go, Node and Docker
# (#2231), for the agent workflows: the protoc pair make gen runs, pnpm for the
# docs build, and the ICMP sysctl test-go-rest's capability gate wants. The pins
# match gen-drift.yml and docs-build.yml; scripts/preflight_test.go fails when
# they drift apart.
set -euo pipefail

PROTOC_VERSION="34.1"
PROTOC_GEN_GO_VERSION="v1.36.11"
PNPM_VERSION="10"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL -o "$tmp/protoc.zip" \
  "https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-linux-x86_64.zip"
sudo unzip -o -q "$tmp/protoc.zip" -d /usr/local bin/protoc 'include/*'
go install "google.golang.org/protobuf/cmd/protoc-gen-go@${PROTOC_GEN_GO_VERSION}"
npm install -g --silent "pnpm@${PNPM_VERSION}"
sudo sysctl -w net.ipv4.ping_group_range="0 2147483647"
