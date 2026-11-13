# The server's retention pass reports on its own platform component
#
# The boot ensures the server's platform component before the bus starts and hands its id to the
# retention pass, so the pass the boot runs already lands its figures there: per lane the rows it
# removed and whether it stopped behind, the pass's duration and failures, and the table sizes
# (#1671, ADR-1671). A boot whose ensure fails logs a WARN and serves anyway, reporting none of these.

Type "# The boot ensured the server's platform component before the bus started"
Enter
Wait
Type `grep -m1 -o 'msg="server platform component" server=[^ ]*' /stack/logs/server.log`
Enter
Wait
Type `omniglass component get server | jq -c '{name, label, product}'`
Enter
Wait

Hide
Type `demo_until 150 'omniglass component metric list server | jq -e "any(.metrics[]; .metric_type_name == \"retention-pass-duration\")"'`
Enter
Wait@180s
Type "clear"
Enter
Wait
Show
Type "# The pass the boot ran landed its figures on that component"
Enter
Wait
Type `omniglass component metric list server | jq -c '[.metrics[].metric_type_name | select(test("^(retention-|metric-|table-bytes|database-bytes)"))] | unique'`
Enter
Wait
Type "# Behind per lane: 0 is a lane that reached its window, the healthy reading"
Enter
Wait
Type `omniglass component metric list server | jq -c '.metrics[] | select(.metric_type_name == "retention-behind") | [.instances[] | {(.instance): .value}] | add'`
Enter
Wait
Type "# The pass's own account: how long it took and how many prunes failed"
Enter
Wait
Type `omniglass component metric list server | jq -c '[.metrics[] | select(.metric_type_name | test("^retention-pass-")) | {(.metric_type_name): .value}] | add'`
Enter
Wait
