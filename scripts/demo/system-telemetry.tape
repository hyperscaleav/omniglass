# A system owns its own metrics and properties
#
# A huddle room in Suite B4 is a system staffed by two panels. A calc rule over its members writes
# the room's mean temperature on the system itself, and a cloud booking service pushes what it knows
# about the room as a whole: a head count, an occupancy state and a meeting that started. Both read
# back on the system's own surfaces. A system has no endpoints and owns no log lines, so a push
# naming an instance or carrying a log line is refused (#422, ADR-0422).

Type "# A huddle room of this demo's own in Suite B4, staffed by two panels"
Enter
Wait
Type `omniglass system create --name demo-huddle --location harbor.pavilion-b.suite-b4 | jq -c '{name}'`
Enter
Wait
Type `s='harbor.pavilion-b.suite-b4.$sys.demo-huddle'`
Enter
Wait
Hide
Type `demo_hush 'for side in left right; do omniglass component create --name "huddle-panel-$side" --product generic-device --location harbor.pavilion-b.suite-b4 >/dev/null && omniglass system role update "$s" "$side" --quorum 1 --impact none >/dev/null && omniglass system role assignment update "$s" "$side" "harbor.pavilion-b.suite-b4.\$comp.huddle-panel-$side" >/dev/null; done'`
Enter
Wait
Show
Type `omniglass system role list "$s" | jq -c '.roles[] | {role: .name, filled: .assigned_to}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# The primary path: a calc rule over the room's members writes a metric on the system"
Enter
Wait
Type `omniglass metric-type create --name huddle-temp-mean --data-type float | jq -c '{name}'`
Enter
Wait
Type `omniglass calc-rule create --name huddle-temp --owner-kind system --inputs '[{"alias":"t","key":"room-temperature"}]' --reduce '{"expr":"avg(t)"}' --output-key huddle-temp-mean | jq -c '{name, owner_kind, output_key}'`
Enter
Wait
Type `for side in left:21 right:24; do omniglass telemetry push --owner "{\"kind\":\"component\",\"ref\":\"harbor.pavilion-b.suite-b4.\$comp.huddle-panel-${side%%:*}\"}" --metrics "[{\"name\":\"room-temperature\",\"value\":${side##*:}}]" | jq -c .accepted; done`
Enter
Wait

Type "# The push path: a booking service reports for the room as a whole, owner kind system"
Enter
Wait
Type `omniglass telemetry push --owner "{\"kind\":\"system\",\"ref\":\"$s\"}" --source room-booking-cloud --metrics '[{"name":"occupancy-count","value":3}]' --properties '[{"name":"occupancy-state","value":"occupied"}]' --events '[{"name":"call-started","message":"standup started"}]' | jq -c .accepted`
Enter
Wait
Hide
Type `demo_until 30 'omniglass system metric list "$s" | jq -e "[.metrics[] | select(.metric_type_name == \"huddle-temp-mean\" or .metric_type_name == \"occupancy-count\")] | length == 2"'`
Enter
Wait@40s
Type "clear"
Enter
Wait
Show
Type "# Both read on the system: the rule's mean, named by its rule, and the pushed count"
Enter
Wait
Type `omniglass system metric list "$s" | jq -c '.metrics[] | {metric: .metric_type_name, value, source_rule}'`
Enter
Wait
Type `omniglass system property sample list "$s" occupancy-state | jq -c '.samples[] | {value, provenance, source}'`
Enter
Wait
Type `omniglass system event list "$s" | jq -c '.events[] | select(.key == "call-started") | {key, owner_kind, message}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# A system has no endpoints: an observation naming one is refused"
Enter
Wait
Type `omniglass telemetry push --owner "{\"kind\":\"system\",\"ref\":\"$s\"}" --metrics '[{"name":"occupancy-count","value":4,"instance":"https"}]' 2>/dev/null | jq -r .detail || true`
Enter
Wait
Type "# And a log line is a component's own words, so a system batch carrying one is refused"
Enter
Wait
Type `omniglass telemetry push --owner "{\"kind\":\"system\",\"ref\":\"$s\"}" --logs '[{"message":"booking sync ok"}]' 2>/dev/null | jq -r .detail || true`
Enter
Wait
