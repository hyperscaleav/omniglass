# A fire-and-forget command reads settled only once its node runs it
#
# A reboot has no value to settle against, so its node's execution report decides it (#1630).
# A simulated Kestrel bar in the depot's Bay 1, attached by a custom driver that binds reboot
# to the bar's admin route: the reboot reads issued and queued, then settled once edge-hq
# carries it out. A second bar holding a stale login refuses it: failed, its refusal the reason.

Type "# A custom driver: the shipped Kestrel API driver, plus the bar's admin reboot route"
Enter
Wait
Type `omniglass driver create --name kestrel-admin --label 'Kestrel API with admin verbs' --version 1.0.0 --spec "$(omniglass driver get kestrel-api | jq -c '.spec | .commands += [{command_type: "reboot", request: {method: "POST", path: "/api/admin/reboot"}}]')" | jq -c '{name, commands: [.spec.commands[].command_type]}'`
Enter
Wait
Type `bar='depot.bay-1.$comp.videobar-1' stale='depot.bay-1.$comp.videobar-2'`
Enter
Wait

Hide
Type `demo_hush 'inputs=$(omniglass endpoint get "$bar/https" | jq -c .inputs)'`
Enter
Wait
Type `demo_hush 'omniglass endpoint delete "$bar/https"'`
Enter
Wait
Type `demo_hush 'omniglass endpoint create --component "$bar" --driver kestrel-admin --node edge-hq --inputs "$inputs"'`
Enter
Wait
Type "clear"
Enter
Wait
Show

Type "# A reboot has nothing to settle against: it reads issued, queued for the node"
Enter
Wait
Type `omniglass component command issue "$bar" --command-type reboot | jq -c '{status, phase, settle_mode, endpoint, node}'`
Enter
Wait

Hide
Type `demo_until 60 'omniglass component command list "$bar" --limit 1 | jq -e ".commands[0].status == \"settled\""'`
Enter
Wait@70s
Show

Type "# The node ran it and reported: settled by that report, and on the bar's timeline"
Enter
Wait
Type `omniglass component command list "$bar" --limit 1 | jq -c '.commands[0] | {status, settled_by, reason}'`
Enter
Wait
Type `omniglass component event list "$bar" | jq -c '[.events[] | .key] | .[:2]'`
Enter
Wait

Hide
Type `demo_hush 'omniglass secret create --name stale-bar-login --owner-kind platform --secret-type basic-auth --fields "{\"username\":\"admin\",\"password\":\"stale\"}"'`
Enter
Wait
Type `demo_hush 'inputs=$(omniglass endpoint get "$stale/https" | jq -c ".inputs | .login = \"stale-bar-login\"")'`
Enter
Wait
Type `demo_hush 'omniglass endpoint delete "$stale/https"'`
Enter
Wait
Type `demo_hush 'omniglass endpoint create --component "$stale" --driver kestrel-admin --node edge-hq --inputs "$inputs"'`
Enter
Wait
Type "clear"
Enter
Wait
Show

Type "# A second bar holds a stale login: the same reboot, refused by the bar"
Enter
Wait
Type `omniglass component command issue "$stale" --command-type reboot | jq -c '{status, phase}'`
Enter
Wait

Hide
Type `demo_until 60 'omniglass component command list "$stale" --limit 1 | jq -e ".commands[0].status == \"failed\""'`
Enter
Wait@70s
Show

Type "# The report carries the refusal: failed, never read as done"
Enter
Wait
Type `omniglass component command list "$stale" --limit 1 | jq -c '.commands[0] | {status, settled_by, reason}'`
Enter
Wait
