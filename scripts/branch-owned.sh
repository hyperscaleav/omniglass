#!/usr/bin/env bash
# Is a branch still held by another session? (#1112)
#
# The autopilot must never adopt a branch a live session is building: once it
# copied an unpushed in-progress branch, shipped it, and opened a second PR for
# the same issue. Run this before resuming or adopting any existing branch or
# worktree; an owned branch is skipped.
#
#   branch-owned.sh <branch>    run from inside the repo (any worktree of it)
#
# A branch is owned when any of these holds, checked in this order:
#   locked   it is checked out in a worktree that `git worktree list` shows as
#            locked (the harness locks a live session's worktree)
#   claimed  a claim file sits beside its worktree: <worktree path>.claim. The
#            harness does not write one today; a missing file is no claim, and
#            this is the hook for when it does
#   active   its tip commit, or the newest file in its worktree (outside .git
#            and node_modules), is newer than the window: 24 hours, or
#            AUTOPILOT_OWNED_WINDOW_HOURS
#
# Prints "owned <reason>" or "free". Exit 0 owned, 1 free (a branch with no
# worktree and no commit in the window is free), 2 unknown branch, 64 bad usage.
# Prereqs: bash 4.4+, git, find, awk.
set -euo pipefail
shopt -s inherit_errexit

if [ $# -ne 1 ] || [ -z "$1" ]; then
  printf 'usage: branch-owned.sh <branch>\n' >&2
  exit 64
fi
branch=$1

window_hours=${AUTOPILOT_OWNED_WINDOW_HOURS:-24}
if ! [[ "$window_hours" =~ ^[0-9]+$ ]]; then
  printf 'branch-owned.sh: AUTOPILOT_OWNED_WINDOW_HOURS must be a whole number of hours, got %q\n' "$window_hours" >&2
  exit 64
fi

# A local branch wins; a remote-tracking one still dates a pushed branch.
ref=
for candidate in "refs/heads/$branch" "refs/remotes/origin/$branch"; do
  if git rev-parse --verify --quiet "$candidate^{commit}" >/dev/null; then
    ref=$candidate
    break
  fi
done
if [ -z "$ref" ]; then
  printf 'branch-owned.sh: no branch %s here (local or origin)\n' "$branch" >&2
  exit 2
fi

# The worktree holding the branch, and whether git has it locked. The porcelain
# list is blank-line separated blocks of "worktree P", "branch R", "locked [why]".
worktree= locked=
while IFS=$'\t' read -r path is_locked; do
  worktree=$path
  locked=$is_locked
done < <(git worktree list --porcelain | awk -v want="refs/heads/$branch" '
  /^worktree / { path = substr($0, 10); br = ""; lk = "no" }
  /^branch /   { br = substr($0, 8) }
  /^locked/    { lk = "yes" }
  /^$/         { if (br == want) printf "%s\t%s\n", path, lk; path = "" }
  END          { if (path != "" && br == want) printf "%s\t%s\n", path, lk }
')

if [ "$locked" = yes ]; then
  printf 'owned locked\n'
  exit 0
fi

if [ -n "$worktree" ] && [ -e "$worktree.claim" ]; then
  printf 'owned claimed\n'
  exit 0
fi

newest=$(git log -1 --format=%ct "$ref")
if [ -n "$worktree" ] && [ -d "$worktree" ]; then
  file_newest=$(find "$worktree" \( -name .git -o -name node_modules \) -prune -o -type f -printf '%T@\n' |
    sort -n | tail -n 1)
  file_newest=${file_newest%.*}
  if [ -n "$file_newest" ] && [ "$file_newest" -gt "$newest" ]; then
    newest=$file_newest
  fi
fi

if [ $(($(date +%s) - newest)) -lt $((window_hours * 3600)) ]; then
  printf 'owned active\n'
  exit 0
fi

printf 'free\n'
exit 1
