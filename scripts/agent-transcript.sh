#!/usr/bin/env bash
# What an agent run did, from claude-code-action's transcript (#2230): the
# workflows keep it as an artifact so the daily lessons sweep (#2235) can tell
# a miss (the agent never ran make preflight) from a flake.
#
#   agent-transcript.sh redact   FILE  print the transcript safe to keep on a
#                                      public repo (its artifacts are public
#                                      too): each tool result cut to its exit
#                                      code line, and secrets masked
#   agent-transcript.sh commands FILE  print {commands, preflight, make_test}:
#                                      each shell command the agent ran with
#                                      its exit code (null when the run ended
#                                      before it returned), then whether make
#                                      preflight and make test ran and the
#                                      last exit code of each
#
# FILE is the action's execution_file: a JSON array of the session's messages
# (one message per line is read too). An exit code comes from the tool result:
# "Exit code N" on an error, 1 for an error without one, else 0; a command
# piped into another reports the pipeline's code, as the shell did.
#
# Secrets are masked in both modes, by value first: each of the run's own
# secret variables that is set (SECRET_VARS), with its base64 and the base64 of
# the "x-access-token:" basic auth git's credential config carries. Then by
# shape, for what the run did not hand us: private keys, GitHub and Anthropic
# tokens, JWTs, Authorization headers and credentials in a URL. No list of
# shapes is complete, which is why redact keeps no tool output at all.
#
# Exit 2 for a missing, empty or unreadable transcript, 64 for bad usage. The
# workflows warn and go on when this fails: a run never fails for want of it.
set -euo pipefail

usage() { printf 'usage: agent-transcript.sh redact|commands FILE\n' >&2; exit 64; }
[ $# -eq 2 ] || usage
mode=$1 file=$2
case "$mode" in redact | commands) ;; *) usage ;; esac
[ -s "$file" ] || { printf 'agent-transcript: no transcript at %s\n' "$file" >&2; exit 2; }

# Slurp, so an array and one message per line read the same.
messages() {
  jq -s 'if length == 1 and (.[0] | type) == "array" then .[0] else . end' "$file" 2>/dev/null \
    || { printf 'agent-transcript: %s is not JSON\n' "$file" >&2; exit 2; }
}

SECRET_VARS="GITHUB_TOKEN GH_TOKEN CLAUDE_CODE_OAUTH_TOKEN ANTHROPIC_API_KEY ACTIONS_RUNTIME_TOKEN ACTIONS_ID_TOKEN_REQUEST_TOKEN"

# The exact strings to mask, as a JSON array. Short values are skipped: an
# eight character secret is not one, and masking it would shred the text.
secret_values() {
  local name value
  for name in $SECRET_VARS; do
    value=${!name:-}
    [ "${#value}" -ge 8 ] || continue
    # Longest first: the base64 of the bare value is a suffix of the header's.
    printf '%s\n' "$(printf 'x-access-token:%s' "$value" | base64 | tr -d '\n')" \
      "$(printf '%s' "$value" | base64 | tr -d '\n')" \
      "$value"
  done | jq -R . | jq -s .
}

mask='walk(if type == "string" then
    reduce $secrets[] as $s (.; split($s) | join("[redacted secret]"))
    | gsub("-----BEGIN [A-Z ]*PRIVATE KEY-----[\\s\\S]*?-----END [A-Z ]*PRIVATE KEY-----"; "[redacted private key]")
    | gsub("gh[pousr]_[A-Za-z0-9]{20,}"; "[redacted token]")
    | gsub("github_pat_[A-Za-z0-9_]{20,}"; "[redacted token]")
    | gsub("sk-ant-[A-Za-z0-9_-]{20,}"; "[redacted token]")
    | gsub("eyJ[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+"; "[redacted token]")
    | gsub("(?<h>authorization:\\s*(basic|bearer|token)\\s+)[^\\s\"\u0027]+"; "\(.h)[redacted token]"; "i")
    | gsub("(?<s>[a-z][a-z0-9+.-]*://)[^/@\\s\"\u0027]+:[^/@\\s\"\u0027]+@"; "\(.s)[redacted credentials]@"; "i")
  else . end)'

# A tool result keeps its exit code line, which the command list reads, and
# nothing else.
withhold='map(if .type == "user" and (.message.content | type) == "array" then
    .message.content |= map(if type == "object" and .type == "tool_result" then
      .content |= ((if type == "string" then . elif type == "array" then map(.text? // "") | join("\n") else "" end)
        | (capture("^(?<line>Exit code [0-9]+)") | .line + "\n") // ""
        | . + "[output withheld: public artifact]")
    else . end)
  else . end)'

all=$(messages)
secrets=$(secret_values)

case "$mode" in
  redact)
    printf '%s' "$all" | jq --argjson secrets "$secrets" "$withhold | $mask"
    ;;

  commands)
    printf '%s' "$all" | jq --argjson secrets "$secrets" "$mask"' as $m
      | def blocks: .message.content | if type == "array" then .[] else empty end;
        def text: if type == "string" then . elif type == "array" then map(.text? // "") | join("\n") else "" end;
        def code:
          if . == null then null
          elif .is_error == true then ((.content | text | capture("^Exit code (?<n>[0-9]+)") | .n | tonumber) // 1)
          else 0 end;
        def last_of($re): [.[] | select(.command | test($re))] | last
          | {ran: (. != null), exit_code: (if . == null then null else .exit_code end)};
      ([$m[] | select(.type == "user") | blocks | select(type == "object" and .type == "tool_result")
        | {key: .tool_use_id, value: .}] | from_entries) as $results
      | [$m[] | select(.type == "assistant") | blocks
          | select(type == "object" and .type == "tool_use" and .name == "Bash")
          | {command: (.input.command // ""), exit_code: ($results[.id] | code)}] as $cmds
      | {commands: $cmds,
         preflight: ($cmds | last_of("\\bmake preflight\\b")),
         make_test: ($cmds | last_of("\\bmake test($|[^-])"))}'
    ;;
esac
