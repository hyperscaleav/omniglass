# One cloud service, many owners: the fan-out
#
# The meeting platform's account-wide service (a conduit) reports every room's call state in one
# answer. Each room is named by its ucc-room-id, and the conduit's identity bindings say which
# system each room is: the node's poll of its room list and the service's webhook report both land
# each room's value on its own system, observed, its source naming the conduit. The room nobody
# bound lands on no system and waits in the conduit's unbound inbox; binding it makes its next value
# land on its system (#1282, ADR-1282).

Type "# The meeting platform's account-wide service at headquarters: the conduit"
Enter
Wait
Type `c='hq.$comp.ucc-platform'`
Enter
Wait
Type `huddle='hq.west.level-2.huddle.$sys.huddle'; board_a='hq.west.level-2.boardroom-a.$sys.boardroom'; board_b='hq.west.level-2.boardroom-b.$sys.boardroom'`
Enter
Wait
Type "# Its identity bindings: which system each of its room ids is"
Enter
Wait
Type `omniglass component identity-binding list "$c" | jq -c '.bindings[] | {key, value, owner: .owner_name}'`
Enter
Wait

Hide
Type `demo_until 120 'omniglass system property list "$huddle" | jq -e "[.properties[] | select(.property_type_name == \"call-state\" and .reconciliation.is != null)] | length > 0"'`
Enter
Wait@130s
Type "clear"
Enter
Wait
Show
Type "# The node polled the room list once: each room's call state landed on its own system"
Enter
Wait
Type `for s in "$board_a" "$huddle"; do omniglass system property sample list "$s" call-state --limit 1 | jq -c --arg s "$s" '.samples[0] | {system: $s, value, instance, source}'; done`
Enter
Wait
Type "# Nothing about a room landed on the conduit itself"
Enter
Wait
Type `omniglass component property list "$c" | jq -c '[.properties[] | select(.property_type_name == "call-state")] | length'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# A room the platform knows and nobody bound: kept in the conduit's unbound inbox"
Enter
Wait
Type `omniglass component unbound-identity list "$c" | jq -c '.identities[] | {key, value, count, keys_carried}'`
Enter
Wait
Type "# Bind it to its system: this costs update on the conduit and on the system"
Enter
Wait
Type `omniglass component identity-binding update "$c" ucc-room-id zr-boardroom-b --owner-kind system --owner "$board_b" | jq -c '{key, value, owner: .owner_name}'`
Enter
Wait
Type `omniglass component unbound-identity list "$c" | jq -c '.identities | length'`
Enter
Wait

Hide
Type `demo_hush 'url="$OMNIGLASS_SERVER/api/v1/webhooks/$(omniglass secret reveal "$c/webhook-token" | jq -r .fields.token)"'`
Enter
Wait
Show
Type "# The same service's webhook: subscribe it (the token stays off screen) and post one report naming every room"
Enter
Wait
Type `omniglass component simulation fire "$c" --surface http-api --method POST --path /api/webhooks --body "{\"url\":\"$url\",\"scheme\":\"hmac\",\"secret\":\"ucc-lab-signing-key\"}" | jq -c .status`
Enter
Wait
Type `omniglass component simulation fire "$c" --surface http-api --method POST --path /api/rooms/report --body '{}' | jq -c .status`
Enter
Wait
Hide
Type `demo_until 30 'omniglass system property sample list "$board_b" call-state --limit 1 | jq -e ".samples | length > 0"'`
Enter
Wait@40s
Show
Type "# The newly bound room's value landed on its system, through the same fan-out"
Enter
Wait
Type `omniglass system property sample list "$board_b" call-state --limit 1 | jq -c '.samples[0] | {value, instance, source}'`
Enter
Wait
