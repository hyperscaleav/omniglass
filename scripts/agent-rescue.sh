#!/usr/bin/env bash
# Keep an agent's work when its run ends before it pushed it.
#
# claude -p exits the moment the model ends its turn, so an agent that stops
# early (run 44743523903 on #2223 ended at turn 142 of 200 with a background
# Monitor running) leaves its commits and edits in a checkout the runner then
# throws away. agent-build and agent-fix run this after the agent, with no
# token in the step, to turn whatever is left into one pushable branch.
#
#   agent-rescue.sh snapshot   read the checkout, print key=value lines:
#                                push=true|false  reason=<why not, when false>
#                                branch= sha= remote_sha= ahead= dirty=
#   agent-rescue.sh body       read {issue, branch, dirty, ahead, last_message}
#                              as JSON on stdin, print a draft PR body
#
# snapshot reads, from the environment:
#   WORKSPACE   the agent's checkout (the agent may have run git worktree add)
#   BARE        a path that does not exist yet: the fresh bare repo it builds
#   ISSUE       the issue the branch is for, named in the wip commit
#   REPO_URL    where main and the remote branch are read from, with no token
#   BRANCH      the branch to rescue (agent-fix: the PR's head). Unset, the
#               agent's own <type>/issue-ISSUE-<name> branch is found
#   AUTHOR_NAME, AUTHOR_EMAIL   who the wip commit is by
#
# The agent controls the checkout's .git (hooks, core.fsmonitor and other config
# run code on a git command), so nothing here runs a working-tree git command
# beyond ls-files, with a scrubbed environment, and every object is built in the
# fresh bare repo. Uncommitted work is copied by file list into a temp tree and
# committed there as "wip: agent stopped before finishing, Refs #ISSUE" on top of
# the branch. main's agent config (the paths agent-sync.sh lists) is taken from
# the branch, never from the checkout, where claude-code-action put main's copies.
# A branch that changes .github/workflows is refused (exit 1): the agent's push
# may not carry it. Prereqs: bash 4.4+, git, GNU tar and coreutils.
set -euo pipefail
shopt -s inherit_errexit

mode=${1:-}
case "$mode" in snapshot | body) ;; *)
  printf 'usage: agent-rescue.sh snapshot|body\n' >&2
  exit 64
  ;;
esac

