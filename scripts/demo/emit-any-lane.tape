# A driver emit lands on any lane
#
# The shipped newtron-nvp driver lands one push from the simulated touch panel on two lanes (the
# video-input property and an input-changed event carrying the input), a diagnostics line as a log
# line with its severity, and a push nothing claims as a raw log line; kestrel-api lands a call
# starting and ending as events carrying the call's id. Nothing is polled for any of it (#1145).

Type "# A suite's touch panel and video bar, by their dotted addresses"
Enter
Wait
Type `panel='harbor.pavilion-b.suite-b4.$comp.panel-1'; bar='harbor.pavilion-b.suite-b4.$comp.videobar-1'`
Enter
Wait
Type "# The shipped driver: one push lands on several lanes, and what nothing claims lands as a log line"
Enter
Wait
Type `omniglass driver get newtron-nvp | jq -c '{unmatched: .spec.unmatched, listeners: [.spec.listeners[] | {name, match: .match.prefix, emits: [.emits[] | (.name // "a log line")]}]}'`
Enter
Wait

Hide
Type `demo_until 120 'omniglass component reachability list "$panel" | jq -e ".endpoints[] | select(.endpoint == \"tcp\") | .session.state == \"up\""'`
Enter
Wait@150s
Type "clear"
Enter
Wait
Show

Type "# The panel switches to HDMI 3: the property moves, and an event carries the input"
Enter
Wait
Type `omniglass component simulation fire "$panel" --surface nvp --command "SET INPUT hdmi3" | jq -r .body`
Enter
Wait
Hide
Type `demo_until 15 '[ "$(omniglass component event list "$panel" | jq -r "[.events[] | select(.key == \"input-changed\")][0].attributes.input")" = hdmi3 ]'`
Enter
Wait@30s
Show
Type `omniglass component event list "$panel" | jq -c '[.events[] | select(.key == "input-changed")][0] | {key, message, origin, attributes}'`
Enter
Wait
Type `omniglass component property sample list "$panel" video-input --limit 1 | jq -c '.samples[0] | {instance, value}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# The panel logs a warning, then changes scene, a push no listener claims"
Enter
Wait
Type `omniglass component simulation fire "$panel" --surface http-api --method POST --path /api/diagnostics/log --body '{"line":"WARN demo backlight driver reset"}' | jq -c .status`
Enter
Wait
Type `omniglass component simulation fire "$panel" --surface http-api --method POST --path /api/room/scene --body '{"scene":"presentation"}' | jq -c .status`
Enter
Wait
Hide
Type `demo_until 15 'omniglass component log list "$panel" | jq -e "[.logs[] | select(.message == \"EVT SCENE presentation\")] | length > 0"'`
Enter
Wait@30s
Show
Type "# Two log lines on the panel: the warning with its severity, the scene change whole, each naming its endpoint"
Enter
Wait
Type `omniglass component log list "$panel" | jq -c '.logs[:2][] | {severity, message, via: .instance}'`
Enter
Wait
Type "# The read filters by severity, warning and worse, and lists the sources the window carries"
Enter
Wait
Type `omniglass component log list "$panel" --severity warning | jq -c '{first: (.logs[0] | {severity, message}), sources}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# The bar places a call and hangs up: call-started and call-ended, each carrying the call's id"
Enter
Wait
Type `omniglass component simulation fire "$bar" --surface http-api --method POST --path /api/call/dial --body '{"id":"demo-call-1145","participants":4}' | jq -c .status`
Enter
Wait
Type `omniglass component simulation fire "$bar" --surface http-api --method POST --path /api/call/disconnect | jq -c .status`
Enter
Wait
Hide
Type `demo_until 15 'omniglass component event list "$bar" | jq -e "[.events[] | select(.key == \"call-ended\" and .attributes.call_id == \"demo-call-1145\")] | length > 0"'`
Enter
Wait@30s
Show
Type `omniglass component event list "$bar" | jq -c '[.events[] | select(.attributes.call_id == "demo-call-1145")] | reverse | .[] | {key, message, attributes}'`
Enter
Wait
