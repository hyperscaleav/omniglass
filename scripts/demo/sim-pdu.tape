# The power conditioner is simulated and collected
#
# The dev fleet's rack power conditioner is a Ferrite PDU 4C with a simulator profile and a driver of its own:
# the mains voltage, the total current and each outlet's state over SNMP (#1368). SNMP and the document
# stay read only; its http control face is what switches an outlet (#1653).

Type "# The rack's power conditioner, collected by its own driver"
Enter
Wait
Type `pdu='hq.west.level-2.boardroom-a.$comp.power-1'`
Enter
Wait
Hide
Type `demo_until 120 'omniglass component metric list "$pdu" | jq -e "[.metrics[] | select(.metric_type_name == \"voltage\" or .metric_type_name == \"current\")] | length == 2"'`
Enter
Wait@130s
Type "clear"
Enter
Wait
Show
Type "# The mains it is fed (volts) and what the rack draws (amps), over SNMP"
Enter
Wait
Type `omniglass component metric list "$pdu" | jq -c '.metrics[] | select(.metric_type_name | test("^(voltage|current)$")) | {metric: .metric_type_name, value}'`
Enter
Wait
Type "# Which outlets are on"
Enter
Wait
Type `omniglass component property list "$pdu" | jq -c '.properties[] | select(.property_type_name | test("^outlet-")) | {property: .property_type_name, is: .reconciliation.is}'`
Enter
Wait
