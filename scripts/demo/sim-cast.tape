# The Boreal media player is simulated and collected
#
# The dev fleet's Boreal Cast M1 signage players now have a simulator profile and a driver of their own:
# an HTTP JSON API behind an api key, reporting playback, the item playing and how full the player's
# storage is, with a playlist that moves along while it plays. Each player carries signals of its class
# beside its identity and uptime (#893).

Type "# A depot bay's signage player, collected by its own driver"
Enter
Wait
Type `pl='depot.bay-1.$comp.player-1'`
Enter
Wait
Hide
Type `demo_until 120 'omniglass component property list "$pl" | jq -e "[.properties[] | select(.property_type_name == \"playback-state\" and .reconciliation.is != null)] | length == 1"'`
Enter
Wait@130s
Type "clear"
Enter
Wait
Show
Type "# What it is playing, and how full its storage is (a ratio)"
Enter
Wait
Type `omniglass component property list "$pl" | jq -c '.properties[] | select(.property_type_name | test("^(playback-state|content-name)$")) | {property: .property_type_name, is: .reconciliation.is}'`
Enter
Wait
Type `omniglass component metric list "$pl" | jq -c '.metrics[] | select(.metric_type_name == "storage-utilization") | {metric: .metric_type_name, endpoint: .instance, value: ((.value * 1000 | round) / 1000)}'`
Enter
Wait

Type "# Pause it over its API"
Enter
Wait
Type `omniglass component simulation fire "$pl" --surface http-api --method POST --path /api/playback --body '{"state": "paused"}' | jq -c '{status, body: (.body | fromjson)}'`
Enter
Wait
Hide
Type `demo_until 90 'omniglass component property list "$pl" | jq -e ".properties[] | select(.property_type_name == \"playback-state\") | .reconciliation.is == \"paused\""'`
Enter
Wait@100s
Type "clear"
Enter
Wait
Show
Type "# The next poll reads it paused, still on the same item"
Enter
Wait
Type `omniglass component property list "$pl" | jq -c '.properties[] | select(.property_type_name | test("^(playback-state|content-name)$")) | {property: .property_type_name, is: .reconciliation.is}'`
Enter
Wait
