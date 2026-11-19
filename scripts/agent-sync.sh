#!/usr/bin/env bash
# Keep an agent's branch current with main and keep main's agent config out of
# its diff (#2232, #2231).
#
# claude-code-action restores CLAUDE.md, .claude/ and the other paths below
# from the base branch into the work tree before an agent runs on a PR, and
# leaves them unstaged (anthropics/claude-code-action,
# src/github/operations/restore-config.ts, SENSITIVE_PATHS). On a branch behind
# main that does two things: `git merge origin/main` refuses to run over those
# dirty files, and an agent that "cleans up" commits main's copies as the
# branch's own change (#1993's f1ab4652, #1948's 3114cabd). So the workflow
# merges main before the action runs, and checks the diff after it.
#
#   agent-sync.sh merge [BASE]   merge BASE (default origin/main) into HEAD,
#                                never rebasing. Prints "current", "merged", or
#                                "conflict" then each conflicted path, leaving
#                                the markers for the agent to resolve
#   agent-sync.sh guard [BASE]   print each agent-config path the branch
#                                changes against its merge base with BASE
#   agent-sync.sh drop  [BASE]   put those paths back as they are at the merge
#                                base and commit that, so the PR's diff no
#                                longer touches them; prints the paths
#
# guard is a gate only for an agent run: it fails when AGENT_RUN is 1 and
# AGENT_CONFIG_OK is not, and on a lessons/ branch it never fails (the daily
# sweep edits the agents' instructions on purpose, #2235). Anywhere else it
# prints what it found and passes, so a person editing CLAUDE.md is not stopped.
#
# Exit: merge 0 current or merged, 3 conflict; guard 0 clean or not a gate,
# 1 the branch changes agent config; drop 0; 2 a dirty work tree or an unknown
# BASE; 64 bad usage. Prereqs: bash 4.4+, git.
set -euo pipefail
shopt -s inherit_errexit

# The action's SENSITIVE_PATHS, as pathspecs. Keep in step with the action
# version the agent workflows pin.
PATHS=(CLAUDE.md CLAUDE.local.md AGENTS.md .claude .mcp.json .claude.json .gitmodules .ripgreprc .husky)

mode=${1:-}
base=${2:-origin/main}
case "$mode" in merge | guard | drop) ;; *)
  printf 'usage: agent-sync.sh merge|guard|drop [BASE]\n' >&2
  exit 64
  ;;
esac
[ $# -le 2 ] || { printf 'usage: agent-sync.sh merge|guard|drop [BASE]\n' >&2; exit 64; }

git rev-parse --verify --quiet "${base}^{commit}" >/dev/null \
  || { printf 'agent-sync: unknown base %q (fetch it first)\n' "$base" >&2; exit 2; }

clean_tree() {
  [ -z "$(git status --porcelain --untracked-files=no)" ] \
    || { printf 'agent-sync: the work tree has uncommitted changes; %s needs a clean tree\n' "$mode" >&2; exit 2; }
}

changed() {
  local mb
  mb=$(git merge-base "$base" HEAD)
  git diff --name-only "$mb" HEAD -- "${PATHS[@]}"
}

case "$mode" in
  merge)
    clean_tree
    if git merge-base --is-ancestor "$base" HEAD; then
      echo current
      exit 0
    fi
    if git merge --no-edit --no-ff "$base" >/dev/null 2>&1; then
      echo merged
      exit 0
    fi
    conflicts=$(git diff --name-only --diff-filter=U)
    if [ -z "$conflicts" ]; then
      printf 'agent-sync: git merge %s failed with no conflicted path\n' "$base" >&2
      git merge --abort 2>/dev/null || true
      exit 2
    fi
    echo conflict
    printf '%s\n' "$conflicts"
    exit 3
    ;;

  guard)
    found=$(changed)
    [ -n "$found" ] || exit 0
    printf '%s\n' "$found"
    branch=$(git symbolic-ref --quiet --short HEAD || true)
    if [ "${AGENT_RUN:-}" = 1 ] && [ "${AGENT_CONFIG_OK:-}" != 1 ] && [[ "$branch" != lessons/* ]]; then
      printf "agent-sync: this branch changes the agents' own config (above). An agent's PR leaves it as main has it; run scripts/agent-sync.sh drop, or merge %s if the change came from there.\n" "$base" >&2
      exit 1
    fi
    printf 'agent-sync: the branch changes agent config (above); not a gate outside an agent run\n' >&2
    exit 0
    ;;

  drop)
    clean_tree
    mb=$(git merge-base "$base" HEAD)
    found=$(changed)
    [ -n "$found" ] || exit 0
    while IFS= read -r path; do
      if git cat-file -e "${mb}:${path}" 2>/dev/null; then
        git checkout "$mb" -- "$path"
      else
        git rm -q --cached -- "$path"
        rm -f -- "$path"
      fi
    done <<<"$found"
    git commit -q -m "chore: keep main's agent config out of this PR" \
      -m "These paths are main's to change; claude-code-action puts main's copies in the work tree, and they had been committed here:" \
      -m "$found"
    printf '%s\n' "$found"
    ;;
esac
