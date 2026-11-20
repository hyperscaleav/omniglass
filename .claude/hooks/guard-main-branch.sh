#!/usr/bin/env bash
# PreToolUse hook: block git writes on main and force-pushes to main (#2188).
#
# Claude Code passes the pending tool call as JSON on stdin:
#   { "tool_name": "Bash", "tool_input": { "command": "..." } }
#
# Exit 2 to block the tool call; exit 0 to allow it.

set -u

input=$(cat)

# Extract the command string from the JSON.  Python 3 handles quoting and
# escapes correctly; if it is not available fall through and allow.
command=$(python3 -c "
import json, sys
try:
    d = json.loads(sys.stdin.read())
    print(d.get('tool_input', {}).get('command', ''))
except Exception:
    pass
" <<< "$input" 2>/dev/null) || command=""

if [ -z "$command" ]; then
  exit 0
fi

# Only intercept git subcommands that write state.
case "$command" in
  git\ commit*|git\ merge*|git\ rebase*|git\ push*)
    ;;
  *)
    exit 0
    ;;
esac

# Read the current branch; if git is unavailable or we are not in a repo, allow.
branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null) || exit 0

case "$command" in
  git\ commit*|git\ merge*|git\ rebase*)
    if [ "$branch" = "main" ]; then
      printf 'Blocked: "%s" is not allowed on main.\nCreate a feature branch first (see the slice workflow in CLAUDE.md).\n' "$command" >&2
      exit 2
    fi
    ;;
  git\ push*)
    if [ "$branch" = "main" ]; then
      printf 'Blocked: push from main is not allowed.\nCreate a feature branch first (see the slice workflow in CLAUDE.md).\n' >&2
      exit 2
    fi
    # Normalize away shell quoting so "main" and 'main' are caught.
    command_clean=$(printf '%s' "$command" | tr -d "\"'")
    # Block force pushes targeting main from any branch.  main must be a whole
    # refspec side so feature/main-nav or main-rewrite are not mistaken for it.
    # Form 1: explicit force flag (-f, a combined short flag such as -fu,
    #          --force, --force-with-lease) with main as a refspec destination.
    if echo "$command_clean" | grep -qE '(--force|--force-with-lease|(^|[[:space:]])-[[:alpha:]]*f[[:alpha:]]*($|[[:space:]]))' && \
       echo "$command_clean" | grep -qE '(^|[[:space:]+:])(refs/heads/)?main($|[[:space:]])'; then
      printf 'Blocked: force push to main is not allowed (see CLAUDE.md).\n' >&2
      exit 2
    fi
    # Form 2: + prefix on the refspec itself (git push origin +main or
    #          +HEAD:main), which forces without an explicit flag.
    if echo "$command_clean" | grep -qE '(^|[[:space:]])\+([^[:space:]:]+:)?(refs/heads/)?main($|[[:space:]])'; then
      printf 'Blocked: force push to main is not allowed (see CLAUDE.md).\n' >&2
      exit 2
    fi
    ;;
esac

exit 0
