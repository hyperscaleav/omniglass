# Snooze an alarm's condition
#
# A snooze is a fact about an alarm's condition (its component and dedup key), not about one alarm
# row: an operator snoozes Touch Panel 2's flapping touch alarm for an hour from any alarm of that
# condition, and every alarm of it reads the snooze, the next re-raise included, while another
# condition on the panel does not. Snoozing neither acknowledges nor moves health; it is audited on
# the alarm's timeline. A viewer is refused, and a duration outside one minute to seven days is
# refused naming the bound (#1318).

Hide
Type `demo_hush 'omniglass principal create --username vic && omniglass principal grant create vic --role viewer --scope-kind all'`
Enter
Wait
Type `demo_hush 'vic=$(omniglass principal impersonate vic --mode act_as | jq -r .token)'`
Enter
Wait
Type "clear"
Enter
Wait
Show

Type "# Touch Panel 2's touch surface keeps dropping out"
Enter
Wait
Type `a=$(omniglass component alarm create panel-2 --severity high --message 'touch unresponsive' --dedup-key touch | jq -r .id)`
Enter
Wait
Type "# Snooze its condition for an hour: it is not acknowledged, and the panel is still in outage"
Enter
Wait
Type `omniglass component alarm snooze panel-2 "$a" --duration 1h | jq -c '{dedup_key, snoozed_by, minutes_left: (((.snoozed_until[0:19] + "Z" | fromdateiso8601) - now) / 60 | round), acknowledged, active}'`
Enter
Wait
Type `omniglass component alarm list panel-2 | jq -r .verdict`
Enter
Wait
Type "# It clears and comes back as a new alarm: the snooze is on the condition, so the new one reads it too"
Enter
Wait
Type `omniglass component alarm delete panel-2 "$a"`
Enter
Wait
Type `b=$(omniglass component alarm create panel-2 --severity high --message 'touch unresponsive' --dedup-key touch | jq -r .id)`
Enter
Wait
Type `omniglass component alarm create panel-2 --severity warning --message 'fan noise' --dedup-key fan >/dev/null`
Enter
Wait
Type `omniglass component alarm list panel-2 | jq -c '.alarms[] | {dedup_key, snoozed_by}'`
Enter
Wait
Type "# The snooze is on the first alarm's timeline, by who set it"
Enter
Wait
Type `omniglass component alarm timeline panel-2 "$a" | jq -c '.entries[] | select(.kind == "audit") | {what, actor}'`
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
Type "# Unsnooze ends it early"
Enter
Wait
Type `omniglass component alarm unsnooze panel-2 "$b" | jq -c '{dedup_key, snoozed_until}'`
Enter
Wait
