# A pushed batch carries a delivery ledger, and lab measure reads the push lane as its own row
#
# A batch pushed to the API (POST /telemetry:push, or a webhook) carried no ledger, so whatever the
# push lane lost was invisible. Now the server mints one as it publishes: a boot per server process,
# a dense seq, and each kind's count, and the receipt carries it with a null node. omniglass lab
# measure reconciles the push lane apart from the nodes', one ingest row per kind, 0 healthy (#1696).
# A corporate lab runs a booking service a site, which pushes on its own (#1743); this one also pushes by hand,
# an event besides, so every push kind reads.

# demo-needs: docker-socket

Hide
Type `demo_hush 'omniglass lab run --scenario corporate --systems 3 --port 0 --ready-file /tmp/scale.env >/tmp/scale.log 2>&1 & scale=$!'`
Enter
Wait
Type `demo_until 170 'test -s /tmp/scale.env' || demo_hush 'tail -30 /tmp/scale.log'`
Enter
Wait@180s
Type `demo_hush '. /tmp/scale.env; export OMNIGLASS_SERVER=$LAB_HOST_URL OMNIGLASS_TOKEN=$LAB_TOKEN'`
Enter
Wait
Type "clear"
Enter
Wait
Show

Type "# A corporate scale lab of 3 systems is up. Push to one of its rooms, as a booking service would"
Enter
Wait
Type `omniglass telemetry push --owner '{"kind":"system","ref":"hq-huddle-room-1"}' --source room-booking-cloud --metrics '[{"name":"occupancy-count","value":3}]' --properties '[{"name":"occupancy-state","value":"occupied"}]' --events '[{"name":"call-started","message":"standup started"}]' | jq -c .accepted`
Enter
Wait
Type "# Keep pushing every 5 seconds while the lab is measured, so a later push settles the window's last"
Enter
Wait
Type `(while :; do omniglass telemetry push --owner '{"kind":"system","ref":"hq-huddle-room-1"}' --metrics '[{"name":"occupancy-count","value":3}]' --properties '[{"name":"occupancy-state","value":"occupied"}]' >/dev/null 2>&1; sleep 5; done) & pusher=$!`
Enter
Wait
Hide
Type `demo_hush 'omniglass lab measure --systems 0 --warmup 10s --window 40s --fleet-reads 1 --backlog 0 --json /tmp/push-lane.json >/tmp/push-lane.txt 2>/tmp/push-lane.err'`
Enter
Wait@300s
Show
Type "# Measured unseen while it pushed: omniglass lab measure --systems 0 --warmup 10s --window 40s"
Enter
Wait
Type "# The delivery table: the node lane first, then the push lane, each kind across the ingest hop"
Enter
Wait
Type "sed -n '/^delivery /,$p' /tmp/push-lane.txt"
Enter
Wait
Type "# The same rows in the JSON, the lane on each: every pushed item stored, a loss of 0"
Enter
Wait
Type `jq -c '.delivery[] | select(.lane == "push") | {lane, kind, hop, in, out, loss, deduplicated}' /tmp/push-lane.json`
Enter
Wait

Hide
Type `demo_hush 'kill "$pusher"; kill -INT "$scale"; wait "$scale"'`
Enter
Wait@120s
Show
