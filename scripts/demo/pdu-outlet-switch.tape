# An operator switches a PDU outlet, and switching it off holds what it powers
#
# The rack's Ferrite PDU 4C gains a driver for its http control face, and with it two command types:
# outlet-on, and outlet-off, which is disruptive and names its outlet parameter as the power marker.
# Switching an outlet off closes the gate of every endpoint of the component that outlet feeds, so the
# node holds its polls instead of reading the cut as failures, while the PDU itself and what its other
# outlets feed stay open. The outlet reads off over SNMP, and switching it back on with the device's
# first up verdict reopens the gate (#1653).

Type "# outlet-off is disruptive, and its outlet parameter is the power marker"
Enter
Wait
Type `omniglass command-type get outlet-off | jq -c '{name, disruptive, quiet_seconds, hold_seconds, power_outlet_param}'`
Enter
Wait
Type `omniglass command-type get outlet-off | jq -c '.params_schema.properties.outlet'`
Enter
Wait

Type "# The rack's PDU, and what each of its outlets feeds"
Enter
Wait
Type `d='hq.west.level-2.boardroom-a.$comp'; p="$d.power-1"; a="$d.amp-1"`
Enter
Wait
Type `omniglass component dependency list "$p" | jq -c '.dependents[] | {component, kind, outlet}'`
Enter
Wait

Hide
Type `demo_until 150 'omniglass component property list "$p" | jq -e ".properties[] | select(.property_type_name == \"outlet-2-state\") | .reconciliation.is == \"on\""'`
Enter
Wait@180s
Type `demo_until 150 'omniglass endpoint poll run "$a/snmp" status > /dev/null 2>&1'`
Enter
Wait@180s
Type "clear"
Enter
Wait
Show

Type "# An outlet is required and has to exist on a four outlet PDU"
Enter
Wait
Type `omniglass component command issue "$p" --command-type outlet-off 2>/dev/null | jq -r '"\(.status) \(.detail)"' || true`
Enter
Wait
Type `omniglass component command issue "$p" --command-type outlet-off --params '{"outlet": 5}' 2>/dev/null | jq -r '"\(.status) \(.detail)"' || true`
Enter
Wait

Type "# Switch off outlet 2, the amplifier's"
Enter
Wait
Type `omniglass component command issue "$p" --command-type outlet-off --params '{"outlet": 2}' | jq -c '{command_type, params, status}'`
Enter
Wait
Type "# The simulator powers nothing, so unplug the amplifier to stand in for the cut"
Enter
Wait
Type `omniglass component impairment set "$a" --unplugged | jq -c '{unplugged}'`
Enter
Wait
Hide
Type `demo_until 60 'omniglass component reachability list "$a" | jq -e "[.endpoints[] | .gate_reasons // [] | .[] | .kind] | index(\"disrupted\")"'`
Enter
Wait@90s
Show

Type "# Only what outlet 2 feeds is held: not the PDU, not the video bar on outlet 1"
Enter
Wait
Type `for c in amp-1 power-1 videobar-1; do omniglass component reachability list "$d.$c" | jq -c --arg c "$c" '.endpoints[] | {component: $c, endpoint, gate_reasons: [(.gate_reasons // [])[] | .kind]}'; done`
Enter
Wait
Type "# A run now is held, nothing is sent, and the 409 names the outlet-off"
Enter
Wait
Type `omniglass endpoint poll run "$a/snmp" status 2>/dev/null | jq -r '"\(.status) \(.detail)"' || true`
Enter
Wait

Hide
Type `demo_until 150 'omniglass component property list "$p" | jq -e ".properties[] | select(.property_type_name == \"outlet-2-state\") | .reconciliation.is == \"off\""'`
Enter
Wait@180s
Type `demo_until 150 'omniglass component reachability list "$a" | jq -e "[.endpoints[] | .gate_reasons // [] | .[] | .kind] | index(\"unreachable\")"'`
Enter
Wait@180s
Type "clear"
Enter
Wait
Show

Type "# The PDU reads outlet 2 off over SNMP"
Enter
Wait
Type `omniglass component property list "$p" | jq -c '.properties[] | select(.property_type_name | test("^outlet-")) | {property: .property_type_name, is: .reconciliation.is}'`
Enter
Wait
Type "# Past the quiet the amplifier is still dark: its probe reads it down, and a down verdict releases nothing"
Enter
Wait
Type `omniglass component reachability list "$a" | jq -c '.endpoints[] | {endpoint, gate_reasons: [(.gate_reasons // [])[] | .kind]}'`
Enter
Wait

Type "# Switch outlet 2 back on; the amplifier's first up verdict reopens its gate"
Enter
Wait
Type `omniglass component command issue "$p" --command-type outlet-on --params '{"outlet": 2}' | jq -c '{command_type, params, status}'`
Enter
Wait
Type `omniglass component impairment set "$a" | jq -c '{unplugged}'`
Enter
Wait
Hide
Type `demo_until 150 'omniglass component reachability list "$a" | jq -e "[.endpoints[] | .gate_reasons // [] | length] | add == 0"'`
Enter
Wait@180s
Show
Type `omniglass component reachability list "$a" | jq -c '.endpoints[] | {endpoint, gate_reasons}'`
Enter
Wait
Type `omniglass endpoint poll run "$a/snmp" status | jq -c '{stored, poll, samples: [.samples[] | .name]}'`
Enter
Wait
