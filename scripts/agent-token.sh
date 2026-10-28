#!/usr/bin/env bash
# Agent sessions act on GitHub as their own identity, the omniglass-agent GitHub
# App, not as the architect (#1281). This script is the one place that identity
# is minted and wired: it signs a JWT with the App's private key, trades it for
# an installation token scoped to this repo, caches the token until it is close
# to expiry, and hands it to git and gh.
#
#   agent-token.sh token            print a valid installation token (cached;
#                                   re-minted when under AGENT_TOKEN_MIN_TTL
#                                   seconds remain)
#   agent-token.sh git-credential get
#                                   the git credential-helper protocol: answers
#                                   github.com with the token, anything else with
#                                   nothing (store and erase are no-ops)
#   agent-token.sh setup            wire the current clone: git author is the
#                                   App's bot user, and the only credential
#                                   helper for github.com is this script
#   agent-token.sh whoami           print the bot login and the repos the token
#                                   reaches, one per line
#
# gh is wired by putting scripts/agent-bin first on PATH: its gh shim mints a
# token per call, so a session longer than a token's hour never goes stale.
#
# This only works where the caller's credential reaches GitHub unchanged. The
# cloud agent sandbox's egress proxy replaces any GitHub credential with the
# connected account's (tested on #1281), so agent identity is a self-hosted
# worker's property; in the cloud sandbox a session is the architect.
#
# Env: AGENT_APP_ID (required), AGENT_APP_KEY (required: path to the App's PEM
# private key), AGENT_REPO (hyperscaleav/omniglass), AGENT_INSTALLATION_ID
# (resolved from AGENT_REPO when unset), AGENT_TOKEN_CACHE
# ($XDG_CACHE_HOME/omniglass-agent/token.json), AGENT_TOKEN_MIN_TTL (600),
# AGENT_API (https://api.github.com).
# Exit: 0 done, 1 an error from GitHub or the local tools, 2 a usage or
# configuration error. Prereqs: openssl, curl, jq.
set -euo pipefail
# A failure inside a command substitution must abort the substitution too, or
# a JWT that failed to sign would still be sent.
shopt -s inherit_errexit

