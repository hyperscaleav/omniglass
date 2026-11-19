# What an outpost's endpoints saw arrive, beside what the outpost sent
#
# The simulator beside an outpost now counts every request that reaches its devices, per protocol:
# received, answered, refused, dropped, and the devices nothing reached. The outpost tallies what it
# sent on each transport. One read brackets the outpost's tally between two simulator reads, so a
# scale lab can show every request reached its endpoint and was answered (#1639).

Type "# The dev lab's outpost, edge-hq, and the simulator beside it"
Enter
Wait
Hide
Type `demo_until 120 'omniglass outpost simulation observation list edge-hq | jq -e "[.sim_after.protocols[] | select(.answered > 0)] | length > 1"'`
Enter
Wait@130s
Type "clear"
Enter
Wait
Show
Type "# What arrived at the simulated devices, summed per protocol"
Enter
Wait
Type `omniglass outpost simulation observation list edge-hq | jq -c '.sim_after.protocols[] | {protocol, devices: .served, received, answered, refused, unhit}'`
Enter
Wait

Type "# What the outpost tallied sending on each transport"
Enter
Wait
Type `omniglass outpost simulation observation list edge-hq | jq -c '.outpost.transports[] | {transport, sent, answered, refused, timed_out}'`
Enter
Wait

Type "# The outpost's read sits between two simulator reads, so a request in transit is never a false gap"
Enter
Wait
Type `omniglass outpost simulation observation list edge-hq | jq -c '{sim_before: .sim_before.at, outpost: .outpost.at, sim_after: .sim_after.at}'`
Enter
Wait
