#!/usr/bin/env bash
# Demo: every operator resource answers to its name (#1133).
#
# A scripted, re-runnable CLI session against a live stack (the dev seed on a
# private Postgres, scripts/demo/lib/stack.sh) that never types a uuid:
#
#   1. a component by its dotted address, and an endpoint attached to it by
#      that address, read back as <component>/<endpoint>, even by the bare
#      component name where only one component of that name speaks https;
#   2. the endpoint retried by name, the audit row keyed on its uuid;
#   3. a secret rotated by name, and the same name sealed at a second owner
#      refused with both spellings that resolve, each of which then works;
#   4. a principal group, its member, a seeded file and a grant's scope, by name;
#   5. the uuid every response carries, still addressing the same rows.
#
# Usage: scripts/demo/name-addressing.sh [--skip-build]
set -euo pipefail

. "$(dirname "$0")/lib/stack.sh"
demo_stack 1133 "$@"

PANEL='hq.west.level-2.huddle.$comp.panel-1'
ROOM='hq.west.level-2.huddle'

say "1. A component by its dotted address, an endpoint as <component>/<endpoint>"
run "$OG component get '$PANEL' | jq -c '{name, label, product, path}'"
run "$OG secret create --name demo-panel-login --owner-kind platform --secret-type basic-auth --fields '{\"username\":\"admin\",\"password\":\"newtron\"}' | jq -c '{name, owner_kind, fields: [.fields[] | {name, value}]}'"
ADDR=$($OG component simulation get "$PANEL" | jq -r .address)
run "$OG endpoint create --component '$PANEL' --driver newtron-panel-web --node edge-hq --label 'Panel web API' --inputs '{\"host\":\"$ADDR\",\"port\":\"10443\",\"login\":\"demo-panel-login\"}' | jq -c '{component, name, driver, label}'"
run "$OG endpoint get '$PANEL/https' | jq -c '{component, name, label, driver}'"
echo "(36 components are named panel-1, but only the huddle's speaks https, so the bare name resolves:)"
run "$OG component list | jq '[.components[] | select(.name == \"panel-1\")] | length'"
run "$OG endpoint get panel-1/https | jq -c '{component, name, label}'"
echo "(a name no visible endpoint answers to is a 404 that names the reference:)"
run_refused "$OG endpoint get panel-1/https-admin"

say "2. Retry the endpoint by name"
run "$OG endpoint retry '$PANEL/https' | jq -c '{component, name, label}'"
echo "(the audit row keys on the endpoint's uuid, never the spelling that found it:)"
run "$OG audit-log list --resource endpoint --limit 1 | jq -c '.events[0] | {verb, resource, resource_id}'"

say "3. Rotate a secret by name"
run "$OG secret update demo-panel-login --fields '{\"password\":\"rotated\"}' | jq -c '{name, owner_kind, fields: [.fields[] | {name, value}]}'"
echo "(the same name sealed at the huddle as well, overriding the platform's down the cascade:)"
run "$OG secret create --name demo-panel-login --owner-kind location --owner $ROOM --secret-type basic-auth --fields '{\"username\":\"admin\",\"password\":\"huddle\"}' | jq -c '{name, owner_kind, owner_name}'"
run_refused "$OG secret update demo-panel-login --fields '{\"password\":\"which-one\"}'"
run "$OG secret update '\$platform/demo-panel-login' --fields '{\"password\":\"rotated-again\"}' | jq -c '{name, owner_kind}'"
run "$OG secret reveal $ROOM/demo-panel-login | jq -c ."

say "4. A group, its member, a file, and a grant's scope, by name"
run "$OG principal-group create --name night-shift --label 'Night shift' | jq -c '{name, label}'"
run "$OG principal-group member create night-shift --principal-id dev"
run "$OG principal-group member list night-shift | jq -c '[.members[] | {name, kind}]'"
run "$OG file download codec-firmware-2.1.4.txt | jq -c '{name, content_type}'"
run "$OG principal-group grant create night-shift --role viewer --scope-kind location --scope-id $ROOM | jq -c '{role, scope_kind, scope_id}'"

say "5. The uuid every response carries still addresses the same row"
EP_ID=$($OG endpoint get "$PANEL/https" | jq -r .id)
SECRET_ID=$($OG secret list | jq -r '.secrets[] | select(.name == "demo-panel-login" and .owner_kind == "location") | .id')
run "$OG endpoint get $EP_ID | jq -c '{component, name, label}'"
run "$OG secret reveal $SECRET_ID | jq -c ."
