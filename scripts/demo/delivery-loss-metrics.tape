# Every hop writes its delivery loss per kind on its platform component
#
# The node and the server each write their own hop's delivery as three platform metrics, the item
# kind (metric, property, event, log) as the instance: delivery-in-count, delivery-out-count and
# delivery-loss-ratio, (in - out) / in, where 0 is healthy. They land every report window, 0 when
# quiet, and replace the old spool-drop, publish-failure and bus drop counts (#1659, ADR-1659).

Hide
Type `demo_until 150 'omniglass component metric list edge-hq | jq -e "[.metrics[] | select(.metric_type_name == \"delivery-loss-ratio\") | .instance_count] == [4]"'`
Enter
Wait@180s
Type `demo_hush 'srv=$(omniglass view platform list | jq -r ".servers[0].name")'`
Enter
Wait
Type `demo_until 150 'omniglass component metric list "$srv" | jq -e "[.metrics[] | select(.metric_type_name == \"delivery-loss-ratio\") | .instance_count] == [4]"'`
Enter
Wait@180s
Type "clear"
Enter
Wait
Show
Type "# One key family on every hop, the item kind as its instance"
Enter
Wait
Type `omniglass component metric list edge-hq | jq -c '.metrics[] | select(.metric_type_name | startswith("delivery-")) | {metric: .metric_type_name, kinds: [.instances[].instance]}'`
Enter
Wait
Type "# The node edge-hq: each kind's window, in, out and the loss between them"
Enter
Wait
Type `omniglass component metric list edge-hq | jq -c '[.metrics[] | select(.metric_type_name | startswith("delivery-")) | .metric_type_name as $m | .instances[] | {m: $m, kind: .instance, v: .value}] | group_by(.kind)[] | (map({(.m): .v}) | add) as $d | {kind: .[0].kind, in: $d["delivery-in-count"], out: $d["delivery-out-count"], loss: $d["delivery-loss-ratio"]}'`
Enter
Wait
Type "# The server's own hop, read the same way on its own platform component"
Enter
Wait
Type `omniglass component metric list "$srv" | jq -c '[.metrics[] | select(.metric_type_name | startswith("delivery-")) | .metric_type_name as $m | .instances[] | {m: $m, kind: .instance, v: .value}] | group_by(.kind)[] | (map({(.m): .v}) | add) as $d | {kind: .[0].kind, in: $d["delivery-in-count"], out: $d["delivery-out-count"], loss: $d["delivery-loss-ratio"]}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# The ratio is the catalog's to explain, and the bespoke drop counts are gone"
Enter
Wait
Type `omniglass metric-type get delivery-loss-ratio | jq -r .description`
Enter
Wait
Type `omniglass metric-type get spool-drop-count 2>/dev/null | jq -r .detail || true`
Enter
Wait