REPO=${AGENT_REPO:-hyperscaleav/omniglass}
API=${AGENT_API:-https://api.github.com}
MIN_TTL=${AGENT_TOKEN_MIN_TTL:-600}
CACHE=${AGENT_TOKEN_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/omniglass-agent/token.json}
SELF="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"

die() {
  printf 'agent-token: %s\n' "$*" >&2
  exit 1
}
bad_input() {
  printf 'agent-token: %s\n' "$*" >&2
  exit 2
}
usage() {
  cat >&2 <<'EOF'
usage: agent-token.sh token
       agent-token.sh git-credential <get|store|erase>
       agent-token.sh setup
       agent-token.sh whoami
EOF
  exit 2
}

require_config() {
  [ -n "${AGENT_APP_ID:-}" ] || bad_input "AGENT_APP_ID is not set: the omniglass-agent App's id (App settings, About)"
  [[ "$AGENT_APP_ID" =~ ^[0-9]+$ ]] || bad_input "AGENT_APP_ID must be a number, got '$AGENT_APP_ID'"
  [ -n "${AGENT_APP_KEY:-}" ] || bad_input "AGENT_APP_KEY is not set: the path to the App's PEM private key"
  [ -r "$AGENT_APP_KEY" ] || bad_input "AGENT_APP_KEY '$AGENT_APP_KEY' is not a readable file"
  [[ "$MIN_TTL" =~ ^[0-9]+$ ]] || bad_input "AGENT_TOKEN_MIN_TTL must be a whole number of seconds, got '$MIN_TTL'"
  [[ "$REPO" == */* ]] || bad_input "AGENT_REPO must be owner/name, got '$REPO'"
  local tool
  for tool in openssl curl jq; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is not on PATH"
  done
}

b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }

# app_jwt prints a JWT the App signs: issued a minute in the past to absorb
# clock skew, valid nine minutes (GitHub's ceiling is ten).
app_jwt() {
  local now header payload unsigned sig
  now=$(date +%s)
  header=$(printf '{"alg":"RS256","typ":"JWT"}' | b64url)
  payload=$(printf '{"iat":%d,"exp":%d,"iss":"%s"}' $((now - 60)) $((now + 540)) "$AGENT_APP_ID" | b64url)
  unsigned="$header.$payload"
  sig=$(printf '%s' "$unsigned" | openssl dgst -sha256 -sign "$AGENT_APP_KEY" -binary | b64url) ||
    die "could not sign with AGENT_APP_KEY (is it the App's PEM private key?)"
  [ -n "$sig" ] || die "could not sign with AGENT_APP_KEY (is it the App's PEM private key?)"
  printf '%s' "$unsigned.$sig"
}

# api METHOD PATH AUTH [BODY] prints the response body; a non-2xx is fatal and
# names GitHub's message.
api() {
  local method=$1 path=$2 auth=$3 body=${4:-} out code
  local args=(-sS -X "$method" -H "Accept: application/vnd.github+json"
    -H "X-GitHub-Api-Version: 2022-11-28" -H "Authorization: Bearer $auth"
    -w '\n%{http_code}')
  [ -n "$body" ] && args+=(-H "Content-Type: application/json" --data "$body")
  out=$(curl "${args[@]}" "$API$path") || die "$method $path: the request failed"
  code=${out##*$'\n'}
  out=${out%$'\n'*}
  if [[ ! "$code" =~ ^2 ]]; then
    die "$method $path: HTTP $code: $(printf '%s' "$out" | jq -r '.message // empty' 2>/dev/null || true)"
  fi
  printf '%s' "$out"
}

installation_id() {
  if [ -n "${AGENT_INSTALLATION_ID:-}" ]; then
    printf '%s' "$AGENT_INSTALLATION_ID"
    return
  fi
  local jwt
  jwt=$(app_jwt)
  api GET "/repos/$REPO/installation" "$jwt" | jq -er '.id' ||
    die "the App is not installed on $REPO"
}

# cached prints the cached token when it has more than MIN_TTL seconds left.
cached() {
  [ -r "$CACHE" ] || return 1
  local tok exp
  tok=$(jq -er '.token' "$CACHE" 2>/dev/null) || return 1
  exp=$(jq -er '.expires_at' "$CACHE" 2>/dev/null) || return 1
  [ $((exp - $(date +%s))) -gt "$MIN_TTL" ] || return 1
  printf '%s' "$tok"
}

# mint trades the App's JWT for an installation token narrowed to this one
# repo, so a wider installation still yields a token that reaches only it.
mint() {
  local id jwt resp tok exp dir tmp
  id=$(installation_id)
  jwt=$(app_jwt)
  resp=$(api POST "/app/installations/$id/access_tokens" "$jwt" \
    "$(jq -nc --arg r "${REPO#*/}" '{repositories: [$r]}')")
  tok=$(printf '%s' "$resp" | jq -er '.token') || die "GitHub answered without a token"
  exp=$(printf '%s' "$resp" | jq -er '.expires_at | fromdateiso8601') || die "GitHub answered without an expiry"
  dir=$(dirname "$CACHE")
  (umask 077 && mkdir -p "$dir")
  tmp=$(umask 077 && mktemp "$dir/.token.XXXXXX")
  jq -nc --arg t "$tok" --argjson e "$exp" '{token: $t, expires_at: $e}' >"$tmp"
  mv -f "$tmp" "$CACHE"
  printf '%s' "$tok"
}

token() { cached || mint; }

git_credential() {
  case "${1:-}" in
    get) ;;
    store | erase)
      cat >/dev/null
      return 0
      ;;
    *) usage ;;
  esac
  local line host="" protocol=""
  while IFS= read -r line && [ -n "$line" ]; do
    case "$line" in
      host=*) host=${line#host=} ;;
      protocol=*) protocol=${line#protocol=} ;;
    esac
  done
  [ "$protocol" = https ] && [ "$host" = github.com ] || return 0
  local tok
  tok=$(token)
  printf 'username=x-access-token\npassword=%s\n' "$tok"
}

# bot_identity prints "<slug>[bot] <user id>": the commit author GitHub links
# to the App.
bot_identity() {
  local jwt tok slug uid
  jwt=$(app_jwt)
  tok=$(token)
  slug=$(api GET /app "$jwt" | jq -er '.slug') || die "GitHub answered /app without a slug"
  uid=$(api GET "/users/$slug%5Bbot%5D" "$tok" | jq -er '.id') || die "no bot user for $slug"
  printf '%s[bot] %s' "$slug" "$uid"
}

setup() {
  git rev-parse --git-dir >/dev/null 2>&1 || bad_input "setup runs inside the clone it wires"
  local who login uid
  who=$(bot_identity)
  login=${who% *}
  uid=${who##* }
  git config --local user.name "$login"
  git config --local user.email "$uid+$login@users.noreply.github.com"
  # The empty first entry clears every inherited helper (gh's, a keychain's)
  # for github.com, so the architect's credential can never answer instead.
  git config --local --unset-all credential.https://github.com.helper 2>/dev/null || true
  git config --local --add credential.https://github.com.helper ""
  git config --local --add credential.https://github.com.helper "!'$SELF' git-credential"
  printf 'git author: %s <%s+%s@users.noreply.github.com>\n' "$login" "$uid" "$login"
  printf 'credential helper: %s git-credential\n' "$SELF"
}

whoami() {
  local tok who
  tok=$(token)
  who=$(bot_identity)
  printf '%s\n' "${who% *}"
  api GET /installation/repositories "$tok" | jq -r '.repositories[].full_name'
}

[ $# -ge 1 ] || usage
cmd=$1
shift
case "$cmd" in
  token)
    [ $# -eq 0 ] || usage
    require_config
    token
    printf '\n'
    ;;
  git-credential)
    [ $# -eq 1 ] || usage
    require_config
    git_credential "$1"
    ;;
  setup)
    [ $# -eq 0 ] || usage
    require_config
    setup
    ;;
  whoami)
    [ $# -eq 0 ] || usage
    require_config
    whoami
    ;;
  *) usage ;;
esac
