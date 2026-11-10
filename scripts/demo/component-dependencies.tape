# A disruptive command's hold set follows the component's dependency edges
#
# A component now declares what it depends on: power (on a named outlet), host and path, each edge
# refused if it would close a loop. A disruptive command reads those edges for its hold set: a
# restart of a video bar closes the gates of the bar's in-band endpoints and of the touch panel it
# hosts, while an out-of-band endpoint stays open and the PDU upstream of it is not held, since a
# restart takes down what it hosts, not what feeds it (#1648).

Type "# A video bar in the boardroom: powered from a PDU outlet, and hosting the room's touch panel"
Enter
Wait
Type `d='hq.west.level-2.boardroom-a.$comp'; b="$d.videobar-1"; p="$d.panel-1"`
Enter
Wait
Type `omniglass component dependency list "$b" | jq -c '{upstreams: [.upstreams[] | {upstream, kind, outlet}], dependents: [.dependents[] | {component, kind}]}'`
Enter
Wait

Type "# An edge that would close a loop is refused: the panel cannot host the bar that hosts it"
Enter
Wait
Type `omniglass component dependency create "$b" --upstream "$p" --kind host 2>/dev/null | jq -r '"\(.status) \(.detail)"' || true`
Enter
Wait
Type "# A power edge names the outlet it hangs off"
Enter
Wait
Type `omniglass component dependency create "$p" --upstream "$d.power-1" --kind power 2>/dev/null | jq -r '"\(.status) \(.detail)"' || true`
Enter
Wait

Type "# A restart of our own: disruptive, with a quiet long enough to read the hold on screen"
Enter
Wait
Type `omniglass command-type create --name bar-restart --disruptive --quiet-seconds 120 --hold-seconds 300 | jq -c '{name, disruptive, quiet_seconds, hold_seconds}'`
Enter
Wait

Hide
Type `demo_hush 'inputs=$(omniglass endpoint get "$b/https" | jq -c .inputs)'`
Enter
Wait
Type `demo_hush 'spec=$(omniglass driver get kestrel-api | jq -c ".spec | .commands += [{command_type: \"bar-restart\", request: {method: \"POST\", path: \"/api/admin/reboot\"}}]")'`
Enter
Wait
Type `demo_hush 'omniglass driver create --name kestrel-api-restart --label "Kestrel Device API with restart" --spec "$spec"'`
Enter
Wait
Type `demo_hush 'omniglass endpoint delete "$b/https"'`
Enter
Wait
Type `demo_hush 'omniglass endpoint create --component "$b" --driver kestrel-api-restart --node edge-hq --inputs "$inputs"'`
Enter
Wait
Type `demo_until 150 'omniglass endpoint poll run "$b/https" system > /dev/null 2>&1'`
Enter
Wait@180s
Type "clear"
Enter
Wait
Show

Type "# The bar's websocket endpoint stands in for a management port: out of band, it answers through a restart"
Enter
Wait
Type `omniglass endpoint update "$b/wss" --out-of-band | jq -c '{name, out_of_band}'`
Enter
Wait

Type "# Restart the bar"
Enter
Wait
Type `omniglass component command issue "$b" --command-type bar-restart | jq -c '{command_type, status}'`
Enter
Wait
Hide
Type `demo_until 60 'omniglass component reachability list "$p" | jq -e "[.endpoints[] | .gate_reasons // [] | .[] | .kind] | index(\"disrupted\")"'`
Enter
Wait@90s
Show

Type "# Held: the bar's in-band endpoint and every endpoint of the panel it hosts. Open: the out-of-band one, and the PDU feeding the bar"
Enter
Wait
Type `for c in videobar-1 panel-1 power-1; do omniglass component reachability list "$d.$c" | jq -c --arg c $c '{component: $c, held: [.endpoints[] | select([(.gate_reasons // [])[] | .kind] | index("disrupted")) | .endpoint], open: [.endpoints[] | select([(.gate_reasons // [])[] | .kind] | index("disrupted") | not) | .endpoint]}'; done`
Enter
Wait
Type "# The panel's gates name the bar's restart as their source"
Enter
Wait
Type `omniglass component reachability list "$p" | jq -c '.endpoints[0] | {endpoint, gate_reasons: [(.gate_reasons // [])[] | select(.kind == "disrupted") | {kind, source}]}'`
Enter
Wait
