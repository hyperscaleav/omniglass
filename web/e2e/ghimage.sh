#!/usr/bin/env bash
# Upload images to GitHub user-attachments and print markdown refs, using the
# GitHub session cookie. Reads GH_SESSION_TOKEN if set, else ~/.config/gh-image/token
# (the host-chrome user_session value, copied from DevTools, since WSL cannot decrypt
# Windows Chrome cookies). Usage: e2e/ghimage.sh [--repo owner/repo] <image>...
set -euo pipefail
tok="${GH_SESSION_TOKEN:-$(cat "$HOME/.config/gh-image/token" 2>/dev/null || true)}"
[ -n "$tok" ] || { echo "no token: paste host-chrome github.com user_session into ~/.config/gh-image/token" >&2; exit 1; }
GH_SESSION_TOKEN="$tok" gh image "$@"
