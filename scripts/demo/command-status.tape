# A command's status reads from issue to settlement
#
# Two simulated Kestrel bars in the depot's Bay 1, one commanded over https by the shipped
# kestrel-api driver and one over its control socket by kestrel-ws: set-volume with --wait
# answers settled, the bar's reply its evidence. A third bar, behind a node that is powered
# off, reads queued, then timed out with the reason, and the outcome lands on its timeline (#1155).

Type "# Two Kestrel bars in the depot's Bay 1, by their dotted addresses"
Enter
Wait
Type `web='depot.bay-1.$comp.videobar-1' socket='depot.bay-1.$comp.videobar-2'`
Enter
Wait

Hide
Type `demo_hush 'addr=$(omniglass component simulation get "$socket" | jq -r .address)'`
Enter
Wait
Type `demo_hush 'omniglass endpoint delete "$socket/https"'`
Enter
Wait
Type `demo_hush 'omniglass secret create --name depot-bar-login --owner-kind platform --secret-type basic-auth --fields "{\"username\":\"admin\",\"password\":\"kestrel\"}"'`
Enter
Wait
Type `demo_hush 'omniglass endpoint create --component "$socket" --driver kestrel-ws --node edge-hq --inputs "{\"host\":\"$addr\",\"port\":\"9443\",\"login\":\"depot-bar-login\"}"'`
Enter
Wait
Type `demo_until 120 'omniglass component reachability list "$socket" | jq -e ".endpoints[] | select(.endpoint == \"wss\") | .session.state == \"up\""'`
Enter
Wait@150s
Type "clear"
Enter
Wait
Show

Type "# Over https: --wait blocks until the bar's reply settles it"
Enter
Wait
Type `omniglass component command issue "$web" --command-type set-volume --value 42 --wait | jq -c '{status, endpoint, driver, settled_by, reason, reply}'`
Enter
Wait
Type "# Over the control socket: the answer frame carries back the minted id, and the value set"
Enter
Wait
Type `omniglass component command issue "$socket" --command-type set-volume --value 37 --wait | jq -c '{status, endpoint, driver, reason, reply: (.reply | fromjson | {type, value})}'`
Enter
Wait
Type "# Each reads back by its id under its component, the whole arc stamped"
Enter
Wait
Type `omniglass component command list "$socket" --limit 1 | jq -c '.commands[0] | {command_type, intended, phase, settle_mode, issuer, stamped: ([.issued_at, .delivered_at, .executed_at, .settled_at] | map(. != null))}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show

Type "# A third bar, collected by a node that is powered off: the command waits in its queue"
Enter
Wait
Type `dark='depot.bay-1.$comp.dark-bar'`
Enter
Wait

Hide
Type `demo_hush 'omniglass node create --name edge-depot-dark --label "Depot edge (powered off)"'`
Enter
Wait
Type `demo_hush 'omniglass component create --name dark-bar --product kestrel-vroom --location depot.bay-1'`
Enter
Wait
Type `demo_hush 'omniglass endpoint create --component "$dark" --driver kestrel-api --node edge-depot-dark --inputs "{\"host\":\"127.60.9.250\",\"login\":\"depot-bar-login\"}"'`
Enter
Wait
Show

Type `omniglass component command issue "$dark" --command-type set-volume --value 30 | jq -c '{status, phase, endpoint, node, settle_window_seconds}'`
Enter
Wait

Hide
Type `demo_until 40 'omniglass component command list "$dark" --status timed-out | jq -e ".commands | length == 1"'`
Enter
Wait@50s
Show

Type "# Its window closed with no evidence: timed out, and the reason says where it stopped"
Enter
Wait
Type `omniglass component command list "$dark" | jq -c '.commands[0] | {status, settled_by, reason}'`
Enter
Wait
Type `omniglass component event list "$dark" | jq -c '[.events[] | .key]'`
Enter
Wait
