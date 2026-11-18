#!/usr/bin/env bash
# Why an agent run stopped, in one sentence for its status comment (#2175).
# Reads the job's check-run annotations (a JSON array, as
# GET /repos/{repo}/check-runs/{id}/annotations returns them) on stdin and
# prints the reason, or nothing when no annotation says. A turn cap is named
# with its number; any other failure gives the first error the job raised.
#
# Exit 0 always with the answer on stdout; 2 when the input is not an array.
set -euo pipefail

notes=$(cat)
if ! printf '%s' "$notes" | jq -e 'type == "array"' >/dev/null 2>&1; then
	echo "agent-stop-reason: input is not a JSON array" >&2
	exit 2
fi

printf '%s' "$notes" | jq -r '
	[.[] | select(.annotation_level == "failure") | .message // ""
	 | select(test("^Process completed with exit code") | not)] as $errs
	| ([$errs[] | capture("maximum number of turns \\((?<n>[0-9]+)\\)")] | first) as $cap
	| if $cap then "It used all \($cap.n) of its turns."
	  elif ($errs | length) > 0 then
	    ($errs[0] | gsub("\\s+"; " ") | if length > 200 then .[:200] + "..." else . end) as $m
	    | "It failed with: \($m)"
	  else "" end'
