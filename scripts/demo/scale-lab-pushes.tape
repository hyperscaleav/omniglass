# A scale lab pushes on its own: each site's booking service and meeting platform report unasked
#
# A corporate lab declares one booking service a site (#1743). Each run mints every pusher its
# token, the seed grants its service principal telemetry:push on its site's systems and nothing
# else, and the simulator on the site's node posts each system's bookings to POST /telemetry:push
# every 30 seconds. It also declares one meeting platform a site (#1747): a component with a signed
# webhook endpoint, a signing key minted for the run and never shown, and the site's systems bound
# as its rooms, which the simulator adds and reports on every 30 seconds. Nobody pushes by hand
# here, yet lab measure reads the push lane, bookings and webhook posts both, at a loss of 0.

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

Type "# The scenario declares its outside actors like its devices: a booking service and a meeting platform at each of 3 sites"
Enter
Wait
Type "omniglass lab run --scenario corporate --systems 3 --plan | sed -n '/^PUSHER/,/^$/p'"
Enter
Wait
Type "# Each is a service principal that may push to its site's systems and do nothing else"
Enter
Wait
Type `omniglass principal list | jq -c '.principals[] | select(.service.name // "" | test("-booking-")) | {service: .service.name, grants: [.grants[] | {role, scope_kind}]}'`
Enter
Wait
Type "# Nobody pushes by hand: the simulator posts each room's bookings with that principal's token"
Enter
Wait

Hide
Type `demo_until 90 'omniglass system metric list hq-huddle-room-1 | jq -e "[.metrics[] | select(.metric_type_name == \"booking-count\" and .is_sampled)] | length == 1"'`
Enter
Wait@100s
Show

Type `omniglass system metric list hq-huddle-room-1 | jq -c '.metrics[] | select(.metric_type_name == "booking-count") | {metric: .metric_type_name, sampled: .is_sampled}'`
Enter
Wait
Type "# Each site's meeting platform is a component with a signed webhook endpoint; its key is named, never shown"
Enter
Wait
Type `omniglass endpoint list | jq -c '.endpoints[] | select(.driver == "ucc-webhook") | {driver, inputs}'`
Enter
Wait
Type "# Each key was minted for this run alone and sealed in the server"
Enter
Wait
Type `omniglass secret list | jq -c '.secrets[] | select(.name | endswith("-signing")) | {name, secret_type, owner_kind}'`
Enter
Wait
Type "# Each site's systems are bound as the platform's rooms, so each room's post lands on its system"
Enter
Wait
Type `for id in $(omniglass endpoint list | jq -r '.endpoints[] | select(.driver == "ucc-webhook") | .component_id'); do omniglass component identity-binding list "$id" | jq -c '[.bindings[] | {room: .value, system: .owner_name}]'; done`
Enter
Wait
Type "# lab measure reads the push lane: a window long enough for a later push to settle the first"
Enter
Wait
Type "omniglass lab measure --systems 0 --warmup 10s --window 70s --fleet-reads 1 --backlog 0 --json /tmp/push-lane.json >/tmp/push-lane.txt 2>/tmp/push-lane.err"
Enter
Wait@300s
Type "# Every booking and every signed platform post was stored: each push-lane row reads a loss of 0"
Enter
Wait
Type `jq -c '.delivery[] | select(.lane == "push") | {lane, kind, hop, in, out, loss}' /tmp/push-lane.json`
Enter
Wait

Hide
Type `demo_hush 'kill -INT "$scale"; wait "$scale"'`
Enter
Wait@120s
Show
