# A node's command queue reports its health
#
# Each node's command queue lands on its platform component as plain metrics: how many commands
# wait, how old the oldest is, how long a take and a run took, and how many a pull redelivered. A
# command keeps its first delivery and its delivery count, so a redelivered one tells its story.
# Here the huddle bar is unplugged while its session is still connecting: edge-hq takes the command
# and holds it, no report comes back, and the queue delivers it again until the bar returns (#1626).

Hide
Type `bar='hq.west.level-2.huddle.$comp.videobar-1'`
Enter
Wait
Type `demo_hush 'omniglass component impairment set "$bar" --unplugged'`
Enter
Wait
Type `demo_hush 'omniglass endpoint update "$bar/https" --params "{\"timeout\":\"20s\"}"'`
Enter
Wait
Type `demo_until 90 'omniglass component reachability list "$bar" | jq -e ".endpoints[] | select(.endpoint == \"https\") | .session.state == \"establishing\""'`
Enter
Wait@120s
Type "clear"
Enter
Wait
Show
Type "# The huddle bar is unplugged, and its session is still trying to connect"
Enter
Wait
Type `omniglass component reachability list "$bar" | jq -c '.endpoints[] | {endpoint, session: .session.state}'`
Enter
Wait
Type "# A command issued now: edge-hq takes it and holds it until the session is up"
Enter
Wait
Type `omniglass component command issue "$bar" --command-type set-volume --value 34 | jq -c '{command_type, status, phase, node}'`
Enter
Wait

Hide
Type `demo_until 60 'omniglass component command list "$bar" --limit 1 | jq -e ".commands[0].delivery_count == 1"'`
Enter
Wait@90s
Show
Type `omniglass component command list "$bar" --limit 1 | jq -c '.commands[0] | {delivery_count, first_delivered: (.first_delivered_at != null), executed: (.executed_at != null)}'`
Enter
Wait

Hide
Type `demo_until 90 'omniglass component command list "$bar" --limit 1 | jq -e ".commands[0].delivery_count >= 2"'`
Enter
Wait@120s
Type `demo_until 60 'omniglass component metric sample list edge-hq command-redelivery-count --hours 1 | jq -e "[.samples[].value] | add >= 1" && omniglass component metric list edge-hq | jq -e "[.metrics[] | select(.metric_type_name == \"command-pending-count\") | .value] | length == 1 and all(. >= 1)"'`
Enter
Wait@90s
Type "clear"
Enter
Wait
Show
Type "# No report came back within 30 s, so the next pull delivered it again, keeping the first"
Enter
Wait
Type `omniglass component command list "$bar" --limit 1 | jq -c '.commands[0] | {delivery_count, first_kept: (.first_delivered_at != .delivered_at), executed: (.executed_at != null)}'`
Enter
Wait
Type "# Its 15 s settle window closed without the volume, so it reads timed-out, yet it still waits"
Enter
Wait
Type `omniglass component command list "$bar" --limit 1 | jq -c '.commands[0] | {status, reason}'`
Enter
Wait
Type "# The node's platform component carries the queue: depth, oldest age, latencies, redeliveries"
Enter
Wait
Type `omniglass component metric list edge-hq | jq -c '[.metrics[] | select(.metric_type_name | startswith("command-")) | {(.metric_type_name): (.value | floor)}] | add'`
Enter
Wait
Type "# Redeliveries count per 15 s window, zero when quiet, so the series keeps the window that saw it"
Enter
Wait
Type `omniglass component metric sample list edge-hq command-redelivery-count --hours 1 | jq -c '{windows: (.samples | length), redelivered: ([.samples[].value] | add)}'`
Enter
Wait

Hide
Type `demo_hush 'omniglass component impairment set "$bar"'`
Enter
Wait
Type `demo_until 90 'omniglass component command list "$bar" --limit 1 | jq -e ".commands[0].executed_at != null"'`
Enter
Wait@120s
Type `demo_until 60 'omniglass component metric list edge-hq | jq -e "[.metrics[] | select(.metric_type_name == \"command-pending-count\" or .metric_type_name == \"command-pending-age\") | .value] | length == 2 and all(. == 0)"'`
Enter
Wait@90s
Type "clear"
Enter
Wait
Show
Type "# The bar is back: the node runs the command and reports it, and the queue drains to zero"
Enter
Wait
Type `omniglass component command list "$bar" --limit 1 | jq -c '.commands[0] | {delivery_count, executed: (.executed_at != null)}'`
Enter
Wait
Type `omniglass component metric list edge-hq | jq -c '[.metrics[] | select(.metric_type_name | test("^command-pending-")) | {(.metric_type_name): .value}] | add'`
Enter
Wait