if [ "$mode" = body ]; then
  jq -r '
    def lines: split("\n") | map("> " + .) | join("\n");
    "## Outcome\n\n"
    + "The agent building #\(.issue) stopped before it finished, so this PR holds what it had when the run ended. It is a draft and not ready for review.\n\n"
    + "Closes #\(.issue)\n\n"
    + "## Scope\n\n"
    + "- **In:** \(.ahead) commit(s) on `\(.branch)`" + (if .dirty then ", plus the uncommitted edits, saved as a `wip:` commit" else "" end) + ".\n"
    + "- **Thin cut:** whatever the agent had not finished. Its last message says what; it is below.\n"
    + "- **Deferred:** everything the ticket asks for that is not in the diff.\n\n"
    + "## Proof (ran fresh)\n\n"
    + "Not run. Nothing here passed `make preflight` after the run stopped.\n\n"
    + "## Visual confirmation\n\nNot taken.\n\n"
    + "## Demo\n\ndemo: none (the agent stopped before finishing)\n\n"
    + "## Docs and review\n\n- **Docs:** not checked.\n- **Review:** not run.\n\n"
    + "## Decisions\n\n- **Need from you:** continue the work with `@hyperscale-agent` on this PR, or take the branch over.\n\n"
    + "## Risk\n\nThe branch is whatever the agent left, unreviewed and possibly red.\n\n"
    + "## The agent'"'"'s last message\n\n"
    + ((.last_message // "") | if . == "" then "_The agent left no message._" else lines end) + "\n"
  '
  exit 0
fi

: "${WORKSPACE:?}" "${BARE:?}" "${ISSUE:?}" "${REPO_URL:?}"
[[ "$ISSUE" =~ ^[0-9]+$ ]] || { printf 'agent-rescue: ISSUE %q is not a number\n' "$ISSUE" >&2; exit 64; }
[ ! -e "$BARE" ] || { printf 'agent-rescue: %s already exists; it must be a fresh path\n' "$BARE" >&2; exit 64; }
BASE_REF=${BASE_REF:-main}
export GIT_TERMINAL_PROMPT=0 GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export LC_ALL=C
export GIT_AUTHOR_NAME=${AUTHOR_NAME:-agent} GIT_COMMITTER_NAME=${AUTHOR_NAME:-agent}
export GIT_AUTHOR_EMAIL=${AUTHOR_EMAIL:-agent@users.noreply.github.com} GIT_COMMITTER_EMAIL=${AUTHOR_EMAIL:-agent@users.noreply.github.com}

fail() { printf 'agent-rescue: %s\n' "$*" >&2; exit 1; }
gitb() { git -C "$BARE" -c core.hooksPath=/dev/null "$@"; }
# The one git command run in the agent's tree, with none of the run's variables:
# a hook or filter it tripped cannot reach a token or the runner's file commands.
ws_ls() { env -i PATH="$PATH" HOME=/nonexistent GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 \
  git -C "$1" -c core.hooksPath=/dev/null -c core.fsmonitor=false ls-files -z "${@:2}"; }

git init -q --bare "$BARE"
gitb fetch -q --no-tags "file://${WORKSPACE}" '+refs/heads/*:refs/ws/*' 2>/dev/null || true
gitb fetch -q --no-tags "$REPO_URL" "+refs/heads/${BASE_REF}:refs/remotes/origin/${BASE_REF}" \
  || fail "could not fetch ${BASE_REF} to check the branch against"
main="refs/remotes/origin/${BASE_REF}"

# The branch: the one asked for, else the agent's own for this issue (the one
# its checkout has out, or the newest).
branch=${BRANCH:-}
if [ -z "$branch" ]; then
  out=$(sed -n 's#^ref: refs/heads/##p' "${WORKSPACE}/.git/HEAD" 2>/dev/null || true)
  re="^[a-z]+/issue-${ISSUE}-[A-Za-z0-9._-]+$"
  if [[ "$out" =~ $re ]] && gitb rev-parse -q --verify "refs/ws/${out}" >/dev/null; then
    branch=$out
  else
    branch=$(gitb for-each-ref --sort=-committerdate --format='%(refname:strip=2)' 'refs/ws/' \
      | sed 's#^ws/##' | grep -E "$re" | head -n 1 || true)
  fi
fi
[ -z "$branch" ] || [[ "$branch" =~ ^[A-Za-z0-9._/-]+$ ]] || fail "branch ${branch} has characters a branch of this repo does not"

# Where the branch stands on GitHub, and what the agent's checkout has.
remote_sha=""
if [ -n "$branch" ] && gitb fetch -q --no-tags "$REPO_URL" "+refs/heads/${branch}:refs/remotes/origin/${branch}" 2>/dev/null; then
  remote_sha=$(gitb rev-parse "refs/remotes/origin/${branch}")
fi
tip=""
[ -z "$branch" ] || tip=$(gitb rev-parse -q --verify "refs/ws/${branch}" 2>/dev/null || true)
[ -n "$tip" ] || tip=$remote_sha

# The checkout whose working tree belongs to that branch: the main one, or a
# worktree the agent added under it.
work=""
if [ -n "$branch" ]; then
  [ "$(sed -n 's#^ref: refs/heads/##p' "${WORKSPACE}/.git/HEAD" 2>/dev/null || true)" != "$branch" ] || work=$WORKSPACE
  if [ -z "$work" ]; then
    for h in "${WORKSPACE}"/.git/worktrees/*/HEAD; do
      [ -f "$h" ] || continue
      [ "$(sed -n 's#^ref: refs/heads/##p' "$h")" = "$branch" ] || continue
      gd=$(head -n 1 "$(dirname "$h")/gitdir" 2>/dev/null || true)
      [ -d "$(dirname "$gd")" ] && work=$(dirname "$gd") && break
    done
  fi
else
  # No branch of the agent's: its edits sit in the checkout it started on.
  head_sha=""
  if gitb fetch -q --no-tags "file://${WORKSPACE}" HEAD 2>/dev/null; then head_sha=$(gitb rev-parse FETCH_HEAD); fi
  if [ -n "$head_sha" ]; then branch="chore/issue-${ISSUE}-agent-stopped"; tip=$head_sha; work=$WORKSPACE; fi
fi
[ -n "$branch" ] || { printf 'push=false\nreason=the agent left no branch for this issue\n'; exit 0; }

sha=$tip dirty=false
if [ -n "$work" ] && [ -n "$tip" ]; then
  tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
  # Every file the work tree has that git does not ignore, minus the ones the
  # agent deleted: the snapshot is exactly what the agent left.
  ws_ls "$work" -c -o --exclude-standard | sort -z -u > "$tmp/all"
  ws_ls "$work" -d | sort -z -u > "$tmp/gone"
  comm -z -23 "$tmp/all" "$tmp/gone" > "$tmp/keep"
  mkdir "$tmp/tree"
  (cd "$work" && tar --null -T "$tmp/keep" -cf -) | tar -xf - -C "$tmp/tree"
  export GIT_INDEX_FILE="$tmp/index"
  gitw() { git --git-dir="$BARE" --work-tree="$tmp/tree" -c core.hooksPath=/dev/null "$@"; }
  gitw add -A -f
  # main's agent config is the branch's, not the checkout's.
  read -r -a cfg <<<"$(sed -n 's/^PATHS=(\(.*\))$/\1/p' "$(dirname "$0")/agent-sync.sh")"
  [ "${#cfg[@]}" -gt 0 ] || fail "could not read the agent config paths from agent-sync.sh"
  gitw ls-files -z --cached -- "${cfg[@]}" | xargs -0 -r git --git-dir="$BARE" --work-tree="$tmp/tree" update-index --force-remove --
  gitb ls-tree -r --format='%(objectmode) %(objectname)%x09%(path)' "$tip" -- "${cfg[@]}" | gitw update-index --index-info
  tree=$(gitb write-tree)
  unset GIT_INDEX_FILE
  if [ "$tree" != "$(gitb rev-parse "${tip}^{tree}")" ]; then
    dirty=true
    sha=$(gitb commit-tree "$tree" -p "$tip" -m "wip: agent stopped before finishing, Refs #${ISSUE}")
  fi
fi
[ -n "$sha" ] || { printf 'push=false\nreason=the agent left no commits and no edits\n'; exit 0; }

ahead=$(gitb rev-list --count "${main}..${sha}")
touched=$(gitb diff --name-only "${main}...${sha}" | grep '^\.github/workflows/' || true)
[ -z "$touched" ] || fail "${branch} changes workflow files, which the agent's push may not: ${touched//$'\n'/, }"

push=true reason=""
if [ "$ahead" -eq 0 ]; then
  push=false reason="the branch has nothing beyond ${BASE_REF}"
elif [ "$sha" = "$remote_sha" ]; then
  push=false reason="the branch is already pushed"
elif [ -n "$remote_sha" ] && ! gitb merge-base --is-ancestor "$remote_sha" "$sha"; then
  push=false reason="the branch on GitHub has commits the agent's does not (no force push)"
elif [ -n "$remote_sha" ] && [ "$(gitb rev-list --no-merges --count "${remote_sha}..${sha}" "^${main}")" -eq 0 ]; then
  # agent-fix merges main into the checkout before the agent starts; a run that
  # then changed nothing is not work to save.
  push=false reason="the agent added only a merge of ${BASE_REF}"
fi
[ "$push" = false ] || gitb update-ref "refs/heads/${branch}" "$sha"
printf 'push=%s\nreason=%s\nbranch=%s\nsha=%s\nremote_sha=%s\nahead=%s\ndirty=%s\n' \
  "$push" "$reason" "$branch" "$sha" "$remote_sha" "$ahead" "$dirty"
