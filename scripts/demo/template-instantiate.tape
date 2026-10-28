# A component, a system and a floor start from templates
#
# Three sizes, one primitive: each call turns a template into one plan and
# lands it whole, every piece the ordinary create in the caller's scope with
# its own audit row, and nothing points back at the template (#1326, #1327,
# #1328).

Type "# A floor of meeting rooms under the West Building, systems and all"
Enter
Wait
Type `omniglass location instantiate --template meeting-floor --parent hq.west --name level-7 --label 'Level 7' | jq -c '{places: [.places[] | "\(.parent)/\(.location) (\(.type))"], systems: [.systems[] | "\(.location): \(.standard)"]}'`
Enter
Wait

Type "# One more system, at a room, with its touch panel attached on the node"
Enter
Wait
Type `omniglass system instantiate --template mr55-system --location hq.west.level-7.mr-1 --name mr-1-b --node edge-hq --attach '{"touch-control":[{"host":"127.0.0.9"}]}' | jq -c '{system, standard, standard_forked, components: [.components[] | "\(.role) \(.attached)"]}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# Its staffing reads complete: every role filled to quorum"
Enter
Wait
Type `omniglass system role list mr-1-b | jq -r '.roles[] | "\(.name): quorum \(.quorum), understaffed \(.understaffed)"'`
Enter
Wait
Type "# A single component, unattached until it is given an address"
Enter
Wait
Type `omniglass component instantiate --template boreal-edge-55 --location hq.west.level-7 | jq -c '{component, product, attached}'`
Enter
Wait
Type "# A floor cannot sit under a room: the types refuse the pair"
Enter
Wait
Type `omniglass location instantiate --template meeting-floor --parent hq.west.level-7.mr-1 --name level-9 2>/dev/null | jq -r .detail || true`
Enter
Wait
