#!/usr/bin/env bash
# The parallel adversarial review behind the agent-review check (#2211) with
# compact output and incremental re-review support (#2224).
#
# agent-review used to be one job running one agent over the whole sweep, and
# a large PR took 20 minutes. The sweep is now split into lenses, one Actions
# job each, run side by side; each returns its findings as JSON and posts
# nothing. One verdict job then posts a single review that carries every
# finding. This script holds both halves so they are tested in go test
# ./scripts/ rather than buried in workflow YAML:
#
#   agent-review.sh lenses DIR [DIFF]  print the lenses, as a JSON array of
#                                       {name, task}, for the anti-pattern
#                                       catalog in DIR. If DIFF (a git diff
#                                       file) is given, skip catalog and
#                                       invariant lenses when no Go or web code
#                                       is touched.
#   agent-review.sh review              read the lenses' results on stdin and
#                                       print the one review to post, as the
#                                       body of POST /repos/{o}/{r}/pulls/{n}/reviews
#   agent-review.sh turns N             print the turn limit for a diff of N
#                                       added/removed lines (scales with size)
#   agent-review.sh incremental-base    read {reviewer_bot, reviews} from stdin;
#                                       print the commit_id the reviewer bot last
#                                       reviewed (the diff base for a re-review),
#                                       or "" for a full review
#
# review reads {head_sha, expected: [lens names], lenses: [{name, summary,
# findings: [{severity, path, line, rule, title?, failure, fix}]}],
# inline (bool, default true), own_pr (bool, default false),
# open_threads ([{path, line, rule}], default [])}.
# A blocking finding, or one whose severity is not "advisory", makes the review
# REQUEST_CHANGES; otherwise it is COMMENT. It never approves. With own_pr true
# (the App posting the review also wrote the PR, and GitHub refuses Request
# changes on your own PR) a blocking review is a COMMENT too; its body still
# opens with the blocking count, which is what agent-trust.sh verdict reads.
# With inline false every finding goes in the body instead of on a line, for
# when GitHub refuses a line that is not in the diff. open_threads deduplicates:
# a new finding whose path, line and rule match an existing reviewer thread is
# dropped from this review.
#
# The workflow runs the default branch's copy, like agent-trust.sh, because
# the event this prints is the verdict that script then reads.
#
# Exit: 0 printed, 2 unreadable input or catalog, 3 an expected lens returned
# nothing, 64 bad usage. Prereqs: bash 4.4+, jq.
set -euo pipefail
shopt -s inherit_errexit

# Six shards of about 16 entries each: with three, the slowest shard of 31
# took 10 minutes on its own (PR #2213's first run).
SHARDS=6

mode=${1:-}

