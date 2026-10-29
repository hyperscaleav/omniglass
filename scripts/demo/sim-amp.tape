# The Polaris amplifier is simulated and collected
#
# The dev fleet's Polaris Amp 4-100s now have a simulator profile: an SNMP agent answering the
# amplifier's power, protect, heatsink temperature, fan and a channel table, and an HTTP API for
# standby and the protect reset. Their own driver, polaris-amp-snmp, reads the operating state, so
# every amplifier carries a signal of its class beside its identity and uptime (#898).

Type "# The auditorium's first amplifier, collected by its own driver"
Enter
Wait
Type `amp='east.hall.level-1.auditorium.$comp.amp-1'`
Enter
Wait
Hide
Type `demo_until 120 'omniglass component metric list "$amp" | jq -e "[.metrics[] | select(.metric_type_name == \"temperature\" or .metric_type_name == \"fan-speed\")] | length == 2"'`
Enter
Wait@130s
Type "clear"
Enter
Wait
Show
Type "# Its operating state, read off the Polaris arc over SNMP"
Enter
Wait
Type `omniglass component metric list "$amp" | jq -c '.metrics[] | select(.metric_type_name | test("^(temperature|fan-speed)$")) | {metric: .metric_type_name, endpoint: .instance, value}'`
Enter
Wait
Type `omniglass component property list "$amp" | jq -c '.properties[] | select(.property_type_name | test("^(power|protect)-state$")) | {property: .property_type_name, is: .reconciliation.is}'`
Enter
Wait

Type "# The channel table is served too: channel 1's output level and the load it drives"
Enter
Wait
Type `for oid in 1.3.6.1.4.1.55540.3.10.1.2.1 1.3.6.1.4.1.55540.3.10.1.4.1; do omniglass component simulation fire "$amp" --surface snmp --oid "$oid" | jq -c --arg oid "$oid" '{oid: $oid, value: .body}'; done`
Enter
Wait

Type "# Standby, entered over the amplifier's HTTP API"
Enter
Wait
Type `omniglass component simulation fire "$amp" --surface http-api --method POST --path /api/power --body '{"power": "standby"}' | jq -c '{status, body}'`
Enter
Wait
Hide
Type `demo_until 90 'omniglass component property list "$amp" | jq -e ".properties[] | select(.property_type_name == \"power-state\") | .reconciliation.is == \"standby\""'`
Enter
Wait@100s
Type "clear"
Enter
Wait
Show
Type "# The next SNMP poll reads it back"
Enter
Wait
Type `omniglass component property list "$amp" | jq -c '.properties[] | select(.property_type_name == "power-state") | {property: .property_type_name, is: .reconciliation.is}'`
Enter
Wait
