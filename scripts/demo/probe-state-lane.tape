# A probe's state is stored when it flips, not every tick
#
# A tcp probe on a suite's touch panel reports two things every ten seconds: whether the port is
# open and how long the connect took. The timing is a number an operator trends, so it lands as a
# metric row every probe. The state is a word (open or closed) that only matters when it changes, so
# it is a property, written once until it flips. At 1,000 systems the two probe states were half of
# every metric row, each repeating the last one, which is why they moved lanes (#1592).

Type "# A suite's touch panel, by its dotted address; its edge node probes the panel's tcp port every 10s"
Enter
Wait
Type `panel='harbor.pavilion-b.suite-b4.$comp.panel-1'`
Enter
Wait

Hide
Type `demo_until 150 'omniglass component metric sample list "$panel" tcp-connect-time | jq -e ".samples | length >= 3"'`
Enter
Wait@180s
Type "clear"
Enter
Wait
Show

Type "# The reachability ladder reads each layer's state in its own words: open or closed, up or down"
Enter
Wait
Type `omniglass component reachability list "$panel" | jq -c '.endpoints[] | select(.endpoint == "tcp") | .layers[] | {layer, check, value, detail}'`
Enter
Wait

Type "# The connect time is a metric: one row per probe, a trend to read"
Enter
Wait
Type `omniglass component metric sample list "$panel" tcp-connect-time | jq -c '{key: "tcp-connect-time", rows: (.samples | length), latest_ms: ((.samples[0].value * 100 | round) / 100)}'`
Enter
Wait

Type "# The port's state is a property: the same probes wrote it once, and it stays one row until the port closes"
Enter
Wait
Type `omniglass component property sample list "$panel" tcp-open | jq -c '{key: "tcp-open", rows: (.samples | length), value: .samples[0].value, since: .samples[0].ts}'`
Enter
Wait

Type "# No probe state is a metric any more: the panel's probe metrics are its timings alone"
Enter
Wait
Type `omniglass component metric list "$panel" | jq -c '[.metrics[] | .metric_type_name | select(test("^(tcp|icmp)-"))] | unique'`
Enter
Wait
