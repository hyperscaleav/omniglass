# HTTP logins keep a session, and digest answers its challenge
#
# A simulated touch panel behind a form login and a simulated display answering
# HTTP digest, each attached to its shipped driver: both log in and collect, the
# panel logs in exactly once more when it forgets its session, a wrong password
# is refused after one attempt, and fixing the secret in place recovers (#1114).

Type "# A touch panel behind a form login, and a display answering digest, by their dotted addresses"
Enter
Wait
Type `panel='hq.west.level-2.huddle.$comp.panel-1' display='airport.annex.briefing.$comp.display-1'`
Enter
Wait
Type `omniglass component simulation get "$panel" | jq -c '{address, http: [.surface[] | select(.protocol == "http") | {port, tls, auth_type}]}'`
Enter
Wait
Type `omniglass component simulation get "$display" | jq -c '{address, http: [.surface[] | select(.protocol == "http") | {port, tls, auth_type}]}'`
Enter
Wait
Sleep 2s

Type "# A login secret each, and the shipped http driver that logs in with it"
Enter
Wait
Type `omniglass secret create --name demo-panel-login --owner-kind platform --secret-type basic-auth --fields '{"username":"admin","password":"newtron"}' | jq -c '{name, fields: [.fields[] | {name, value}]}'`
Enter
Wait
Type `omniglass secret create --name demo-display-login --owner-kind platform --secret-type basic-auth --fields '{"username":"admin","password":"boreal"}' | jq -c '{name, fields: [.fields[] | {name, value}]}'`
Enter
Wait
Type `omniglass endpoint create --component "$panel" --driver newtron-panel-web --node edge-hq --label 'Panel web API' --inputs '{"host":"127.60.0.17","port":"443","login":"demo-panel-login"}' | jq -c '{name, driver}'`
Enter
Wait
Type `omniglass endpoint create --component "$display" --driver boreal-edge-web --node edge-hq --label 'Display web admin' --inputs '{"host":"127.60.0.20","port":"80","login":"demo-display-login"}' | jq -c '{name, driver}'`
Enter
Wait
Sleep 1s

Hide
Type `demo_hush 'panel_ep=$(omniglass endpoint get "$panel/https" | jq -r .id)'`
Enter
Wait
Type `demo_until 120 'omniglass component reachability list "$panel" | jq -e ".endpoints[] | select(.endpoint == \"https\") | .authenticated.value == \"yes\"" && omniglass component reachability list "$display" | jq -e ".endpoints[] | select(.endpoint == \"http\") | .authenticated.value == \"yes\""'`
Enter
Wait@150s
Type "clear"
Enter
Wait
Show

Type "# Each logs in, and collects (reach: an endpoint's reachability rungs, and why it is refused)"
Enter
Wait
Type `reach() { omniglass component reachability list "$1" | jq -c --arg e "$2" '.endpoints[] | select(.endpoint == $e) | {endpoint, verdict: .verdict.value, authenticated: .authenticated.value, rejection: .rejection.reason}'; }`
Enter
Wait
Type `reach "$panel" https; reach "$display" http`
Enter
Wait
Type `omniglass component property sample list "$panel" model-number --limit 5 | jq -c '[.samples[] | select(.instance == "https")][0] | {instance, value}'`
Enter
Wait
Type `grep 'msg="http login"' /stack/logs/node.log | grep "$panel_ep" | grep -o 'why=.*'`
Enter
Wait
Sleep 2s

Hide
Type "clear"
Enter
Wait
Show
Type "# The panel forgets its sessions, as on a reboot: the node logs in exactly once more"
Enter
Wait
Type `omniglass component simulation fire "$panel" --surface http-api --method POST --path /api/admin/sessions/expire | jq -c '{status, body: (.body | fromjson)}'`
Enter
Wait
Hide
Type `demo_until 90 'grep "msg=\"http login\"" /stack/logs/node.log | grep "$panel_ep" | grep -q "session lapsed"'`
Enter
Wait@120s
Show
Type `grep 'msg="http login"' /stack/logs/node.log | grep "$panel_ep" | grep -o 'why=.*'`
Enter
Wait
Type `reach "$panel" https`
Enter
Wait
Sleep 2s

Hide
Type "clear"
Enter
Wait
Show
Type "# A wrong password: refused, with its reason, after one attempt"
Enter
Wait
Type `omniglass secret update demo-panel-login --fields '{"password":"wrong-password"}' | jq -c '{name, fields: [.fields[] | {name, value}]}'`
Enter
Wait
Hide
Type `demo_until 90 'omniglass component reachability list "$panel" | jq -e ".endpoints[] | select(.endpoint == \"https\") | .rejection.reason != null"'`
Enter
Wait@120s
Show
Type `reach "$panel" https`
Enter
Wait
Hide
Type `demo_hush 'sleep 70'`
Enter
Wait@100s
Show
Type "# Seventy seconds (two polls) later, the refused login is still the last"
Enter
Wait
Type `grep 'msg="http login"' /stack/logs/node.log | grep "$panel_ep" | grep -o 'why=.*'`
Enter
Wait
Sleep 2s

Hide
Type "clear"
Enter
Wait
Show
Type "# The secret fixed in place: the gate lifts, and the panel recovers"
Enter
Wait
Type `omniglass secret update demo-panel-login --fields '{"password":"newtron"}' | jq -c '{name, fields: [.fields[] | {name, value}]}'`
Enter
Wait
Hide
Type `demo_until 120 'omniglass component reachability list "$panel" | jq -e ".endpoints[] | select(.endpoint == \"https\") | .authenticated.value == \"yes\" and .rejection.reason == null"'`
Enter
Wait@150s
Show
Type `reach "$panel" https`
Enter
Wait
