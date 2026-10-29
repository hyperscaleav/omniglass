# The Lyra microphones are simulated and collected
#
# The dev fleet's Lyra Arc A2 ceiling arrays and HX-1 wireless systems now have simulator profiles and
# drivers of their own: mute and the input level over SNMP on the Lyra arc, and the wireless system's
# transmitter battery, stored as a ratio. Each microphone carries signals of its class beside its
# identity and uptime (#894).

Type "# A wireless microphone in the auditorium, collected by its own driver"
Enter
Wait
Type `mic='east.hall.level-1.auditorium.$comp.mic-3'`
Enter
Wait
Hide
Type `demo_until 120 'omniglass component metric list "$mic" | jq -e "[.metrics[] | select(.metric_type_name == \"battery-charge\" or .metric_type_name == \"audio-level\")] | length == 2"'`
Enter
Wait@130s
Type "clear"
Enter
Wait
Show
Type "# Its input level (dBFS) and its transmitter's battery (a ratio), over SNMP"
Enter
Wait
Type `omniglass component metric list "$mic" | jq -c '.metrics[] | select(.metric_type_name | test("^(audio-level|battery-charge)$")) | {metric: .metric_type_name, endpoint: .instance, value}'`
Enter
Wait
Type `omniglass component property list "$mic" | jq -c '.properties[] | select(.property_type_name == "mute") | {property: .property_type_name, is: .reconciliation.is}'`
Enter
Wait

Type "# Mute it over its API"
Enter
Wait
Type `omniglass component simulation fire "$mic" --surface http-api --method POST --path /api/mute --body '{"mute": true}' | jq -c '{status, body: (.body | fromjson)}'`
Enter
Wait
Hide
Type `demo_until 90 'omniglass component property list "$mic" | jq -e ".properties[] | select(.property_type_name == \"mute\") | .reconciliation.is == \"true\""'`
Enter
Wait@100s
Type "clear"
Enter
Wait
Show
Type "# The next SNMP poll reads it muted"
Enter
Wait
Type `omniglass component property list "$mic" | jq -c '.properties[] | select(.property_type_name == "mute") | {property: .property_type_name, is: .reconciliation.is}'`
Enter
Wait
