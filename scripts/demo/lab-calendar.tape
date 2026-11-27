# The lab calendar shows what the simulator intends
#
# Every simulated room acts out meetings off one lab calendar, a pure function of the scenario's knobs
# and a seed. A lab server serves that same calendar at GET /lab/calendar, the ground truth to lay beside
# what Omniglass detected (#2770). It is derived by the server from the lab's own world, never asked of a
# simulator, and limited to the rooms the caller's system read scope reaches.

# demo-needs: docker-socket

Hide
Type `demo_hush 'omniglass lab run --scenario corporate --systems 3 --port 0 --ready-file /tmp/scale.env >/tmp/scale.log 2>&1 & scale=$!'`
Enter
Wait
Type `demo_until 170 'test -s /tmp/scale.env' || demo_hush 'tail -30 /tmp/scale.log'`
Enter
Wait@180s
Type `demo_hush '. /tmp/scale.env; export OMNIGLASS_SERVER=$LAB_HOST_URL OMNIGLASS_TOKEN=$LAB_TOKEN'`
Enter
Wait
Type `demo_hush 'day=$(date -u +%Y-%m-%d); start=${day}T00:00:00Z; end=$(date -u -d "$day +1 day" +%Y-%m-%dT00:00:00Z)'`
Enter
Wait
Type "clear"
Enter
Wait
Show

Type "# What the simulator intends today, room by room, read from the lab server"
Enter
Wait
Type `omniglass lab calendar list --start "$start" --end "$end" | jq -c '.entries | group_by(.system) | .[] | {system: .[0].system, room: .[0].room, entries: length}'`
Enter
Wait
Type "# One room's day: when each meeting ran, who organized it and how it went"
Enter
Wait
Type `sys=$(omniglass lab calendar list --start "$start" --end "$end" | jq -r '.entries[0].system')`
Enter
Wait
Type `omniglass lab calendar list --start "$start" --end "$end" --system "$sys" | jq -c '.entries[:5][] | {start, end, kind, outcome: .payload.outcome, organizer: .payload.organizer, actual: .payload.actual}'`
Enter
Wait
Type "# A window past 31 days is refused, naming the bound"
Enter
Wait
Type `omniglass lab calendar list --start "$start" --end "2099-01-01T00:00:00Z" 2>&1 | grep -o 'the window is at most.*'`
Enter
Wait

Hide
Type `demo_hush 'kill -INT "$scale"; wait "$scale"'`
Enter
Wait@120s
Show
