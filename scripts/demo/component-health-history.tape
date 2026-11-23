# Read a component's health history over a time range
#
# A component's own health is recorded as a transition each time its alarms move it. The health
# read serves the same envelope a system's does: the current health and the recorded edges, oldest
# first. Ops raises an alarm on Touch Panel 2 and reads the edge, then narrows the range with
# --since and --until: a range that holds no edge is empty, and one that ends before it starts is
# refused (#2305).

Hide
Type `demo_hush 'omniglass user create --username ops --email ops@example.com && omniglass user grant create ops --role operator --scope-kind all'`
Enter
Wait
Type `demo_hush 'export OMNIGLASS_TOKEN=$(omniglass user impersonate ops --mode act_as | jq -r .token)'`
Enter
Wait
Type "clear"
Enter
Wait
Show

Type "# Touch Panel 2 drops out"
Enter
Wait
Type `a=$(omniglass component alarm create panel-2 --severity high --message 'touch unresponsive' --dedup-key touch | jq -r .id)`
Enter
Wait
Type "# Its health and the edges that got it there, over the default last 30 days"
Enter
Wait
Type `omniglass component health list panel-2 | jq -c '{owner_kind, health, edges: [.transitions[].health]}'`
Enter
Wait
Type "# A range that ended an hour ago holds no edge"
Enter
Wait
Type `omniglass component health list panel-2 --until "$(date -u -d '1 hour ago' +%FT%TZ)" | jq -c '{health, edges: [.transitions[].health]}'`
Enter
Wait
Type "# A range that ends before it starts is refused"
Enter
Wait
Type `omniglass component health list panel-2 --since "$(date -u +%FT%TZ)" --until "$(date -u -d '1 day ago' +%FT%TZ)" 2>&1 | head -3`
Enter
Wait
