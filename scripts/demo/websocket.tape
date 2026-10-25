# The websocket transport carries messages and answers by id
#
# A simulated Kestrel bar serves a control socket over wss. A websocket driver attached to it
# subscribes on connect, lands a change the bar pushes at once, sends a command answered by the id
# the node minted into it, recovers and subscribes again after a drop, and reads rejected after
# one refused upgrade until the secret is fixed in place (#1034).

Type "# Boardroom B's Kestrel bar, by its dotted address, and the shipped control-socket driver"
Enter
Wait
Type `bar='hq.west.level-2.boardroom-b.$comp.videobar-1'`
Enter
Wait
Type `omniglass driver get kestrel-ws | jq -c '.spec | {transport, upgrade: .session.upgrade, set_volume: .commands[0].request}'`
Enter
Wait
Sleep 3s

Hide
Type `demo_hush 'addr=$(omniglass component simulation get "$bar" | jq -r .address)'`
Enter
Wait
Type `demo_hush 'omniglass endpoint delete "$bar/https"'`
Enter
Wait
Type "clear"
Enter
Wait
Show

Type "# Attach it on its own secret (the seeded https attach is detached, off screen)"
Enter
Wait
Type `omniglass secret create --name boardroom-ws-login --owner-kind platform --secret-type basic-auth --fields '{"username":"admin","password":"kestrel"}' | jq -c '{name}'`
Enter
Wait
Type `omniglass endpoint create --component "$bar" --driver kestrel-ws --node edge-hq --inputs "{\"host\":\"$addr\",\"port\":\"9443\",\"login\":\"boardroom-ws-login\"}" | jq -c '{name, driver}'`
Enter
Wait

Hide
Type `demo_until 120 'omniglass component reachability list "$bar" | jq -e ".endpoints[] | select(.endpoint == \"wss\") | .session.state == \"up\""'`
Enter
Wait@150s
Show

Type `reach() { omniglass component reachability list "$bar" | jq -c '.endpoints[] | select(.endpoint == "wss") | {session: .session.state, authenticated: .authenticated.value, rejection: .rejection.reason}'; }`
Enter
Wait
Type `reach`
Enter
Wait
Type "# The subscription went out on connect, and the bar acknowledged it"
Enter
Wait
Type `armed() { omniglass node log list edge-hq | jq -c '[.logs[] | select(.message == "session armed" and .attributes.endpoint == "wss") | .attributes | {sent: (.line | fromjson | .type), answered: (.reply | fromjson | .type)}]'; }`
Enter
Wait
Type `armed`
Enter
Wait
Sleep 2s

Hide
Type "clear"
Enter
Wait
Show
Type "# A change on the bar is pushed over the socket and lands at once"
Enter
Wait
Type `omniglass component simulation fire "$bar" --surface http-api --method POST --path /api/audio/volume --body '{"volume": 37}' | jq -c .status`
Enter
Wait
Hide
Type `demo_until 5 'omniglass component metric sample list "$bar" volume --limit 5 | jq -e "[.samples[] | select(.instance == \"wss\")][0].value == 37"'`
Enter
Wait@10s
Show
Type `vols() { omniglass component metric sample list "$bar" volume --limit 20 | jq -c '[.samples[] | select(.instance == "wss") | .value][:4] | reverse'; }`
Enter
Wait
Type `vols`
Enter
Wait
Type "# A command rides the same socket: its set frame carries a fresh id, and the bar's answer carries it back"
Enter
Wait
Type `omniglass component command issue "$bar" --command-type set-volume --value 42 | jq -c '{command_type, settlement}'`
Enter
Wait
Hide
Type `demo_until 20 'omniglass component metric sample list "$bar" volume --limit 5 | jq -e "[.samples[] | select(.instance == \"wss\")][0].value == 42"'`
Enter
Wait@30s
Show
Type `vols`
Enter
Wait
Sleep 2s

Hide
Type "clear"
Enter
Wait
Show
Type "# The bar drops every socket: the session recovers and subscribes again"
Enter
Wait
Type `omniglass component simulation fire "$bar" --surface http-api --method POST --path /api/admin/sockets/drop | jq -c '.body | fromjson'`
Enter
Wait
Hide
Type `demo_until 30 '[ "$(omniglass node log list edge-hq | jq "[.logs[] | select(.message == \"session armed\" and .attributes.endpoint == \"wss\")] | length")" -ge 2 ]'`
Enter
Wait@40s
Show
Type `omniglass component property sample list "$bar" session-state --limit 3 | jq -c '[.samples[] | .value] | reverse'`
Enter
Wait
Type `armed`
Enter
Wait
Sleep 2s

Hide
Type "clear"
Enter
Wait
Show
Type "# A wrong password: one upgrade, refused, and the socket reads rejected with its reason"
Enter
Wait
Type `omniglass secret update boardroom-ws-login --fields '{"password":"wrong-password"}' | jq -c '{name}'`
Enter
Wait
Hide
Type `demo_until 60 'omniglass component reachability list "$bar" | jq -e ".endpoints[] | select(.endpoint == \"wss\") | .session.state == \"rejected\""'`
Enter
Wait@90s
Show
Type `reach`
Enter
Wait
Sleep 2s
Type "# The secret fixed in place: the socket comes back, and a change lands again"
Enter
Wait
Type `omniglass secret update boardroom-ws-login --fields '{"password":"kestrel"}' | jq -c '{name}'`
Enter
Wait
Hide
Type `demo_until 60 'omniglass component reachability list "$bar" | jq -e ".endpoints[] | select(.endpoint == \"wss\") | .session.state == \"up\" and .rejection == null"'`
Enter
Wait@90s
Show
Type `reach`
Enter
Wait
Type `omniglass component simulation fire "$bar" --surface http-api --method POST --path /api/audio/volume --body '{"volume": 44}' | jq -c .status`
Enter
Wait
Hide
Type `demo_until 10 'omniglass component metric sample list "$bar" volume --limit 5 | jq -e "[.samples[] | select(.instance == \"wss\")][0].value == 44"'`
Enter
Wait@20s
Show
Type `vols`
Enter
Wait
Sleep 3s
