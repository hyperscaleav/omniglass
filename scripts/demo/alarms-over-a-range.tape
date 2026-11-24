# Read the alarms raised during a time range
#
# The overlay timeline (#2425) reads a component's and a system's alarms over the page's range, so
# the alarm lists take --since and --until: an alarm is kept when it was raised by the range's end
# and was still raised at its start, so one open across the whole range is kept. Ops raises and
# clears an alarm on Touch Panel 2, then reads it back over a range that holds it, one that ended
# before it, and the Teaching 2 system's read over the last hour.

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

Type "# Touch Panel 2 drops out, and recovers"
Enter
Wait
Type `a=$(omniglass component alarm create panel-2 --severity high --message 'touch unresponsive' --dedup-key touch | jq -r .id)`
Enter
Wait
Type `omniglass component alarm delete panel-2 "$a"`
Enter
Wait
Type "# Over the last hour it is there, cleared, raise to clear"
Enter
Wait
Type `omniglass component alarm list panel-2 --include-cleared --since "$(date -u -d '1 hour ago' +%FT%TZ)" | jq -c '[.alarms[] | {message, raised_at, cleared_at}]'`
Enter
Wait
Type "# A range that ended an hour ago holds none of it"
Enter
Wait
Type `omniglass component alarm list panel-2 --include-cleared --since "$(date -u -d '2 hours ago' +%FT%TZ)" --until "$(date -u -d '1 hour ago' +%FT%TZ)" | jq -c '[.alarms[].message]'`
Enter
Wait
Type "# The system's read over the same hour names the member"
Enter
Wait
Type `omniglass system alarm list teaching-2 --since "$(date -u -d '1 hour ago' +%FT%TZ)" | jq -c '[.alarms[] | {component, message, cleared: (.cleared_at != null)}]'`
Enter
Wait