case "$mode" in
  lenses)
    [ $# -eq 2 ] || [ $# -eq 3 ] || { printf 'usage: agent-review.sh lenses <catalog-dir> [diff-file]\n' >&2; exit 64; }
    dir=$2
    difffile=${3:-}
    [ -d "$dir" ] || { printf 'agent-review: no catalog at %s\n' "$dir" >&2; exit 2; }
    entries=()
    while IFS= read -r f; do
      [ "$f" = README.md ] && continue
      # The names go into the lenses' prompts, so only the catalog's own
      # shape is admitted.
      [[ "$f" =~ ^[a-z0-9][a-z0-9-]*\.md$ ]] || { printf 'agent-review: unexpected catalog file %q\n' "$f" >&2; exit 2; }
      entries+=("$f")
    done < <(find "$dir" -maxdepth 1 -type f -name '*.md' -printf '%f\n' | LC_ALL=C sort)
    [ ${#entries[@]} -gt 0 ] || { printf 'agent-review: the catalog at %s is empty\n' "$dir" >&2; exit 2; }

    # Determine which lens types to include. Without a diff, run everything.
    # A diff with no Go (.go) or web (.tsx/.ts/.js/.jsx) code skips catalog
    # and invariants (they check Go/web anti-patterns) and runs neighbor only.
    has_code=true
    if [ -n "$difffile" ] && [ -f "$difffile" ]; then
      has_code=false
      if grep -qE '^[+]{3} b/.*\.(go|tsx|ts|js|jsx)$' "$difffile" 2>/dev/null; then has_code=true; fi
    fi

    if ! $has_code; then
      # Docs-only or no Go/web code: neighbor only.
      printf '[{"name":"neighbor","task":"Run the neighbor check (procedure step 5): for every behavior the diff flips, grep the docs tree for the claims it falsifies, and check that the docs the diff touches agree with the code. The anti-pattern catalog and the invariants belong to other lenses."}]\n'
      exit 0
    fi

    printf '%s\n' "${entries[@]}" | jq -R -s -c --argjson shards "$SHARDS" '
      split("\n") | map(select(length > 0)) as $all
      | ($all | length) as $n
      | [range(0; $shards) as $i
          | $all[($i * $n / $shards | floor):(($i + 1) * $n / $shards | floor)]
          | select(length > 0)]
      | to_entries
      | map({
          name: "catalog-\(.key + 1)",
          task: ("Sweep every hunk against these anti-pattern catalog entries, and only these "
            + "(procedure step 2), each in .claude/skills/adversarial-review/references/anti-patterns/: "
            + (.value | join(", ")) + ". The other entries, the invariants and the neighbor "
            + "check belong to other lenses.")
        })
      + [
          {name: "invariants",
           task: ("Interrogate the diff against the invariants (procedure step 3): every route "
            + "gated, every applicable query scoped, every privileged mutation audited "
            + "in-transaction, migrations idempotent and never edited, no fixed test ports, "
            + "tests prove behavior fresh. Load the solidjs skill before judging anything under "
            + "web/src. The anti-pattern catalog and the neighbor check belong to other lenses.")},
          {name: "neighbor",
           task: ("Run the neighbor check (procedure step 5): for every behavior the diff flips, "
            + "grep the docs tree for the claims it falsifies, and check that the docs the diff "
            + "touches agree with the code. The anti-pattern catalog and the invariants belong "
            + "to other lenses.")}
        ]'
    ;;

  review)
    [ $# -eq 1 ] || { printf 'usage: agent-review.sh review < results.json\n' >&2; exit 64; }
    facts=$(cat)
    if ! printf '%s' "$facts" | jq -e 'type == "object" and (.head_sha | type == "string") and (.head_sha | length > 0) and (.expected | type == "array") and ((.lenses // []) | type == "array")' >/dev/null 2>&1; then
      printf 'agent-review: stdin is not a lens result set\n' >&2
      exit 2
    fi
    missing=$(printf '%s' "$facts" | jq -r '[(.lenses // [])[].name] as $got | [.expected[] | select(. as $e | $got | index($e) | not)] | join(", ")')
    if [ -n "$missing" ]; then
      printf 'agent-review: no result from lens %s\n' "$missing" >&2
      exit 3
    fi
    printf '%s' "$facts" | jq -c '
      def cut($n): tostring | if length > $n then .[:$n] + " [cut]" else . end;
      def blocking: (.severity // "") != "advisory";
      def title_of: (.title // .rule // "finding" | cut(80));
      def onaline: (.path | type == "string") and (.path | length > 0)
        and (.line | type == "number") and .line >= 1 and (.line | floor) == .line;
      def where_short:
        if onaline then "`\(.path | cut(300)):\(.line)`"
        elif (.path | type == "string") and (.path | length > 0) then "`\(.path | cut(300))`"
        else "no file" end;
      def inline_body:
        "**\(title_of)**\n\n"
        + "<details>\n<summary>Details (\(.lens | cut(40)))</summary>\n\n"
        + "**Rule:** \(.rule // "?" | cut(200))\n\n"
        + "**Failure:** \(.failure // "" | cut(3000))\n\n"
        + "**Fix:** \(.fix // "" | cut(1500))\n\n"
        + "</details>";
      def detail_entry:
        "**\(if blocking then "❌" else "⚠️" end) \(where_short) \(title_of)**"
        + " · \(.lens | cut(40))\n\n"
        + "**Failure:** \(.failure // "" | cut(3000))\n\n"
        + "**Fix:** \(.fix // "" | cut(1500))";

      (.inline != false) as $inline
      | (.own_pr == true) as $own
      | (.open_threads // []) as $open

      # Flatten all findings with the lens name attached.
      | [.lenses[] | .name as $l | (.findings // [])[] | select(type == "object") | . + {lens: $l}] as $raw_all

      # Drop findings that match an existing reviewer thread (same path, line and rule).
      | [$raw_all[] | . as $f |
          if ($open | map(select(
              .path == $f.path and
              ((.rule // "") == ($f.rule // "")) and
              ((.line // 0) == ($f.line // 0))
            )) | length > 0)
          then empty else . end] as $all

      | ($all | map(select(blocking)) | length) as $b
      | ($all | length - $b) as $a
      | ($all | map(select($inline and onaline))) as $lined
      | ($all | map(select(($inline and onaline) | not))) as $loose

      # Lens strip: use surviving (deduplicated) findings per lens.
      | (.expected | map(. as $lname |
          ($all | map(select(.lens == $lname))) as $surviving
          | (($surviving | map(select(blocking)) | length) > 0) as $has_blocking
          | (($surviving | length) > 0) as $has_advisory
          | if $has_blocking then "❌ \($lname | cut(40))"
            elif $has_advisory then "⚠️ \($lname | cut(40))"
            else "✅ \($lname | cut(40))" end
        ) | join(" · ")) as $lens_strip

      # Verdict line: opens with ❌ for blocking so agent-trust.sh can read the
      # count on its own App PRs (where the event is COMMENT not REQUEST_CHANGES).
      | (if ($all | length) == 0 then "✅ Ship · nothing found"
         elif $b > 0 then "❌ Changes requested · \($b) blocking · \($a) advisory"
         else "⚠️ \($a) advisory" end) as $verdict

      # Per-lens summaries and full finding details go in a collapsed block.
      | ([.lenses[] | "**\(.name | cut(40)):** \(.summary // "no summary" | cut(500))"] | join("\n\n")) as $lens_notes
      | ([$all[] | detail_entry] | join("\n\n---\n\n")) as $finding_details
      | ("<details>\n<summary>Detail</summary>\n\n"
         + $lens_notes
         + (if ($all | length) > 0 then "\n\n---\n\n" + $finding_details else "" end)
         + "\n\n</details>") as $details_block

      # One-liner per loose finding (those not placed inline).
      | ([$loose[] | (if blocking then "❌" else "⚠️" end) + " " + where_short + " " + title_of] | join("\n")) as $loose_lines

      | {
        commit_id: .head_sha,
        event: (if $b > 0 and ($own | not) then "REQUEST_CHANGES" else "COMMENT" end),
        body: (
          $verdict + "\n\n"
          + $lens_strip
          + (if ($loose | length) > 0 then "\n\n" + $loose_lines else "" end)
          + "\n\n" + $details_block
        ),
        comments: [$lined[] | {path, line, side: "RIGHT", body: inline_body}]
      }'
    ;;

  turns)
    [ $# -eq 2 ] || { printf 'usage: agent-review.sh turns <diff-lines>\n' >&2; exit 64; }
    n=$2
    [[ "$n" =~ ^[0-9]+$ ]] || { printf 'agent-review: turns: expected a non-negative integer, got %q\n' "$n" >&2; exit 2; }
    if [ "$n" -le 100 ]; then
      echo 40
    elif [ "$n" -le 500 ]; then
      echo 60
    elif [ "$n" -le 2000 ]; then
      echo 80
    else
      echo 120
    fi
    ;;

  incremental-base)
    [ $# -eq 1 ] || { printf 'usage: agent-review.sh incremental-base < reviews.json\n' >&2; exit 64; }
    facts=$(cat)
    if ! printf '%s' "$facts" | jq -e 'type == "object" and (.reviewer_bot | type == "string") and (.reviews | type == "array")' >/dev/null 2>&1; then
      printf 'agent-review: stdin is not a valid reviews object\n' >&2
      exit 2
    fi
    # Return the commit_id of the most recent review by the reviewer bot, or "".
    # The workflow checks that the commit is an ancestor of HEAD before using it.
    printf '%s' "$facts" | jq -r '
      (.reviewer_bot | ascii_downcase) as $bot
      | [.reviews[] | select((.user // "" | ascii_downcase) == $bot)]
      | if length == 0 then ""
        else last.commit_id // "" end'
    ;;

  *)
    printf 'usage: agent-review.sh lenses <catalog-dir> [diff-file] | review < results.json | turns <n> | incremental-base < reviews.json\n' >&2
    exit 64
    ;;
esac
