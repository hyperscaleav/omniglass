# The Kestrel PTZ cameras are simulated and collected
#
# The dev fleet's Kestrel Gaze P2 cameras now have a simulator profile and a driver of their own,
# kestrel-gaze-api: pan, tilt, magnification, the recalled preset and the tracking mode, read from the
# camera's own API. The Relay C1 codec has its profile and driver too (call state, content sharing,
# peripherals), though no Relay is placed in the dev fleet yet (#897).

Type "# A PTZ camera in the auditorium, collected by its own driver"
Enter
Wait
Type `cam='east.hall.level-1.auditorium.$comp.camera-1'`
Enter
Wait
Hide
Type `demo_until 120 'omniglass component metric list "$cam" | jq -e "[.metrics[] | select(.metric_type_name == \"pan-angle\" or .metric_type_name == \"camera-preset\")] | length == 2"'`
Enter
Wait@130s
Type "clear"
Enter
Wait
Show
Type "# Its pan angle (degrees) and recalled preset, over its API"
Enter
Wait
Type `omniglass component metric list "$cam" | jq -c '.metrics[] | select(.metric_type_name | test("^(pan-angle|tilt-angle|zoom-level|camera-preset)$")) | {metric: .metric_type_name, value}'`
Enter
Wait
Type `omniglass component property list "$cam" | jq -c '.properties[] | select(.property_type_name == "tracking-state") | {property: .property_type_name, is: .reconciliation.is}'`
Enter
Wait

Type "# Recall preset 2 on the camera"
Enter
Wait
Type `omniglass component simulation fire "$cam" --surface http-api --method POST --path /api/camera/preset --body '{"preset": "2"}' | jq -c '{status, body: (.body | fromjson)}'`
Enter
Wait
Hide
Type `demo_until 90 'omniglass component metric list "$cam" | jq -e ".metrics[] | select(.metric_type_name == \"camera-preset\") | .value == 2"'`
Enter
Wait@100s
Type "clear"
Enter
Wait
Show
Type "# The next poll reads the new preset and the manual tracking mode"
Enter
Wait
Type `omniglass component metric list "$cam" | jq -c '.metrics[] | select(.metric_type_name | test("^(pan-angle|camera-preset)$")) | {metric: .metric_type_name, value}'`
Enter
Wait
