#!/usr/bin/env bash
# Which jobs a push needs (#2597). The changes job in test.yml pipes the files a
# pull request changed to this script, with EVENT, ACTION and DRAFT from the
# event, and reads key=value lines back for its outputs:
#
#   docs   the docs checks have something to look at
#   go     Go lint and tests
#   web    web lint, typecheck and unit tests
#   gen    gen-drift (generated artifacts span Go, web and the generated docs)
#   image  the shared image build, e2e and smoke-lab
#
# A path is claimed by one area; a path no area claims counts as shared build
# config and runs everything, so a new top-level directory fails safe. A push to
# main or a release branch, a merge queue entry, a manual run, a PR marked ready
# for review, and a PR whose file list is empty (the lookup failed) run all five.
# A draft runs the unit jobs its files call for and never the image jobs.
set -euo pipefail

event=${EVENT:-pull_request}
action=${ACTION:-}
draft=${DRAFT:-false}

docs=false go=false web=false gen=false all=false n=0
while IFS= read -r f; do
  [ -n "$f" ] || continue
  n=$((n + 1))
  case "$f" in
    # Generated documentation is checked by gen-drift, so it is not docs only.
    docs/src/generated/* | docs/src/content/docs/reference/* | docs/src/content/docs/architecture/data-model.md)
      docs=true gen=true ;;
    docs/* | *.md | .github/screenshots/* | .github/ISSUE_TEMPLATE/* | .github/PULL_REQUEST_TEMPLATE* | .github/CODEOWNERS | .claude/* | LICENSE | NOTICE)
      docs=true ;;
    web/*)
      web=true ;;
    *.go | go.mod | go.sum | db/* | internal/* | cmd/* | api/* | proto/*)
      go=true ;;
    *)
      all=true ;;
  esac
done

if [ "$event" != pull_request ] || [ "$action" = ready_for_review ] || [ "$n" -eq 0 ]; then
  all=true
  draft=false
fi
if [ "$all" = true ]; then
  docs=true go=true web=true
fi
if [ "$go" = true ] || [ "$web" = true ]; then gen=true; fi
if [ "$all" = true ]; then gen=true; fi

image=false
if [ "$draft" != true ] && { [ "$all" = true ] || [ "$go" = true ] || [ "$web" = true ]; }; then
  image=true
fi

printf 'docs=%s\ngo=%s\nweb=%s\ngen=%s\nimage=%s\n' "$docs" "$go" "$web" "$gen" "$image"
