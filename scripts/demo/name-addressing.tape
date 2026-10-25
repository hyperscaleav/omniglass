# Every operator resource answers to its name
#
# A component by its dotted address, an endpoint as <component>/<endpoint> (by a
# bare component name too, where only one of that name speaks telnet), the
# endpoint retried by name, a secret rotated by name and, sealed at a second
# owner, refused with the two spellings that resolve; a group, its member, a file
# and a grant's scope by name; and the uuid each answer carries, still working.
# No uuid is typed (#1133).

Type "# A component, by its dotted address"
Enter
Wait
Type `panel='hq.west.level-2.huddle.$comp.panel-1'`
Enter
Wait
Type `omniglass component get "$panel" | jq -c '{name, label, product, path}'`
Enter
Wait
Sleep 1s

Type "# Its endpoints are <component>/<endpoint name>"
Enter
Wait
Type `omniglass endpoint get "$panel/telnet" | jq -c '{component, name, label, driver}'`
Enter
Wait
Type "# 36 components are named panel-1, but only the huddle's speaks telnet, so the bare name resolves"
Enter
Wait
Type `omniglass component list | jq '[.components[] | select(.name == "panel-1")] | length'`
Enter
Wait
Type `omniglass endpoint get panel-1/telnet | jq -c '{component, name, label}'`
Enter
Wait
Sleep 2s

Hide
Type "clear"
Enter
Wait
Show
Type "# Retry it by name: the audit row keys on its uuid, never the spelling that found it"
Enter
Wait
Type `omniglass endpoint retry "$panel/telnet" | jq -c '{component, name}'`
Enter
Wait
Type `omniglass audit-log list --resource endpoint --limit 1 | jq -c '.events[0] | {verb, keyed_on_a_uuid: (.resource_id | test("^[0-9a-f-]{36}$"))}'`
Enter
Wait
Sleep 1s

Type "# A login secret, rotated by name"
Enter
Wait
Type `omniglass secret create --name huddle-panel-login --owner-kind platform --secret-type basic-auth --fields '{"username":"admin","password":"newtron"}' | jq -c '{name, owner_kind}'`
Enter
Wait
Type `omniglass secret update huddle-panel-login --fields '{"password":"rotated"}' | jq -c '{name, owner_kind}'`
Enter
Wait
Type "# Seal the same name at the huddle too: the bare name names two now, and says how to pick one"
Enter
Wait
Type `omniglass secret create --name huddle-panel-login --owner-kind location --owner hq.west.level-2.huddle --secret-type basic-auth --fields '{"username":"admin","password":"huddle"}' | jq -c '{name, owner_kind}'`
Enter
Wait
Type `omniglass secret update huddle-panel-login --fields '{"password":"which-one"}' 2>/dev/null | jq -r .detail || true`
Enter
Wait
Type `omniglass secret update '$platform/huddle-panel-login' --fields '{"password":"rotated-again"}' | jq -c '{name, owner_kind}'`
Enter
Wait
Type `omniglass secret reveal hq.west.level-2.huddle/huddle-panel-login | jq -c .fields`
Enter
Wait
Sleep 2s

Hide
Type "clear"
Enter
Wait
Show
Type "# A group, its member, a file and a grant's scope, each by name"
Enter
Wait
Type `omniglass principal-group create --name night-shift | jq -c '{name}'`
Enter
Wait
Type `omniglass principal-group member create night-shift --principal-id dev && omniglass principal-group member list night-shift | jq -c '[.members[] | .name]'`
Enter
Wait
Type `omniglass file download codec-firmware-2.1.4.txt | jq -c '{name, content_type}'`
Enter
Wait
Type `omniglass principal-group grant create night-shift --role viewer --scope-kind location --scope-id hq.west.level-2.huddle | jq -c '{role, scope_kind}'`
Enter
Wait
Sleep 1s

Type "# The uuid every answer carries still addresses the same endpoint"
Enter
Wait
Hide
Type `demo_hush 'ep=$(omniglass endpoint get "$panel/telnet" | jq -r .id)'`
Enter
Wait
Show
Type `omniglass endpoint get "$ep" | jq -c '{component, name, label}'`
Enter
Wait
Sleep 3s
