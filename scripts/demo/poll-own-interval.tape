# Each poll and probe runs on its own interval
#
# A node's heartbeat is its check-in, nothing more. Every driver poll runs on its own schedule.every
# and every reachability probe on --probe-every, from a dispatcher with a timer of its own, at a
# stable phase, through one lane per endpoint on a bounded worker pool. A due whose last run is still
# in flight is skipped and counted, never queued; each endpoint reports its dues every window (#1634).

Type "# The node's knobs: the heartbeat is its check-in; probes and polls keep their own time"
Enter
Wait
Type `omniglass node run --help | grep -E -- '--(heartbeat|probe-every|poll-workers) '`
Enter
Wait
Type "# A probe interval of zero meant 'every check-in'; it now has no meaning, so it is refused"
Enter
Wait
Type `omniglass node run --name demo --token demo --probe-every 0 2>&1 || true`
Enter
Wait

Type "# The boardroom's power conditioner: its driver polls its status every 30s, on a node that checks in every 2s"
Enter
Wait
Type `pdu='hq.west.level-2.boardroom-a.$comp.power-1'`
Enter
Wait
Hide
Type `demo_until 240 'omniglass component metric sample list "$pdu" poll-due-count | jq -e "[.samples[]] | length >= 2"'`
Enter
Wait@260s
Type "clear"
Enter
Wait
Show
Type "# Its last four voltage readings: thirty seconds apart, at a phase of the poll's own, not the 2s check-in's"
Enter
Wait
Type `omniglass component metric sample list "$pdu" voltage | jq -c 'def secs: (.[0:19] + "Z" | fromdate) + (.[19:-1] as $f | if $f == "" then 0 else ("0" + $f | tonumber) end); .samples[:4] | map(.ts) as $t | {ts: $t, seconds_apart: [range(1; $t | length) as $i | (($t[$i-1] | secs) - ($t[$i] | secs)) * 1000 | round / 1000]}'`
Enter
Wait
Type "# Its endpoint's account of the schedule: the polls that fell due each one-minute window"
Enter
Wait
Type `omniglass component metric sample list "$pdu" poll-due-count | jq -c '.samples[:3][] | {ts, instance, value}'`
Enter
Wait
