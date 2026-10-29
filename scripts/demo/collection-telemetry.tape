# Every endpoint reports its own collection
#
# Beside what a driver reads from the device, every endpoint carries data about its own collection,
# built in and on by default: once a report window (collection.report_seconds, a minute), the mean
# time of its polls, the messages its listeners claimed, and for a webhook endpoint the posts the
# server accepted and refused. A device that only pushes still has telemetry of its own (#1369).

Type "# The report window, a platform setting: a minute by default"
Enter
Wait
Type `omniglass setting list | jq -c '.values.collection'`
Enter
Wait

Type "# The huddle's Kestrel bar: the shipped driver polls it and holds its event stream"
Enter
Wait
Type `bar='hq.west.level-2.huddle.$comp.videobar-1'`
Enter
Wait
Type "# Turn its volume up: the bar pushes the change down its event stream"
Enter
Wait
Type `omniglass component simulation fire "$bar" --surface http-api --method POST --path /api/audio/volume --body '{"volume": 41}' | jq -c .status`
Enter
Wait
Hide
Type `demo_until 90 'omniglass component metric list "$bar" | jq -e "([.metrics[] | select(.metric_type_name == \"push-message-count\" and .value >= 1)] | length == 1) and ([.metrics[] | select(.metric_type_name == \"poll-duration\")] | length == 1)"'`
Enter
Wait@100s
Type "clear"
Enter
Wait
Show
Type "# Its endpoint's account of itself this window: mean poll time (ms), and the push its listener claimed"
Enter
Wait
Type `omniglass component metric list "$bar" | jq -c '.metrics[] | select(.metric_type_name | test("^(poll|push)-")) | {metric: .metric_type_name, endpoint: .instance, value: ((.value * 10 | round) / 10)}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# A meeting service that only pushes: added to the simulated fleet, given its webhook endpoint"
Enter
Wait
Type `ucc='hq.$comp.meetings-cloud'`
Enter
Wait
Hide
Type `demo_hush 'omniglass simulation add-component edge-hq --name meetings-cloud --location hq --product ucc-service'`
Enter
Wait
Type `demo_until 90 'omniglass component simulation fire "$ucc" --surface http-api --path /api/status >/dev/null 2>&1'`
Enter
Wait@100s
Type `demo_hush 'omniglass secret create --name meetings-cloud-signing --owner-kind component --owner "$ucc" --secret-type hmac-key --fields "{\"key\":\"demo-1369-signing-key\"}"'`
Enter
Wait
Type `demo_hush 'omniglass endpoint create --component "$ucc" --driver ucc-webhook --inputs "{\"signing-key\":\"meetings-cloud-signing\"}"'`
Enter
Wait
Type `demo_hush 'url="$OMNIGLASS_SERVER/api/v1/webhooks/$(omniglass secret reveal "$ucc/webhook-token" | jq -r .fields.token)"'`
Enter
Wait
Type "clear"
Enter
Wait
Show
Type "# It posts two meetings signed, and one with the wrong key"
Enter
Wait
Type `omniglass component simulation fire "$ucc" --surface http-api --method POST --path /api/webhooks --body "{\"url\":\"$url\",\"scheme\":\"hmac\",\"secret\":\"demo-1369-signing-key\"}" | jq -c .status`
Enter
Wait
Type `for m in m-1 m-2; do omniglass component simulation fire "$ucc" --surface http-api --method POST --path /api/meetings/start --body "{\"id\":\"$m\"}" | jq -c .status; done`
Enter
Wait
Type `omniglass component simulation fire "$ucc" --surface http-api --method POST --path /api/webhooks --body "{\"url\":\"$url\",\"scheme\":\"hmac\",\"secret\":\"demo-1369-signing-key\",\"fault\":\"bad-signature\"}" | jq -c .status`
Enter
Wait
Type `omniglass component simulation fire "$ucc" --surface http-api --method POST --path /api/meetings/start --body '{"id":"m-3"}' | jq -c .status`
Enter
Wait
Hide
Type `demo_until 90 'omniglass component metric list "$ucc" | jq -e "[.metrics[] | select(.metric_type_name == \"webhook-post-count\" or .metric_type_name == \"webhook-refusal-count\")] | length == 2"'`
Enter
Wait@100s
Type "clear"
Enter
Wait
Show
Type "# No node and no poll, yet its own series: the posts the server accepted, and the one it refused"
Enter
Wait
Type `omniglass component metric list "$ucc" | jq -c '.metrics[] | select(.metric_type_name | startswith("webhook-")) | {metric: .metric_type_name, endpoint: .instance, value}'`
Enter
Wait
