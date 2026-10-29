# Snooze an alarm's condition to suppress its notifications
#
# A snooze is a fact about an alarm's condition (its component and dedup key), not about one alarm
# row. Ops is told in the console inbox about anything on Touch Panel 2. Its touch surface flaps, so
# ops snoozes the condition for an hour: the clear and the re-raise are written suppressed on the
# alarm's timeline and nothing reaches the inbox, while another condition on the panel still does.
# Snoozing neither acknowledges nor moves health, a viewer cannot snooze, and a snooze lasts one
# minute to seven days. Unsnoozing early tells ops again about the condition that is still raised
# (#1318).

Hide
Type `demo_hush 'omniglass principal create --username ops --email ops@example.com && omniglass principal grant create ops --role operator --scope-kind all'`
Enter
Wait
Type `demo_hush 'omniglass principal create --username vic && omniglass principal grant create vic --role viewer --scope-kind all'`
Enter
Wait
Type `demo_hush 'vic=$(omniglass principal impersonate vic --mode act_as | jq -r .token)'`
Enter
Wait
Type `demo_hush 'export OMNIGLASS_TOKEN=$(omniglass principal impersonate ops --mode act_as | jq -r .token)'`
Enter
Wait
Type "clear"
Enter
Wait
Show

Type "# ops is told in the console inbox about every alarm on Touch Panel 2, raised or cleared"
Enter
Wait
Type `omniglass auth me action-rule create --name panel-2-watch --on '["raised","cleared"]' --when 'component == "panel-2"' --channels '["inbox"]' | jq -c '{name, on, channels}'`
Enter
Wait
Type "# The touch surface drops out, and ops is told"
Enter
Wait
Type `a=$(omniglass component alarm create panel-2 --severity high --message 'touch unresponsive' --dedup-key touch | jq -r .id)`
Enter
Wait
Type `omniglass auth me notification list | jq -c '{unread, inbox: [.notifications[] | .subject]}'`
Enter
Wait
Type "# Snooze the condition for an hour: not acknowledged, and the panel is still in outage"
Enter
Wait
Type `omniglass component alarm snooze panel-2 "$a" --duration 1h | jq -c '{dedup_key, snoozed_by, minutes_left: (((.snoozed_until[0:19] + "Z" | fromdateiso8601) - now) / 60 | round), acknowledged}'`
Enter
Wait
Type `omniglass component alarm list panel-2 | jq -r .verdict`
Enter
Wait
Type "# It flaps: clears, then comes back as a new alarm of the same condition, which reads the snooze too"
Enter
Wait
Type `omniglass component alarm delete panel-2 "$a"`
Enter
Wait
Type `b=$(omniglass component alarm create panel-2 --severity high --message 'touch unresponsive' --dedup-key touch | jq -r .id)`
Enter
Wait
Type `omniglass component alarm list panel-2 | jq -c '.alarms[] | {dedup_key, snoozed_by}'`
Enter
Wait
Type "# Both edges were written suppressed on the timeline, and nothing reached the inbox"
Enter
Wait
Type `omniglass component alarm timeline panel-2 "$a" | jq -c '.entries[] | select(.what != "queued") | {kind, what, actor, transition, error}' | tail -3`
Enter
Wait
Type `omniglass component alarm timeline panel-2 "$b" | jq -c '.entries[] | select(.kind == "notification") | {what, transition, error}'`
Enter
Wait
Type `omniglass auth me notification list | jq -c '{unread}'`
Enter
Wait
Type "# Another condition on the panel is not snoozed, and still tells ops"
Enter
Wait
Type `omniglass component alarm create panel-2 --severity warning --message 'fan noise' --dedup-key fan | jq -c '{dedup_key, snoozed_until}'`
Enter
Wait
Type `omniglass auth me notification list | jq -c '{unread, newest: .notifications[0].subject}'`
Enter
Wait
Type "# A viewer cannot snooze, and a snooze lasts one minute to seven days"
Enter
Wait
Type `OMNIGLASS_TOKEN=$vic omniglass component alarm snooze panel-2 "$b" --duration 1h 2>/dev/null | jq -r .detail || true`
Enter
Wait
Type `omniglass component alarm snooze panel-2 "$b" --duration 30d 2>/dev/null | jq -r .detail || true`
Enter
Wait
Type "# Unsnooze ends it early: the touch condition is still raised, so ops is told again"
Enter
Wait
Type `omniglass component alarm unsnooze panel-2 "$b" | jq -c '{dedup_key, snoozed_until, active}'`
Enter
Wait
Type `omniglass component alarm timeline panel-2 "$b" | jq -c '.entries[-2:][] | {kind, what, actor}'`
Enter
Wait
Type `omniglass auth me notification list | jq -c '{unread, newest: .notifications[0].subject}'`
Enter
Wait
