#!/usr/bin/env bash
# The project board (the org project "Omniglass") is the live status of the
# work, and this is the one way an agent moves it (#1191; the board, its fields
# and who moves what: docs/src/content/docs/contributing/labels.md). The skills
# call it at fixed points in the lifecycle.
#
#   board.sh add <issue>                put it on the board in Backlog (an item
#                                       already there keeps its Status)
#   board.sh set <issue> <status>       Backlog | Todo | In Progress | In Review | Done
#                                       (puts the issue on the board first if needed)
#   board.sh iteration <issue> current  put it in the iteration running today
#   board.sh show <issue>               print its Status, Priority and Iteration
#
# Every id (the project's, a field's, an option's, the item's) is resolved by
# name when it runs, once per run, so nobody hand-types one and a renamed option
# fails loudly instead of writing the wrong one. Every command is idempotent: a
# second run reads what the first wrote and changes nothing. It never writes
# Priority, which the architect alone sets. Until the board has an "In Review"
# status, set <issue> "In Review" lands on "In Progress" with one warning. An
# archived item is restored by add, set and iteration.
#
# The board runs workflows of its own, and two meet this script. "Item added to
# project" gives a new item a Status a moment after it is added, so when this
# script adds an issue it waits for that (up to BOARD_SETTLE seconds) before
# writing, or the workflow would overwrite the move. "Auto-close issue" closes an
# issue whose Status becomes Done, so set <issue> Done closes the issue too.
#
# Env: BOARD_OWNER (hyperscaleav), BOARD_PROJECT (Omniglass), BOARD_REPO
# (hyperscaleav/omniglass), BOARD_TODAY (YYYY-MM-DD, default today in UTC),
# BOARD_SETTLE (seconds to wait for a new item's workflow Status, default 6).
# Exit: 0 done or nothing to do, 1 an error from GitHub or the board, 2 a usage
# error. Prereqs: jq, and gh authenticated with the project scope
# (gh auth refresh -s project).
set -euo pipefail

OWNER=${BOARD_OWNER:-hyperscaleav}
PROJECT=${BOARD_PROJECT:-Omniglass}
REPO=${BOARD_REPO:-hyperscaleav/omniglass}
SETTLE=${BOARD_SETTLE:-6}

usage() {
  cat <<'EOF'
usage: board.sh add <issue>
       board.sh set <issue> <Backlog|Todo|In Progress|In Review|Done>
       board.sh iteration <issue> current
       board.sh show <issue>
EOF
}

say() { printf '%s\n' "$*"; }
warn() { printf 'board: warning: %s\n' "$*" >&2; }
die() {
  printf 'board: %s\n' "$*" >&2
  exit 1
}
bad_input() {
  printf 'board: %s\n' "$*" >&2
  exit 2
}
usage_error() {
  printf 'board: %s\n' "$1" >&2
  usage >&2
  exit 2
}

# ---- arguments -------------------------------------------------------------
# Checked before anything touches the network: a malformed call costs nothing.

cmd=${1:-}
case "$cmd" in
-h | --help | help)
  usage
  exit 0
  ;;
"") usage_error "a command is required" ;;
add | show) [ $# -eq 2 ] || usage_error "$cmd takes one issue number" ;;
set) [ $# -ge 3 ] || usage_error "set takes an issue number and a status" ;;
iteration)
  [ $# -eq 3 ] || usage_error "iteration takes an issue number and current"
  [ "$3" = current ] || usage_error "iteration takes current (the iteration running today); the architect picks any other on the board"
  ;;
*) usage_error "unknown command: $cmd" ;;
esac
issue=${2#\#}
[[ $issue =~ ^[1-9][0-9]*$ ]] || usage_error "not an issue number: $2"
want_status=${*:3}
[[ $SETTLE =~ ^[0-9]+$ ]] || bad_input "BOARD_SETTLE is not a number of seconds: $SETTLE"

command -v gh >/dev/null 2>&1 || bad_input "gh is required (https://cli.github.com)"
command -v jq >/dev/null 2>&1 || bad_input "jq is required"

# ---- gh, with its failures in words a caller can act on --------------------

ERRF=$(mktemp)
trap 'rm -f -- "$ERRF"' EXIT

# gh_run <gh args...>: gh's stdout, or the reason it failed. Run in a command
# substitution, its die ends the substitution and set -e ends the script.
gh_run() {
  if gh "$@" 2>"$ERRF"; then
    return 0
  fi
  local err
  err=$(cat -- "$ERRF")
  case "$err" in
  *"missing required scope"* | *INSUFFICIENT_SCOPES*)
    die "the gh token lacks the project scope; run: gh auth refresh -s project"
    ;;
  *"Could not resolve to an Issue"*)
    die "no issue #$issue in $REPO (the board tracks issues; a pull request number is not one)"
    ;;
  esac
  die "gh $1 $2 failed: $err"
}

