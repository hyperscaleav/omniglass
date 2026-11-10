# A reboot closes its device's gates until each endpoint reads up again
#
# A reboot takes its device down, and the node used to read the outage as poll failures. Now reboot
# is a disruptive command type with a quiet period and a hold backstop: its delivery closes the gate
# of every endpoint of the component on that node with the disrupted reason, nothing runs against
# them during the quiet, a run now answers 409 naming the reboot, held polls count as held, and each
# endpoint reopens on its own first up verdict after the quiet (#1651).

Type "# Reboot is a disruptive command type: a quiet period and a hold backstop, neither the settle window"
Enter
Wait
Type `omniglass command-type get reboot | jq -c '{name, disruptive, quiet_seconds, hold_seconds, settle_window_seconds}'`
Enter
Wait
Type "# A custom one is held to the same rule: its backstop has to outlast its quiet"
Enter
Wait
Type `omniglass command-type create --name power-cycle --disruptive --quiet-seconds 60 --hold-seconds 30 2>/dev/null | jq -r '"\(.status) \(.detail)"' || true`
Enter
Wait

Type "# An occupancy sensor whose driver carries the reboot, collected by its edge node"
Enter
Wait
Type `s='east.hall.mezzanine.class-202.$comp.sensor-1'`
Enter
Wait

Hide
Type `demo_hush 'inputs=$(omniglass endpoint get "$s/http" | jq -c .inputs)'`
Enter
Wait
Type `demo_hush 'spec=$(omniglass driver get http-json | jq -c ".spec | .commands = [{command_type: \"reboot\", request: {method: \"POST\", path: \"/api/admin/reboot\"}}]")'`
Enter
Wait
Type `demo_hush 'omniglass driver create --name census-reboot --label "Census sensor with reboot" --spec "$spec"'`
Enter
Wait
Type `demo_hush 'omniglass endpoint delete "$s/http"'`
Enter
Wait
Type `demo_hush 'omniglass endpoint create --component "$s" --driver census-reboot --node edge-hq --inputs "$inputs"'`
Enter
Wait
Type `demo_until 150 'omniglass endpoint poll run "$s/http" occupancy > /dev/null 2>&1'`
Enter
Wait@180s
Type "clear"
Enter
Wait
Show

Type `omniglass component reachability list "$s" | jq -c '.endpoints[] | {endpoint, gate_reasons}'`
Enter
Wait

Type "# Reboot it; the sensor goes dark as it restarts"
Enter
Wait
Type `omniglass component command issue "$s" --command-type reboot | jq -c '{command_type, status}'`
Enter
Wait
Type `omniglass component impairment set "$s" --unplugged | jq -c '{unplugged}'`
Enter
Wait
Hide
Type `demo_until 60 'omniglass component reachability list "$s" | jq -e "[.endpoints[] | .gate_reasons // [] | .[] | .kind] | index(\"disrupted\")"'`
Enter
Wait@90s
Show

Type "# The delivery closed the gate before it sent: disrupted, since when, and by which command"
Enter
Wait
Type `omniglass component reachability list "$s" | jq -c '.endpoints[] | {endpoint, gate_reasons: [(.gate_reasons // [])[] | {kind, since, source}]}'`
Enter
Wait
Type "# A run now is held, nothing is sent, and the 409 names the reboot"
Enter
Wait
Type `omniglass endpoint poll run "$s/http" occupancy 2>/dev/null | jq -r '"\(.status) \(.detail)"' || true`
Enter
Wait

Hide
Type `demo_until 150 'omniglass component metric sample list "$s" poll-held-count | jq -e "[.samples[] | select(.instance == \"http\")] | length > 0"'`
Enter
Wait@180s
Type `demo_until 150 'omniglass component reachability list "$s" | jq -e "[.endpoints[] | .gate_reasons // [] | .[] | .kind] | index(\"unreachable\")"'`
Enter
Wait@180s
Type "clear"
Enter
Wait
Show

Type "# Its due polls were held and counted as held, not as failures"
Enter
Wait
Type `omniglass component metric sample list "$s" poll-held-count | jq -c '[.samples[] | select(.instance == "http")][0] | {metric: "poll-held-count", instance, value}'`
Enter
Wait
Type "# Past the quiet the sensor is still dark: its probe reads it down, and a down verdict releases nothing"
Enter
Wait
Type `omniglass component reachability list "$s" | jq -c '.endpoints[] | {endpoint, gate_reasons: [(.gate_reasons // [])[] | .kind]}'`
Enter
Wait

Type "# The sensor comes back: its first up verdict reopens the gate, with no wait for the backstop"
Enter
Wait
Type `omniglass component impairment set "$s" | jq -c '{unplugged}'`
Enter
Wait
Hide
Type `demo_until 150 'omniglass component reachability list "$s" | jq -e "[.endpoints[] | .gate_reasons // [] | length] | add == 0"'`
Enter
Wait@180s
Show
Type `omniglass component reachability list "$s" | jq -c '.endpoints[] | {endpoint, gate_reasons}'`
Enter
Wait
Type `omniglass endpoint poll run "$s/http" occupancy | jq -c '{stored, poll, samples: [.samples[] | .name]}'`
Enter
Wait
