#!/usr/bin/env bash
# docs-shots-guard.sh <head_ref> [head_repo base_repo]: fail a pull request whose diff modifies, deletes
# or renames an existing docs screenshot (#2561).
#
# Reads `git diff --name-status` on stdin. A PR may ADD a shot (a new file cannot
# conflict with another PR, and the docs build needs it); it may not touch an
# existing one, because git cannot merge binaries and main recaptures the shots
# after every merge. The recapture branch the workflow opens (claude/docs-shots-
# recapture, claude/docs-shots-recapture-<release>) is exempt: it is the one
# writer of existing shots, and only when the branch lives in this repository
# (a fork cannot claim the name: head_repo must equal base_repo).
set -euo pipefail

head_ref=${1:-}
head_repo=${2:-}
base_repo=${3:-}
if [ -n "$head_repo" ] && [ "$head_repo" = "$base_repo" ]; then
  case "$head_ref" in
    claude/docs-shots-recapture | claude/docs-shots-recapture-*) exit 0 ;;
  esac
fi

bad=()
while IFS=$'\t' read -r status path rest; do
  [ -n "${status:-}" ] || continue
  case "$status" in
    A*) continue ;;
  esac
  # A rename or copy lists the old path first and the new one second; the old
  # path is the existing file being removed.
  for p in "$path" "${rest:-}"; do
    case "$p" in
      docs/public/screenshots/* | docs/screenshots/baseline/*) bad+=("$status $p") ;;
    esac
  done
done

if [ ${#bad[@]} -gt 0 ]; then
  printf '%s\n' "${bad[@]}" >&2
  echo "::error::This PR modifies an existing docs screenshot. Main recaptures the docs shots after every merge (#2561): leave existing PNGs under docs/public/screenshots/ and docs/screenshots/baseline/ as they are (a new shot for a new id is fine), and put the visual evidence under .github/screenshots/<n>/." >&2
  exit 1
fi
