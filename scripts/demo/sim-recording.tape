# The recording service is simulated and collected
#
# The dev fleet's recording service now has a simulator profile and a driver of its own: an HTTPS
# JSON API behind an api key, reporting its health and version, the recordings it has in progress,
# its storage and its queue, with a live feed that names a recording the moment it fails. A service
# is watched the way a device is (#1363).

Type "# The fleet's recording service, collected by its own driver"
Enter
Wait
Hide
Type `rec=$(omniglass component list | jq -r '.components[] | select(.product == "recording-service") | .id')`
Enter
Wait
Type `demo_until 120 'omniglass component property list "$rec" | jq -e "[.properties[] | select(.property_type_name == \"service-state\" and .reconciliation.is != null)] | length == 1"'`
Enter
Wait@130s
Type "clear"
Enter
Wait
Show
Type "# What it says of itself: its health and its version"
Enter
Wait
Type `omniglass component property list "$rec" | jq -c '.properties[] | select(.property_type_name | test("^(service-state|software-version)$")) | {property: .property_type_name, is: .reconciliation.is}'`
Enter
Wait
Type "# The work it carries, landed as samples"
Enter
Wait
Type `omniglass component metric list "$rec" | jq -c '.metrics[] | select(.metric_type_name | test("^(active-recording-count|queue-depth|storage-utilization)$")) | {metric: .metric_type_name, endpoint: .instance}'`
Enter
Wait

Type "# Fail a recording over its API"
Enter
Wait
Type `omniglass component simulation fire "$rec" --surface http-api --method POST --path /api/recordings/fail --body '{"id": "r-9001", "reason": "disk full"}' | jq -c '{status, body: (.body | fromjson)}'`
Enter
Wait
Hide
Type `demo_until 30 'omniglass component event list "$rec" | jq -e "[.events[] | select(.key == \"recording-failed\")] | length > 0"'`
Enter
Wait@40s
Type "clear"
Enter
Wait
Show
Type "# The live feed named it: an event on the component, with the recording and why"
Enter
Wait
Type `omniglass component event list "$rec" | jq -c '[.events[] | select(.key == "recording-failed")][0] | {key, message, attributes}'`
Enter
Wait
