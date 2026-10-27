# Omniglass receives webhooks and maps them to any lane
#
# A meeting platform's cloud service, new to the fleet, posts to a URL omniglass does not know: the
# listener answers 404 and keeps the sender in the unclaimed-source inbox. Claiming it is creating
# its webhook endpoint through the shipped ucc-webhook driver, which mints the URL; given the URL
# and its signing key, the service's signed post lands as call-started on the timeline, the
# call-state property and a log line, and the inbox lets the sender go. A post signed with the
# wrong key answers 401, is counted on the endpoint, and lands nothing (#1024).

Type "# A meeting platform's cloud service, added to the simulated fleet at headquarters"
Enter
Wait
Type `ucc='hq.$comp.meetings-cloud'`
Enter
Wait
Type `omniglass simulation add-component edge-hq --name meetings-cloud --location hq --product ucc-service | jq -c '{node, endpoints}'`
Enter
Wait
Hide
Type `demo_until 90 'omniglass component simulation fire "$ucc" --surface http-api --path /api/status >/dev/null 2>&1'`
Enter
Wait@100s
Type "clear"
Enter
Wait
Show

Type "# It already posts its meetings to a URL nobody gave it: omniglass answers 404"
Enter
Wait
Type `omniglass component simulation fire "$ucc" --surface http-api --method POST --path /api/webhooks --body "{\"url\":\"$OMNIGLASS_SERVER/api/v1/webhooks/AAAAAAAAAAAAAAAAAAAAAA\"}" | jq -c .status`
Enter
Wait
Type `omniglass component simulation fire "$ucc" --surface http-api --method POST --path /api/meetings/start --body '{"id":"m-1001","room":"huddle"}' | jq -c .status`
Enter
Wait
Hide
Type `demo_until 20 'omniglass unclaimed-source list | jq -e "[.sources[] | select(.last_message | contains(\"m-1001\"))] | length > 0"'`
Enter
Wait@30s
Show
Type "# The listener kept the sender in the inbox: how often, when, its last message (never the token it tried)"
Enter
Wait
Type `omniglass unclaimed-source list | jq -c '.sources[0] | {host, transport, count, last_message}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# Claim it: its signing key, then a webhook endpoint through the shipped driver, which mints the URL"
Enter
Wait
Type `omniglass secret create --name meetings-cloud-signing --owner-kind component --owner "$ucc" --secret-type hmac-key --fields '{"key":"demo-1024-signing-key"}' | jq -c '{name, secret_type}'`
Enter
Wait
Type `omniglass endpoint create --component "$ucc" --driver ucc-webhook --inputs '{"signing-key":"meetings-cloud-signing"}' | jq -c '{name, webhook: (.webhook | {path, scheme, signed})}'`
Enter
Wait
Hide
Type `demo_hush 'url="$OMNIGLASS_SERVER/api/v1/webhooks/$(omniglass secret reveal "$ucc/webhook-token" | jq -r .fields.token)"'`
Enter
Wait
Show
Type "# Give the service its URL (the token stays off screen) and its key, then start a meeting"
Enter
Wait
Type `omniglass component simulation fire "$ucc" --surface http-api --method POST --path /api/webhooks --body "{\"url\":\"$url\",\"scheme\":\"hmac\",\"secret\":\"demo-1024-signing-key\"}" | jq -c .status`
Enter
Wait
Type `omniglass component simulation fire "$ucc" --surface http-api --method POST --path /api/meetings/start --body '{"id":"m-1002","room":"huddle"}' | jq -c .status`
Enter
Wait
Hide
Type `demo_until 20 'omniglass component event list "$ucc" | jq -e "[.events[] | select(.key == \"call-started\")] | length > 0"'`
Enter
Wait@30s
Show
Type "# Signed, checked and landed: call-started on the timeline, call-state, and the post in its logs"
Enter
Wait
Type `omniglass component event list "$ucc" | jq -c '[.events[] | select(.key == "call-started")][0] | {key, message, attributes}'`
Enter
Wait
Type `omniglass component property sample list "$ucc" call-state --limit 1 | jq -c '.samples[0] | {instance, value}'`
Enter
Wait
Type `omniglass component log list "$ucc" | jq -c '.logs[0] | {via: .instance, message}'`
Enter
Wait
Type "# The inbox let the sender go: its posts land on an endpoint now"
Enter
Wait
Type `omniglass unclaimed-source list | jq -c '.sources | length'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# A post signed with the wrong key: answered 401, counted on the endpoint, nothing lands"
Enter
Wait
Type `omniglass component simulation fire "$ucc" --surface http-api --method POST --path /api/webhooks --body "{\"url\":\"$url\",\"scheme\":\"hmac\",\"secret\":\"demo-1024-signing-key\",\"fault\":\"bad-signature\"}" | jq -c .status`
Enter
Wait
Type `omniglass component simulation fire "$ucc" --surface http-api --method POST --path /api/meetings/start --body '{"id":"m-1003","room":"huddle"}' | jq -c .status`
Enter
Wait
Hide
Type `demo_until 20 'omniglass endpoint get "$ucc/webhook" | jq -e ".webhook.refused.count >= 1"'`
Enter
Wait@30s
Show
Type `omniglass endpoint get "$ucc/webhook" | jq -c '.webhook | {scheme, signed, refused: (.refused | {count, reason})}'`
Enter
Wait
Type `omniglass component event list "$ucc" | jq -c '[.events[] | select(.attributes.meeting == "m-1003")] | length'`
Enter
Wait
