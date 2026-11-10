# An endpoint reached through a closed upstream is held, with that upstream named as the cause
#
# A path dependency says a component is reached through another: a touch panel behind its room's
# videobar. It was recorded and not yet followed. Now the node follows it: while the upstream's gate
# is closed, the dependent's in-band endpoints carry an upstream reason naming it, their polls are
# held and their probes are not run, so the videobar alone reads as down. Once the videobar reopens,
# the panel waits for its own probe to read up (#1652).

Type "# A boardroom touch panel is reached through the room's videobar: a path edge"
Enter
Wait
Type `d='hq.west.level-2.boardroom-a.$comp'; b="$d.videobar-1"; p="$d.panel-1"`
Enter
Wait

Hide
Type `demo_until 120 'omniglass endpoint poll run "$p/https" status > /dev/null 2>&1'`
Enter
Wait@150s
Type "clear"
Enter
Wait
Show

Type `omniglass component dependency list "$p" | jq -c '.upstreams[] | {component, upstream, kind}'`
Enter
Wait

Type "# Both gates are open: no reasons, and a run now reads the panel"
Enter
Wait
Type `omniglass component reachability list "$p" | jq -c '.endpoints[] | select(.endpoint == "https") | {endpoint, gate_reasons}'`
Enter
Wait
Type `omniglass endpoint poll run "$p/https" status | jq -c '{stored, poll, samples: [.samples[] | .name]}'`
Enter
Wait

Type "# The videobar is unplugged: its own probes read down and close its gates"
Enter
Wait
Type `omniglass component impairment set "$b" --unplugged | jq -c '{unplugged}'`
Enter
Wait
Hide
Type `demo_until 120 'omniglass component reachability list "$p" | jq -e "[.endpoints[] | .gate_reasons // [] | .[] | select(.kind == \"upstream\")] | length > 0"'`
Enter
Wait@150s
Show

Type "# The videobar is closed for its own reason: unreachable"
Enter
Wait
Type `omniglass component reachability list "$b" | jq -c '.endpoints[] | {endpoint, reasons: [.gate_reasons[]? | .kind]}'`
Enter
Wait

Type "# The panel is closed by the path: an upstream reason naming the videobar"
Enter
Wait
Type `omniglass component reachability list "$p" | jq -c '.endpoints[] | {endpoint, gate_reasons: [.gate_reasons[]? | {kind, since, component}]}'`
Enter
Wait

Type "# A run now on the panel is held, nothing is sent, and the 409 names the upstream"
Enter
Wait
Type `omniglass endpoint poll run "$p/https" status 2>/dev/null | jq -r '"\(.status) \(.detail)"' || true`
Enter
Wait

Hide
Type `demo_until 150 'omniglass component metric sample list "$p" poll-held-count | jq -e "[.samples[] | select(.instance == \"https\")] | length > 0"'`
Enter
Wait@180s
Type "clear"
Enter
Wait
Show

Type "# Its scheduled polls are held as well, and counted as held, not as failures"
Enter
Wait
Type `omniglass component metric sample list "$p" poll-held-count | jq -c '[.samples[] | select(.instance == "https")][0] | {metric: "poll-held-count", instance, value}'`
Enter
Wait

Type "# Plugged back in, the videobar reopens, and the panel's own probe reopens the panel"
Enter
Wait
Type `omniglass component impairment set "$b" | jq -c '{unplugged}'`
Enter
Wait
Hide
Type `demo_until 150 'omniglass component reachability list "$p" | jq -e "all(.endpoints[]; .gate_reasons == null)"'`
Enter
Wait@180s
Show
Type `omniglass component reachability list "$p" | jq -c '.endpoints[] | select(.endpoint == "https") | {endpoint, gate_reasons}'`
Enter
Wait
Type `omniglass endpoint poll run "$p/https" status | jq -c '{stored, poll, samples: [.samples[] | .name]}'`
Enter
Wait
