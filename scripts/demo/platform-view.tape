# Read Omniglass's own health in one call
#
# omniglass view platform list (GET /views/platform, gated node:read) is the read behind the
# Platform health page: the platform system's verdict, the build each server runs and when it
# started, the node table's column groups with each key's catalog facts, and one row per node with
# its current platform figures. A key the node never reported is absent, never 0 (#1644, ADR-1644).

Hide
Type `demo_until 150 'omniglass view platform list | jq -e "[.nodes[] | select(.name == \"edge-hq\") | .figures[\"checkin-age\"]] | length == 1"'`
Enter
Wait@180s
Type "clear"
Enter
Wait
Show
Type "# One call: the platform system's verdict, and whether this caller reaches the figures"
Enter
Wait
Type `omniglass view platform list | jq -c '{lab, system: (.system | {name, verdict}), figures_read}'`
Enter
Wait
Type "# The build each server runs, and how long it has been up at the server's own read time"
Enter
Wait
Type `omniglass view platform list | jq -c '.read_at as $r | .servers[] | {name, version, up_seconds: (($r[0:19] + "Z" | fromdate) - (.started_at[0:19] + "Z" | fromdate))}'`
Enter
Wait
Type "# The node table's columns are the view's groups: the page holds no key list of its own"
Enter
Wait
Type `omniglass view platform list | jq -c '[.groups[] | {(.label): [.keys[].name]}] | add'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# One row per node: its verdict and its current figures"
Enter
Wait
Type `omniglass view platform list | jq -c '.nodes[] | {name, verdict, figures: (.figures | map_values(.value))}'`
Enter
Wait
Type "# A key the node has not reported is absent, never 0: the page reads it as not reported"
Enter
Wait
Type `omniglass view platform list | jq -c '[.groups[].keys[].name] as $k | .nodes[] | {name, not_reported: ($k - (.figures | keys))}'`
Enter
Wait
