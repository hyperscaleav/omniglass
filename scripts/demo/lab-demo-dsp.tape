# The reachability demo DSP reports its mute and level
#
# The worked reachability check's boardroom DSP becomes a simulated Polaris DSP 16 in the lab, its
# HTTP and line-protocol checks re-aimed at the simulator. It now also carries that product's own
# driver, polaris-dsp-snmp, at the same address, so it reports its program mute and level like every
# other Polaris DSP 16 in the fleet (#1507).

Type "# The boardroom's DSP, named by its operator"
Enter
Wait
Type `dsp='hq.west.level-2.boardroom-a.$comp.dsp'`
Enter
Wait
Hide
Type `demo_until 120 'omniglass component metric list "$dsp" | jq -e "[.metrics[] | select(.metric_type_name == \"audio-level\")] | length >= 1"'`
Enter
Wait@130s
Type "clear"
Enter
Wait
Show
Type "# Its two reach checks, and the driver its product is on, all at one simulated address"
Enter
Wait
Type `id=$(omniglass component get "$dsp" | jq -r .id); omniglass endpoint list | jq -c --arg id "$id" '.endpoints[] | select(.component_id == $id) | {name, transport, driver}'`
Enter
Wait
Type "# The program output, read over SNMP by polaris-dsp-snmp"
Enter
Wait
Type `omniglass component metric list "$dsp" | jq -c '.metrics[] | select(.metric_type_name == "audio-level") | {metric: .metric_type_name, endpoint: .instance, value}'`
Enter
Wait
Type `omniglass component property list "$dsp" | jq -c '.properties[] | select(.property_type_name == "mute") | {property: .property_type_name, is: .reconciliation.is}'`
Enter
Wait
