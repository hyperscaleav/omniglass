# An endpoint's gate holds a set of reasons, and a device that is gone closes it
#
# An endpoint's gate used to hold one reason: the device refused the credential. A device that is
# gone answers nothing, so its polls timed out one by one, each counted a failure. Now the gate
# holds a set of reasons (refused, unreachable): two down probe verdicts in a row close it as
# unreachable, its scheduled polls are held and counted as held, a run now or a preview answers
# 409 naming the reason, and the probe's next up verdict reopens it (#1650).

Type "# An occupancy sensor with an http endpoint; its edge node probes the sensor's address"
Enter
Wait
Type `s='east.hall.mezzanine.class-202.$comp.sensor-1'`
Enter
Wait

Hide
Type `demo_hush 'inputs=$(omniglass endpoint get "$s/http" | jq -c .inputs)'`
Enter
Wait
Type `demo_until 120 'omniglass endpoint poll run "$s/http" occupancy > /dev/null 2>&1'`
Enter
Wait@150s
Type "clear"
Enter
Wait
Show

Type "# The gate is open: no reasons, and a run now reads the sensor"
Enter
Wait
Type `omniglass component reachability list "$s" | jq -c '.endpoints[] | select(.endpoint == "http") | {endpoint, gate_reasons}'`
Enter
Wait
Type `omniglass endpoint poll run "$s/http" occupancy | jq -c '{stored, poll, samples: [.samples[] | .name]}'`
Enter
Wait

Type "# The sensor is unplugged: its probe reads down, and two down verdicts close the gate"
Enter
Wait
Type `omniglass component impairment set "$s" --unplugged | jq -c '{unplugged}'`
Enter
Wait
Hide
Type `demo_until 120 'omniglass component reachability list "$s" | jq -e ".endpoints[] | select(.endpoint == \"http\") | .gate_reasons | length > 0"'`
Enter
Wait@150s
Show

Type "# The gate names its reason: unreachable, since when, and the probe that read it"
Enter
Wait
Type `omniglass component reachability list "$s" | jq -c '.endpoints[] | select(.endpoint == "http") | {endpoint, gate_reasons: [.gate_reasons[] | {kind, since, source: .source[0:12]}]}'`
Enter
Wait
Type `omniglass component property sample list "$s" endpoint-rejection | jq -r '[.samples[] | select(.instance == "http")][0].value'`
Enter
Wait

Type "# A run now is held, nothing is sent, and the 409 says why and what reopens it"
Enter
Wait
Type `omniglass endpoint poll run "$s/http" occupancy 2>/dev/null | jq -r '"\(.status) \(.detail)"' || true`
Enter
Wait
Type "# So is a preview against the sensor's address, through the same node"
Enter
Wait
Type `omniglass driver preview --node edge-hq --driver http-json --polls '["occupancy"]' --inputs "$inputs" 2>/dev/null | jq -r '"\(.status) \(.detail)"' || true`
Enter
Wait

Hide
Type `demo_until 150 'omniglass component metric sample list "$s" poll-held-count | jq -e "[.samples[] | select(.instance == \"http\")] | length > 0"'`
Enter
Wait@180s
Type "clear"
Enter
Wait
Show

Type "# Its scheduled polls are held as well, and counted as held, not as failures"
Enter
Wait
Type `omniglass component metric sample list "$s" poll-held-count | jq -c '[.samples[] | select(.instance == "http")][0] | {metric: "poll-held-count", instance, value}'`
Enter
Wait

Type "# Plugged back in, the probe's next up verdict reopens the gate"
Enter
Wait
Type `omniglass component impairment set "$s" | jq -c '{unplugged}'`
Enter
Wait
Hide
Type `demo_until 120 'omniglass component reachability list "$s" | jq -e ".endpoints[] | select(.endpoint == \"http\") | .gate_reasons == null"'`
Enter
Wait@150s
Show
Type `omniglass component reachability list "$s" | jq -c '.endpoints[] | select(.endpoint == "http") | {endpoint, gate_reasons}'`
Enter
Wait
Type `omniglass endpoint poll run "$s/http" occupancy | jq -c '{stored, poll, samples: [.samples[] | .name]}'`
Enter
Wait
