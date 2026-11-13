# Read the closed loop with a simulator connected
#
# omniglass view platform simulation list (GET /views/platform/simulation, gated simulation:read)
# is the read behind Platform health's simulation section: one row per node with a connected
# simulator, read over a sliding window the server holds in memory. Every hop a request or a batch
# travels reads as a loss ratio over what went into it, 0 healthy, null when nothing did, beside
# what each protocol and each simulated device saw (#1647, ADR-1647).

Hide
Type `demo_until 150 'omniglass view platform simulation list | jq -e "[.nodes[] | select(.state == \"measured\")] | length >= 1"'`
Enter
Wait@180s
Type "clear"
Enter
Wait
Show
Type "# The server brackets each node with a simulator, and reads the newest bracket against the oldest"
Enter
Wait
Type `omniglass view platform simulation list | jq -c '{bracket_seconds, window_seconds, nodes: [.nodes[] | {node, state, brackets}]}'`
Enter
Wait
Type "# Every hop as a loss ratio over what went into it: 0 is healthy, null means nothing went in"
Enter
Wait
Type `omniglass view platform simulation list | jq -c '.nodes[0].gaps | group_by(.gap) | map({gap: .[0].gap, protocols: length, worst_ratio: ([.[].ratio | select(. != null)] | max)}) | .[]'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# What each protocol drew over the window: connections accepted, requests received and answered"
Enter
Wait
Type `omniglass view platform simulation list | jq -c '.nodes[0].protocols[] | {protocol, poll_targets, accepted, received, answered}'`
Enter
Wait
Type "# And each simulated device: a poll target the simulator never saw reads unhit"
Enter
Wait
Type `omniglass view platform simulation list | jq -c '.nodes[0].devices | {devices: length, impaired: map(select(.impaired)) | length, unhit: map(select(.unhit)) | length}'`
Enter
Wait
