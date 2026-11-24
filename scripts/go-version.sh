#!/usr/bin/env bash
# Print the Go version go.mod asks for (#2596): the toolchain directive when
# present, otherwise the go directive, without the "go" prefix. This is the one
# place the image build learns the Go version, so go.mod stays the only pin.
# Usage: scripts/go-version.sh [path/to/go.mod]
set -euo pipefail
mod=${1:-go.mod}
v=$(awk '$1=="toolchain"{t=$2} $1=="go"{g=$2} END{print (t!=""?t:g)}' "$mod")
v=${v#go}
[ -n "$v" ] || { echo "go-version: no go or toolchain directive in $mod" >&2; exit 1; }
printf '%s\n' "$v"
