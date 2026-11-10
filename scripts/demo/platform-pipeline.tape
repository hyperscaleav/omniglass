# Read where work is lost, hop by hop
#
# omniglass view platform list (GET /views/platform, gated node:read) now serves the pipeline
# behind Platform health: the hops an item crosses from its run to the store, in order, each read
# from the same three delivery keys, so one rule reads every hop's loss, (in - out) / in, 0 healthy.
# The end to end composes them, and each node's Loss composes the hops its own component records
# (#1646, ADR-1646).

Hide
Type `demo_until 150 'omniglass view platform list | jq -e "([.pipeline.hops[] | select(.total.last_hour.in > 0 and .total.last_hour.loss == 0)] | length == 2) and ([.nodes[] | select(.name == \"edge-hq\" and .loss == 0)] | length == 1)"'`
Enter
Wait@180s
Type "clear"
Enter
Wait
Show
Type "# The hops an item crosses, in order, and the platform components that record each"
Enter
Wait
Type `omniglass view platform list | jq -c '.pipeline.hops[] | {id, label, component_class}'`
Enter
Wait
Type "# Every hop is read from the same three keys, so one rule reads them all"
Enter
Wait
Type `omniglass view platform list | jq -c '[.pipeline.hops[] | {(.id): .keys}] | add'`
Enter
Wait
Type "# Each hop's loss over the last hour, every kind summed: the share it took in and did not hand on"
Enter
Wait
Type `omniglass view platform list | jq -c '.pipeline.hops[] | {id, took_in: (.total.last_hour.in > 0), loss: .total.last_hour.loss}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# The end to end: one minus the product of every hop's survival, 0 when nothing was lost"
Enter
Wait
Type `omniglass view platform list | jq -c '.pipeline.end_to_end_total'`
Enter
Wait
Type "# A node's Loss composes the hops its own component records, shown on its row"
Enter
Wait
Type `omniglass view platform list | jq -c '{loss_hops, nodes: [.nodes[] | {name, loss}]}'`
Enter
Wait
