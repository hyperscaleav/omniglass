# What a node's endpoints saw arrive, beside what the node sent
#
# The simulator beside a node now counts every request that reaches its devices, per protocol:
# received, answered, refused, dropped, and the devices nothing reached. The node tallies what it
# sent on each transport. One read brackets the node's tally between two simulator reads, so a
# scale lab can show every request reached its endpoint and was answered (#1639).

Type "# The dev lab's node, edge-hq, and the simulator beside it"
Enter
Wait
Hide
Type `demo_until 120 'omniglass node simulation observation list edge-hq | jq -e "[.sim_after.protocols[] | select(.answered > 0)] | length > 1"'`
Enter
Wait@130s
Type "clear"
Enter
Wait
Show
Type "# What arrived at the simulated devices, summed per protocol"
Enter
Wait
Type `omniglass node simulation observation list edge-hq | jq -c '.sim_after.protocols[] | {protocol, devices: .served, received, answered, refused, unhit}'`
Enter
Wait

Type "# What the node tallied sending on each transport"
Enter
Wait
Type `omniglass node simulation observation list edge-hq | jq -c '.node.transports[] | {transport, sent, answered, refused, timed_out}'`
Enter
Wait

Type "# The node's read sits between two simulator reads, so a request in transit is never a false gap"
Enter
Wait
Type `omniglass node simulation observation list edge-hq | jq -c '{sim_before: .sim_before.at, node: .node.at, sim_after: .sim_after.at}'`
Enter
Wait
