#!/usr/bin/env bash
# Record demos (#1132): a demo is a recording of a feature working, made by one
# of two recorders, and a demo id names one demo of either kind:
#
# - a terminal demo, scripts/demo/<id>.tape, for CLI and API work: VHS types it
#   into a real terminal (web/e2e/tapes.sh), to scripts/demo/out/<id>/;
# - a walkthrough, web/e2e/walkthroughs/<id>.yaml, for a console flow:
#   Playwright plays it in the console (web/e2e/walkthroughs.sh), to
#   web/e2e/walkthroughs/out/<id>/.
#
#   demo.sh record [ID...]   record each demo named (default: every demo of both
#                            kinds), each recorder on a throwaway lab of its own
#                            (web/e2e/lib/lab.sh); exits non-zero if any demo
#                            failed, and each recorder lists its failed ids in
#                            its out/failed.txt
#
# This is what make demo and the run:demo workflow's record step run.
#
# Prereqs: docker, and `make` run first (make demo and the workflow do): the
# lab is the binary and the image it builds.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

TAPES=scripts/demo
WALKS=web/e2e/walkthroughs

mode=${1:-record}
[ $# -eq 0 ] || shift
if [ "$mode" != record ]; then
  echo "usage: $0 record [ID...]" >&2
  exit 2
fi

tapes=()
walks=()
if [ $# -eq 0 ]; then
  mapfile -t tapes < <(find "$TAPES" -maxdepth 1 -name '*.tape' -exec basename {} .tape \; | sort)
  mapfile -t walks < <(find "$WALKS" -maxdepth 1 -name '*.yaml' -exec basename {} .yaml \; | sort)
else
  for id in "$@"; do
    [[ $id =~ ^[a-z0-9][a-z0-9-]*$ ]] || { echo "demo: demo id \"$id\" must be lowercase letters, digits and hyphens" >&2; exit 2; }
    if [ -f "$TAPES/$id.tape" ] && [ -f "$WALKS/$id.yaml" ]; then
      echo "demo: \"$id\" names both $TAPES/$id.tape and $WALKS/$id.yaml; a demo id names one demo" >&2
      exit 2
    elif [ -f "$TAPES/$id.tape" ]; then
      tapes+=("$id")
    elif [ -f "$WALKS/$id.yaml" ]; then
      walks+=("$id")
    else
      echo "demo: no demo \"$id\" (neither $TAPES/$id.tape nor $WALKS/$id.yaml)" >&2
      exit 2
    fi
  done
fi

rc=0
if [ "${#walks[@]}" -gt 0 ]; then
  bash web/e2e/walkthroughs.sh record "${walks[@]}" || rc=1
fi
if [ "${#tapes[@]}" -gt 0 ]; then
  bash web/e2e/tapes.sh record "${tapes[@]}" || rc=1
fi
exit "$rc"
