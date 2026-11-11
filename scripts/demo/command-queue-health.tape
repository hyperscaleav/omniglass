# A node's command queue reports its health
#
# Each node's command queue lands on its platform component as plain metrics: how many commands
# wait, how old the oldest is, how long a take and a run took, and how many a pull redelivered. A
# command keeps its first delivery and its delivery count, so a held one tells its story. Here the
# huddle bar answers one command at once; then, unplugged while its session is still connecting, it
# holds the next. That command's 15 s settle window closes before the bar returns, so it settles
# failed on what the bar still reports, and a settled command is never delivered again: the bar
# comes back without acting on it late, and the queue drains to zero (#1626, ADR-1630).

Hide
Type `bar='hq.west.level-2.huddle.$comp.videobar-1'`
Enter
Wait
Type "clear"
Enter
Wait
Show
Type "# The huddle bar is up: edge-hq takes a command, runs it and reports it"
Enter
Wait
Type `omniglass component command issue "$bar" --command-type set-volume --value 30 | jq -c '{command_type, status, node}'`
Enter
Wait
Hide
Type `demo_until 90 'omniglass component command list "$bar" --limit 1 | jq -e ".commands[0].executed_at != null"'`
Enter
Wait@120s
Show
Type `omniglass component command list "$bar" --limit 1 | jq -c '.commands[0] | {status, delivery_count, executed: (.executed_at != null)}'`
Enter
Wait

Hide
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
Type "# Now the bar is unplugged, and its session is still trying to connect"
Enter
Wait
Type `omniglass component reachability list "$bar" | jq -c '.endpoints[] | {endpoint, session: .session.state}'`
Enter
Wait
Type "# A command issued now: edge-hq takes it and holds it until the session is up"
Enter
Wait
Type `omniglass component command issue "$bar" --command-type set-volume --value 34 | jq -c '{command_type, status, node}'`
Enter
Wait

Hide
Type `demo_until 60 'omniglass component command list "$bar" --limit 1 | jq -e ".commands[0].delivery_count == 1 and .commands[0].status == \"failed\" and .commands[0].executed_at == null"'`
Enter
Wait@90s
Type "clear"
Enter
Wait
Show
Type "# Taken once and never run: its 15 s settle window closed first, so it settles failed"
Enter
Wait
Type `omniglass component command list "$bar" --limit 1 | jq -c '.commands[0] | {status, delivery_count, executed: (.executed_at != null)}'`
Enter
Wait
Type `omniglass component command list "$bar" --limit 1 | jq -r '.commands[0].reason'`
Enter
Wait

Hide
Type `demo_hush 'omniglass component impairment set "$bar"'`
Enter
Wait
Type `demo_until 90 'omniglass component reachability list "$bar" | jq -e ".endpoints[] | select(.endpoint == \"https\") | .session.state == \"up\""'`
Enter
Wait@120s
Type `demo_until 60 'omniglass component metric list edge-hq | jq -e "[.metrics[] | select(.metric_type_name == \"command-pending-count\" or .metric_type_name == \"command-pending-age\") | .value] | length == 2 and all(. == 0)"'`
Enter
Wait@90s
Type "clear"
Enter
Wait
Show
Type "# The bar is back, and a settled command is never delivered again: it was not run late"
Enter
Wait
Type `omniglass component command list "$bar" --limit 1 | jq -c '.commands[0] | {status, delivery_count, executed: (.executed_at != null)}'`
Enter
Wait
Type `omniglass component metric list "$bar" | jq -c '.metrics[] | select(.metric_type_name == "volume") | {volume: .value}'`
Enter
Wait
Type "# The node's platform component carries the queue: depth, oldest age, latencies, redeliveries"
Enter
Wait
Type `omniglass component metric list edge-hq | jq -c '[.metrics[] | select(.metric_type_name | startswith("command-")) | {(.metric_type_name): (.value | floor)}] | add'`
Enter
Wait
