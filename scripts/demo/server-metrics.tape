# The server reports its request and query timing on its own platform component
#
# Each server identity (OMNIGLASS_SERVER_NAME, else the hostname) gets an omniglass-server component
# in the reserved platform system. Each report window (collection.report_seconds) the server lands
# its HTTP requests there by route template and status class, its database pool's gauges, and a
# count of queries past database.slow_query_ms (#1627, ADR-1627).

Type "# The platform system now holds the server beside the node"
Enter
Wait
Type `omniglass system member list platform | jq -c '.members[] | {component, primary}'`
Enter
Wait
Type `omniglass component get server | jq -c '{name, label, product}'`
Enter
Wait

Hide
Type `demo_until 150 'omniglass component metric list server | jq -e "[.metrics[] | select(.metric_type_name == \"http-request-count\" or .metric_type_name == \"db-pool-max\")] | length == 2"'`
Enter
Wait@180s
Type "clear"
Enter
Wait
Show
Type "# One report window in: request timing and the database pool, as plain metrics"
Enter
Wait
Type `omniglass component metric list server | jq -c '[.metrics[].metric_type_name] | unique'`
Enter
Wait
Type "# The busiest routes in the latest window, keyed by route template and status class"
Enter
Wait
Type `omniglass component metric sample list server http-request-count | jq -r '(.samples | map(.ts) | max) as $t | [.samples[] | select(.ts == $t)] | sort_by(-.value) | .[0:5][] | "\(.value)\t\(.instance)"'`
Enter
Wait
Type "# The pool at the end of the window: in use, idle, open and the ceiling"
Enter
Wait
Type `omniglass component metric list server | jq -c '[.metrics[] | select(.metric_type_name | test("^db-pool-(acquired|idle|total|max)$")) | {(.metric_type_name): .value}] | add'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# A query past this threshold counts in db-slow-query-count and is logged with its fingerprint"
Enter
Wait
Type `omniglass setting list | jq -c '.values.database'`
Enter
Wait
