#!/usr/bin/env bash
# docs-shots-plan.sh: the decisions the docs-shots recapture job makes (#2602), kept
# out of the workflow so a test can drive them.
#
#   docs-shots-plan.sh select
#       Reads `git status --porcelain` on stdin. Prints the paths to commit, one per
#       line: each masked baseline under docs/screenshots/baseline/ that changed
#       (added, modified or deleted), plus its clean twin of the same name under
#       docs/public/screenshots/. A clean shot whose baseline did not change is left
#       out: its live regions (ages, timestamps) differ on every capture, so it would
#       open a PR after nearly every merge with no UI change. No baseline changed,
#       no output.
#
#   docs-shots-plan.sh branch <base> <sha> <fresh>
#       Prints the branch to push the recapture to. <fresh> is "true" when the open
#       recapture PR is in the merge queue or a push to its branch was refused
#       (GitHub locks a queued PR's branch, GH006): the answer is then a new branch
#       named for the commit, so the job never fails main for a reason that is not a
#       broken capture. Otherwise the standing branch.
set -euo pipefail

cmd=${1:-}
case "$cmd" in
  select)
    while IFS= read -r line; do
      [ -n "$line" ] || continue
      path=${line:3}
      # A rename lists "old -> new"; the new path is the file that exists.
      path=${path##* -> }
      path=${path//\"/}
      case "$path" in
        docs/screenshots/baseline/*)
          printf '%s\n' "$path" "docs/public/screenshots/${path#docs/screenshots/baseline/}"
          ;;
      esac
    done | sort -u
    ;;
  branch)
    base=${2:?base}
    sha=${3:?sha}
    fresh=${4:-false}
    if [ "$base" = main ]; then
      branch=claude/docs-shots-recapture
    else
      branch="claude/docs-shots-recapture-${base//\//-}"
    fi
    if [ "$fresh" = true ]; then
      branch="$branch-${sha:0:7}"
    fi
    printf '%s\n' "$branch"
    ;;
  *)
    echo "usage: docs-shots-plan.sh select < git-status-porcelain | branch <base> <sha> <fresh>" >&2
    exit 2
    ;;
esac
