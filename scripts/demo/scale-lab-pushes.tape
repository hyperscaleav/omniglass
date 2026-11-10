# A scale lab pushes on its own: each site's booking service is an emulator device
#
# A corporate lab declares one booking service a site (#1743). Each run mints every pusher its
# token, the seed grants its service principal telemetry:push on its site's systems and nothing
# else, and the simulator on the site's node posts each system's bookings to POST /telemetry:push
# every 30 seconds. Nobody pushes by hand here, yet lab measure reads the push lane at a loss of 0.

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

Type "# The scenario declares its pushers like its devices: one booking service at each of 3 sites"
Enter
Wait
Type "omniglass lab run --scenario corporate --systems 3 --plan | sed -n '/^PUSHER/,/^$/p'"
Enter
Wait
Type "# Each is a service principal that may push to its site's systems and do nothing else"
Enter
Wait
Type `omniglass principal list | jq -c '.principals[] | select(.service.name | test("-booking-")) | {service: .service.name, grants: [.grants[] | {role, scope_kind}]}'`
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
Type "# lab measure reads the push lane: a window long enough for a later push to settle the first"
Enter
Wait
Type "omniglass lab measure --systems 0 --warmup 10s --window 70s --fleet-reads 1 --backlog 0 --json /tmp/push-lane.json >/tmp/push-lane.txt 2>/tmp/push-lane.err"
Enter
Wait@300s
Type "# Every item the booking services pushed was stored: each push-lane row reads a loss of 0"
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
