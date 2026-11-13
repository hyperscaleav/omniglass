# A node's held sessions come up as their worklist lands, through bounded first dials
#
# The node's run loop reconciles held sessions the moment a worklist arrives, runs each one
# supervised (a panic costs that one session, never the node), and makes each session's first dial
# through one of --session-dials node-wide slots (default 16; a redial takes none). A Kestrel bar's
# control socket is attached on edge-hq and comes up, then is detached and goes away (#1636).

Type "# The node's bound on first dials, and its env twin"
Enter
Wait
Type `omniglass node run --help | grep -- --session-dials`
Enter
Wait
Type "# A bound below one is refused before the node starts"
Enter
Wait
Type `OMNIGLASS_NODE_SESSION_DIALS=0 omniglass node run --name edge-demo --token unused || true`
Enter
Wait
Type `omniglass node run --name edge-demo --token unused --session-dials 0 || true`
Enter
Wait

Hide
Type `bar='hq.west.level-2.boardroom-b.$comp.videobar-1'`
Enter
Wait
Type `demo_hush 'addr=$(omniglass component simulation get "$bar" | jq -r .address)'`
Enter
Wait
Type `demo_hush 'omniglass endpoint delete "$bar/https"'`
Enter
Wait
Type `demo_hush 'omniglass secret create --name boardroom-ws-login --owner-kind platform --secret-type basic-auth --fields "{\"username\":\"admin\",\"password\":\"kestrel\"}"'`
Enter
Wait
Type "clear"
Enter
Wait
Show

Type "# Attach Boardroom B's bar over its control socket on edge-hq (the https attach is detached, off screen)"
Enter
Wait
Type `bar='hq.west.level-2.boardroom-b.$comp.videobar-1'`
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

Type "# The worklist landed, the session took a slot for its first dial, and it is up"
Enter
Wait
Type `omniglass component reachability list "$bar" | jq -c '[.endpoints[] | {endpoint, session: .session.state}]'`
Enter
Wait
Type `omniglass node log list edge-hq | jq -c '[.logs[] | select((.message | startswith("session ")) and .attributes.endpoint == "wss") | .message] | reverse'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# Detach it: the next worklist drops the socket, and the session goes with it"
Enter
Wait
Type `omniglass endpoint delete "$bar/wss"`
Enter
Wait
Hide
Type `demo_until 60 'grep "msg=\"session torn down\"" /stack/logs/node.log | grep -q "endpoint=wss"'`
Enter
Wait@90s
Show
Type `grep 'msg="session torn down"' /stack/logs/node.log | grep 'endpoint=wss' | tail -1 | sed 's/.*msg="\([^"]*\)".*reason="\([^"]*\)".*/\1: \2/'`
Enter
Wait
Type `omniglass component reachability list "$bar" | jq -c '[.endpoints[] | {endpoint, session: .session.state}]'`
Enter
Wait