# ---- names to ids, resolved once per run -----------------------------------

PROJECT_NUMBER="" PROJECT_ID="" FIELDS_JSON=""

load_project() {
  [ -z "$PROJECT_ID" ] || return 0
  local list hit
  list=$(gh_run project list --owner "$OWNER" --format json --limit 100)
  hit=$(jq -r --arg t "$PROJECT" '[.projects[] | select(.title == $t)] |
    if length == 1 then "\(.[0].number)\t\(.[0].id)" elif length > 1 then "many" else "" end' <<<"$list")
  case "$hit" in
  "") die "no project named \"$PROJECT\" under $OWNER (it has: $(jq -r '[.projects[].title] | sort | join(", ")' <<<"$list")); the token's account needs access to it" ;;
  many) die "more than one project named \"$PROJECT\" under $OWNER; set BOARD_PROJECT to a unique title" ;;
  esac
  PROJECT_NUMBER=${hit%%$'\t'*}
  PROJECT_ID=${hit#*$'\t'}
}

load_fields() {
  [ -z "$FIELDS_JSON" ] || return 0
  load_project
  FIELDS_JSON=$(gh_run project field-list "$PROJECT_NUMBER" --owner "$OWNER" --format json --limit 100)
}

field_id() {
  jq -r --arg f "$1" '[.fields[] | select(.name == $f)][0].id // empty' <<<"$FIELDS_JSON"
}

# option <field> <name>: "<option id>\t<option name>", matched ignoring case.
option() {
  jq -r --arg f "$1" --arg o "$2" '[.fields[] | select(.name == $f) | .options[]? |
    select((.name | ascii_downcase) == ($o | ascii_downcase))][0] // empty | "\(.id)\t\(.name)"' <<<"$FIELDS_JSON"
}

option_names() {
  jq -r --arg f "$1" '[.fields[] | select(.name == $f) | .options[]?.name] | join(", ")' <<<"$FIELDS_JSON"
}

# resolve_status <name>: STATUS_FIELD_ID, STATUS_OPTION_ID, STATUS_NAME.
resolve_status() {
  local hit
  STATUS_FIELD_ID=$(field_id Status)
  [ -n "$STATUS_FIELD_ID" ] || die "$PROJECT has no Status field"
  hit=$(option Status "$1")
  if [ -z "$hit" ] && [ "$(tr '[:upper:]' '[:lower:]' <<<"$1")" = "in review" ]; then
    hit=$(option Status "In Progress")
    [ -z "$hit" ] || warn "$PROJECT has no \"In Review\" status yet, so this lands on \"In Progress\" (adding the option is a one-time step in the board's settings: contributing/labels, The board)"
  fi
  if [ -z "$hit" ]; then
    local why=""
    [ -z "$(option Priority "$1")" ] || why="; $1 is a Priority, which the architect alone sets"
    bad_input "no status \"$1\" on $PROJECT (Status: $(option_names Status))$why"
  fi
  STATUS_OPTION_ID=${hit%%$'\t'*}
  STATUS_NAME=${hit#*$'\t'}
}

# ---- the issue and its item ------------------------------------------------

ISSUE_JSON="" ITEM_JSON="" ITEM_ID=""

load_item() {
  load_project
  local q
  # shellcheck disable=SC2016 # GraphQL variables, not shell ones
  q='query($owner: String!, $name: String!, $number: Int!) {
    repository(owner: $owner, name: $name) { issue(number: $number) {
      number title url
      projectItems(first: 50) { nodes { id isArchived project { id }
        fieldValues(first: 50) { nodes {
          ... on ProjectV2ItemFieldSingleSelectValue { name field { ... on ProjectV2FieldCommon { name } } }
          ... on ProjectV2ItemFieldIterationValue { title field { ... on ProjectV2FieldCommon { name } } }
        } } } }
    } } }'
  ISSUE_JSON=$(gh_run api graphql -f query="$q" -f owner="${REPO%%/*}" -f name="${REPO#*/}" -F number="$issue")
  ITEM_JSON=$(jq -c --arg p "$PROJECT_ID" \
    '[.data.repository.issue.projectItems.nodes[] | select(.project.id == $p)][0] // empty' <<<"$ISSUE_JSON")
}

# value <field>: the item's value for a single-select or iteration field.
value() {
  [ -n "$ITEM_JSON" ] || return 0
  jq -r --arg f "$1" '[.fieldValues.nodes[] | select(.field.name? == $f) | (.name // .title)][0] // empty' <<<"$ITEM_JSON"
}

