# The Boreal projector is simulated and collected
#
# The dev fleet's Boreal Beam 340 now has a simulator profile of its own: a projector is not a display.
# Its web admin sets power, the shutter and the input behind HTTP Digest, and its SNMP agent answers the
# light-source hours a projector is serviced by. Its own driver, boreal-beam-snmp, reads them, so the
# projector carries signals of its class beside its identity and uptime (#899).

Type "# The auditorium's projector, collected by its own driver"
Enter
Wait
Type `pj='east.hall.level-1.auditorium.$comp.projector-1'`
Enter
Wait
Hide
Type `demo_until 120 'omniglass component metric list "$pj" | jq -e "[.metrics[] | select(.metric_type_name == \"light-source-hours\")] | length == 1"'`
Enter
Wait@130s
Type "clear"
Enter
Wait
Show
Type "# The hours its light source has been lit, read off the Boreal arc over SNMP"
Enter
Wait
Type `omniglass component metric list "$pj" | jq -c '.metrics[] | select(.metric_type_name == "light-source-hours") | {metric: .metric_type_name, endpoint: .instance, value}'`
Enter
Wait
Type `omniglass component property list "$pj" | jq -c '.properties[] | select(.property_type_name | test("^(power-state|shutter-state|video-input)$")) | {property: .property_type_name, is: .reconciliation.is}'`
Enter
Wait

Type "# Close the shutter over the web admin: the picture blanks, the projector stays on"
Enter
Wait
Type `omniglass component simulation fire "$pj" --surface web-admin --method POST --path /api/projector/shutter --body '{"shutter": "closed"}' | jq -c '{status, body}'`
Enter
Wait
Hide
Type `demo_until 90 'omniglass component property list "$pj" | jq -e ".properties[] | select(.property_type_name == \"shutter-state\") | .reconciliation.is == \"closed\""'`
Enter
Wait@100s
Type "clear"
Enter
Wait
Show
Type "# The next SNMP poll reads it back"
Enter
Wait
Type `omniglass component property list "$pj" | jq -c '.properties[] | select(.property_type_name | test("^(power-state|shutter-state)$")) | {property: .property_type_name, is: .reconciliation.is}'`
Enter
Wait
