# A preview takes the lane of the endpoint it names, and honors that endpoint's gate
#
# Every request a node sends to a device rides that endpoint's lane: one in flight at a time, an
# operator's ahead of the scheduled polls, and the endpoint's gate read as each request is taken.
# A preview aimed at an attached display waits its turn on the display's lane and reads it. Once
# the display refuses the endpoint's login, the same preview is held with the refusal and nothing
# is sent; fixed in place, the gate lifts and the preview reads again (#1249).

Type "# The node's pool runs endpoint lanes: one request per device at a time, operators first"
Enter
Wait
Type `omniglass node run --help | grep -- '--lane-workers '`
Enter
Wait

Type "# A display answering HTTP digest, a login for it, and its shipped driver attached"
Enter
Wait
Type `display='airport.annex.briefing.$comp.display-1'`
Enter
Wait
Hide
Type `demo_hush 'sim=$(omniglass component simulation get "$display"); host=$(echo "$sim" | jq -r .address); port=$(echo "$sim" | jq -r "[.surface[] | select(.protocol == \"http\")][0].port")'`
Enter
Wait
Show
Type `omniglass secret create --name demo-lane-login --owner-kind platform --secret-type basic-auth --fields '{"username":"admin","password":"boreal"}' | jq -c '{name}'`
Enter
Wait
Type `inputs="{\"host\":\"$host\",\"port\":\"$port\",\"login\":\"demo-lane-login\"}"`
Enter
Wait
Type `omniglass endpoint create --component "$display" --driver boreal-edge-web --node edge-hq --inputs "$inputs" | jq -c '{name, driver}'`
Enter
Wait
Hide
Type `demo_until 120 'omniglass component reachability list "$display" | jq -e ".endpoints[] | select(.endpoint == \"http\") | .authenticated.value == \"yes\""'`
Enter
Wait@150s
Type "clear"
Enter
Wait
Show

Type "# A preview naming the display's address rides the display's lane, and reads it; nothing is stored (reach: the endpoint's auth rung and what refused it)"
Enter
Wait
Type `reach() { omniglass component reachability list "$display" | jq -c '.endpoints[] | select(.endpoint == "http") | {endpoint, authenticated: .authenticated.value, refused: .rejection.class}'; }`
Enter
Wait
Type `reach`
Enter
Wait
Type `omniglass driver preview --node edge-hq --driver boreal-edge-web --polls '["identity"]' --inputs "$inputs" | jq -c '{stored, results: [.results[] | {poll, samples: [.samples[] | .name]}]}'`
Enter
Wait

Type "# A wrong password: the display refuses the endpoint's login, and its gate closes"
Enter
Wait
Type `omniglass secret update demo-lane-login --fields '{"password":"wrong-password"}' | jq -c '{name}'`
Enter
Wait
Hide
Type `demo_until 120 'omniglass component reachability list "$display" | jq -e ".endpoints[] | select(.endpoint == \"http\") | .rejection.class == \"auth\""'`
Enter
Wait@150s
Show
Type `reach`
Enter
Wait

Type "# The same preview waits on that lane, reads the gate, and is held: a 409 naming the refusal, nothing sent"
Enter
Wait
Type `omniglass driver preview --node edge-hq --driver boreal-edge-web --polls '["identity"]' --inputs "$inputs" 2>/dev/null | jq -r '"\(.status) \(.detail)"' || true`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# The secret fixed in place: the gate lifts, and the preview reads the display again"
Enter
Wait
Type `omniglass secret update demo-lane-login --fields '{"password":"boreal"}' | jq -c '{name}'`
Enter
Wait
Hide
Type `demo_until 120 'omniglass component reachability list "$display" | jq -e ".endpoints[] | select(.endpoint == \"http\") | .authenticated.value == \"yes\" and .rejection == null"'`
Enter
Wait@150s
Show
Type `reach`
Enter
Wait
Type `omniglass driver preview --node edge-hq --driver boreal-edge-web --polls '["identity"]' --inputs "$inputs" | jq -c '{stored, results: [.results[] | {poll, samples: [.samples[] | .name]}]}'`
Enter
Wait
