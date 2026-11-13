# A node reports its schedule health on its own platform component
#
# Omniglass keeps a reserved, unplaced platform system holding one component per node. The node
# lands its own schedule and runtime readings on that component as plain metrics (no prefix on the
# keys), and the server writes checkin-age beside them every 15 s, the seconds since the node last
# checked in. The rows are Omniglass's own: an operator may relabel them, not rename them (#1625,
# ADR-1625).

Type "# The platform system: reserved to Omniglass, unplaced, one member per node"
Enter
Wait
Type `omniglass system get platform | jq -c '{name, system_type, member_count, path}'`
Enter
Wait
Type `omniglass system member list platform | jq -c '.members[] | {component, primary}'`
Enter
Wait

Hide
Type `demo_until 150 'omniglass component metric list edge-hq | jq -e "[.metrics[] | select(.metric_type_name == \"checkin-age\" or .metric_type_name == \"schedule-lateness\" or .metric_type_name == \"process-goroutine-count\")] | length == 3"'`
Enter
Wait@180s
Type "clear"
Enter
Wait
Show
Type "# The node edge-hq reports on its own component: schedule, spool, sessions and runtime"
Enter
Wait
Type `omniglass component metric list edge-hq | jq -c '[.metrics[].metric_type_name] | unique'`
Enter
Wait
Type "# How late each poll ran against its schedule, and the node's own process"
Enter
Wait
Type `omniglass component metric list edge-hq | jq -c '.metrics[] | select(.metric_type_name | test("^(schedule-lateness|schedule-gap-excess|process-goroutine-count)$")) | {metric: .metric_type_name, value}'`
Enter
Wait
Type "# The server writes checkin-age every 15 s: seconds since the node last checked in"
Enter
Wait
Type `omniglass component metric sample list edge-hq checkin-age | jq -c '{key: "checkin-age", rows: (.samples | length), fresh: all(.samples[0:3][]; .value < 60)}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# These rows are Omniglass's own: a rename is refused"
Enter
Wait
Type `omniglass system rename platform --name my-platform 2>/dev/null | jq -r .detail || true`
Enter
Wait
Type `omniglass component rename edge-hq --name my-node 2>/dev/null | jq -r .detail || true`
Enter
Wait
Type "# A relabel is the operator's to make"
Enter
Wait
Type `omniglass system update platform --label "Fleet platform" | jq -c '{name, label}'`
Enter
Wait
