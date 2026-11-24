# Plan a simulated device before adding it
#
# The add-a-component flow fills its endpoint from a plan: the address a new simulated device of a
# product would answer on, and the driver attach the simulator answers to. The plan creates and
# reserves nothing, so the flow can offer it before the operator commits (#2460).

Type "# What would a new simulated Boreal Edge 55 beside edge-hq get?"
Enter
Wait
Type `omniglass simulation plan-component edge-hq --product boreal-edge-55 | jq -c '{address, endpoints: [.endpoints[] | {driver, inputs}]}'`
Enter
Wait

Type "# Nothing was created: no component answers at that address yet"
Enter
Wait
Type `a=$(omniglass simulation plan-component edge-hq --product boreal-edge-55 | jq -r .address); omniglass simulation get | jq --arg a "$a" '[.outposts[].components[] | select(.address == $a)] | length'`
Enter
Wait

Type "# A product no simulator profile simulates is refused, naming why"
Enter
Wait
Type `omniglass simulation plan-component edge-hq --product generic-app 2>/dev/null | jq -r '"\(.status) \(.detail)"' || true`
Enter
Wait
