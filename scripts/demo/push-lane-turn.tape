# The push lane reports how long a push waits for its publish turn, and lab measure reads its refusals
#
# Every push and webhook post publishes through the push lane's one publisher, one at a time, and a
# push that waits more than 5 seconds for the turn is refused with a 503 and nothing minted. The
# server now writes that turn every report window, on its own platform component: the publishes,
# the refusals among them, and the p99 and max wait in milliseconds, all four on the Ingest panel.
# omniglass lab measure sums them into a push-lane publish row, turns taken against turns refused,
# so a refused push reads as refused, never as missing (#1746, ADR-1746).

# demo-needs: docker-socket

Hide
Type `demo_hush 'omniglass lab run --scenario corporate --systems 3 --port 0 --ready-file /tmp/turn.env >/tmp/turn.log 2>&1 & scale=$!'`
Enter
Wait
Type `demo_until 170 'test -s /tmp/turn.env' || demo_hush 'tail -30 /tmp/turn.log'`
Enter
Wait@180s
Type `demo_hush '. /tmp/turn.env; export OMNIGLASS_SERVER=$LAB_HOST_URL OMNIGLASS_TOKEN=$LAB_TOKEN'`
Enter
Wait
Type `demo_until 150 'omniglass view platform list | jq -e "[.servers[].groups[].figures[] | select(.key == \"push-lane-publish-count\")] | length > 0" >/dev/null'`
Enter
Wait@160s
Type "clear"
Enter
Wait
Show

Type "# A corporate scale lab of 3 systems is up; its site's booking service and meeting platform push every 30 s"
Enter
Wait
Type "# The server's Ingest panel now carries the push lane's turn: publishes, refusals, and the wait in ms"
Enter
Wait
Type `omniglass view platform list | jq -c '.servers[].groups[] | select(.name == "ingest") | .figures[] | select(.key | startswith("push-lane-")) | {key, value}'`
Enter
Wait
Type "# Push by hand too, every 5 seconds, so the measured window holds more turns"
Enter
Wait
Type `(while :; do omniglass telemetry push --owner '{"kind":"system","ref":"hq-huddle-room-1"}' --metrics '[{"name":"occupancy-count","value":3}]' >/dev/null 2>&1; sleep 5; done) & pusher=$!`
Enter
Wait
Type "# Measure the lab for 2 minutes; the run sums every window's push-lane figures into its delivery table"
Enter
Wait
Type "omniglass lab measure --systems 0 --warmup 10s --window 2m --fleet-reads 1 --backlog 0 --json /tmp/turn.json >/tmp/turn.txt 2>/tmp/turn.err & measure=$!"
Enter
Wait
Hide
Type `demo_hush 'wait "$measure"' || demo_hush 'tail -30 /tmp/turn.err'`
Enter
Wait@330s
Type "clear"
Enter
Wait
Show
Type "# The push lane's rows: the publish turn first, in pushes, then each kind across the ingest hop"
Enter
Wait
Type `sed -n '/^delivery /,$p' /tmp/turn.txt | awk 'NR == 1 || $2 == "push"'`
Enter
Wait
Type "# The same row in the JSON: every turn taken, none refused at the 5 s bound, a loss of 0"
Enter
Wait
Type `jq -c '.delivery[] | select(.lane == "push" and .hop == "publish") | {lane, hop, in, out, loss, outcomes}' /tmp/turn.json`
Enter
Wait

Hide
Type `demo_hush 'kill "$pusher"; kill -INT "$scale"; wait "$scale"'`
Enter
Wait@120s
Show