# ensure_item: ITEM_ID, putting the issue on the board, or back from the
# archive, when it is not there.
ensure_item() {
  if [ -z "$ITEM_JSON" ]; then
    local url added
    url=$(jq -r '.data.repository.issue.url' <<<"$ISSUE_JSON")
    added=$(gh_run project item-add "$PROJECT_NUMBER" --owner "$OWNER" --url "$url" --format json)
    ITEM_ID=$(jq -r '.id' <<<"$added")
    say "#$issue added to $PROJECT"
    settle
    return 0
  fi
  ITEM_ID=$(jq -r '.id' <<<"$ITEM_JSON")
  if [ "$(jq -r '.isArchived' <<<"$ITEM_JSON")" = true ]; then
    gh_run project item-archive "$PROJECT_NUMBER" --owner "$OWNER" --id "$ITEM_ID" --undo >/dev/null
    say "#$issue restored from the archive"
  fi
}

# settle: wait, up to SETTLE seconds, for the board's "Item added to project"
# workflow to give the item just added its Status, re-reading the item each
# second. A move written first would be overwritten when the workflow lands; a
# board with that workflow off costs only the wait.
settle() {
  local waited=0
  while [ "$waited" -lt "$SETTLE" ]; do
    sleep 1
    waited=$((waited + 1))
    load_item
    [ -z "$(value Status)" ] || return 0
  done
}

write_status() {
  gh_run project item-edit --id "$ITEM_ID" --project-id "$PROJECT_ID" \
    --field-id "$STATUS_FIELD_ID" --single-select-option-id "$STATUS_OPTION_ID" >/dev/null
}

# ---- the commands ----------------------------------------------------------

cmd_add() {
  load_fields
  resolve_status Backlog
  load_item
  local before current
  before=$ITEM_JSON
  ensure_item
  current=$(value Status)
  if [ -n "$current" ] && [ -z "$before" ]; then
    say "#$issue: $current, from the board's own workflow: unchanged"
    return 0
  fi
  if [ -n "$current" ]; then
    say "#$issue is already on $PROJECT ($current): unchanged"
    return 0
  fi
  write_status
  say "#$issue: (none) -> $STATUS_NAME"
}

cmd_set() {
  load_fields
  resolve_status "$want_status"
  load_item
  local current
  ensure_item
  current=$(value Status)
  if [ "$current" = "$STATUS_NAME" ]; then
    say "#$issue is already $STATUS_NAME: unchanged"
    return 0
  fi
  write_status
  say "#$issue: ${current:-(none)} -> $STATUS_NAME"
}

cmd_iteration() {
  local today field config hit id title current
  today=${BOARD_TODAY:-$(date -u +%F)}
  [[ $today =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] || bad_input "BOARD_TODAY is not a date (YYYY-MM-DD): $today"
  load_fields
  field=$(field_id Iteration)
  [ -n "$field" ] || die "$PROJECT has no Iteration field"
  # shellcheck disable=SC2016 # GraphQL variables, not shell ones
  config=$(gh_run api graphql -f id="$field" -f query='query($id: ID!) { node(id: $id) {
    ... on ProjectV2IterationField { configuration { iterations { id title startDate duration } } } } }')
  # An iteration runs from its start date for duration days; today is in it
  # when start <= today < start + duration (ISO dates compare as strings).
  hit=$(jq -r --arg d "$today" '[.data.node.configuration.iterations[] |
    select(.startDate <= $d and
      $d < ((.startDate + "T00:00:00Z" | fromdateiso8601) + .duration * 86400 | strftime("%Y-%m-%d")))][0] //
    empty | "\(.id)\t\(.title)"' <<<"$config")
  [ -n "$hit" ] || die "no iteration of $PROJECT is running on $today (the architect adds iterations in the Iteration field's settings)"
  id=${hit%%$'\t'*}
  title=${hit#*$'\t'}
  load_item
  ensure_item
  current=$(value Iteration)
  if [ "$current" = "$title" ]; then
    say "#$issue is already in $title: unchanged"
    return 0
  fi
  gh_run project item-edit --id "$ITEM_ID" --project-id "$PROJECT_ID" --field-id "$field" --iteration-id "$id" >/dev/null
  say "#$issue: Iteration ${current:-(none)} -> $title"
}

cmd_show() {
  local status priority iteration
  load_item
  [ -n "$ITEM_JSON" ] || die "#$issue is not on $PROJECT (scripts/board.sh add $issue puts it there)"
  status=$(value Status)
  priority=$(value Priority)
  iteration=$(value Iteration)
  say "#$issue $(jq -r '.data.repository.issue.title' <<<"$ISSUE_JSON")"
  printf '  %-11s %s\n' "Status:" "${status:-(none)}" "Priority:" "${priority:-(none)}" "Iteration:" "${iteration:-(none)}"
  [ "$(jq -r '.isArchived' <<<"$ITEM_JSON")" != true ] || say "  (archived)"
}

"cmd_$cmd"
