# HTTP holds a server-sent events stream
#
# A simulated Kestrel bar streams its changes over https. An SSE driver attached to it lands a
# volume change with no poll, a dropped stream resumes from its last event id with every change
# landed once, and a wrong password reads rejected with its reason until the secret is fixed in
# place (#1038).

Type "# The huddle's Kestrel bar, by its dotted address, and the shipped driver's live feed"
Enter
Wait
Type `bar='hq.west.level-2.huddle.$comp.videobar-1'`
Enter
Wait
Type `omniglass driver get kestrel-api | jq -c '{stream: .spec.session.stream, listeners: [.spec.listeners[].name]}'`
Enter
Wait
Sleep 2s

Hide
Type `demo_hush 'addr=$(omniglass component simulation get "$bar" | jq -r .address)'`
Enter
Wait
Type `demo_hush 'omniglass endpoint delete "$(omniglass endpoint list | jq -r --arg c "$(omniglass component get "$bar" | jq -r .id)" ".endpoints[] | select(.component_id == \$c and .transport == \"https\") | .id")"'`
Enter
Wait
Type "clear"
Enter
Wait
Show

Type "# Attach an SSE driver (the seeded attach is detached, off screen, so this one rides its own secret)"
Enter
Wait
Type "# kestrel-feed: kestrel-api keeping only its system poll, so the volume can only arrive on the stream"
Enter
Wait
Type `omniglass driver create --name kestrel-feed --label 'Kestrel live feed' --version 1.0.0 --spec "$(omniglass driver get kestrel-api | jq -c '.spec | .polls = [.polls[0]]')" | jq -c '{name, polls: [.spec.polls[] | {name, every: .schedule.every}]}'`
Enter
Wait
Type `omniglass secret create --name demo-bar-login --owner-kind platform --secret-type basic-auth --fields '{"username":"admin","password":"kestrel"}' | jq -c '{name}'`
Enter
Wait
Type `omniglass endpoint create --component "$bar" --driver kestrel-feed --node edge-hq --inputs "{\"host\":\"$addr\",\"port\":\"443\",\"login\":\"demo-bar-login\"}" | jq -c '{name, driver}'`
Enter
Wait

Hide
Type `demo_until 120 'omniglass component reachability list "$bar" | jq -e ".endpoints[] | select(.endpoint == \"https\") | .session.state == \"up\""'`
Enter
Wait@150s
Show

Type `reach() { omniglass component reachability list "$bar" | jq -c '.endpoints[] | select(.endpoint == "https") | {session: .session.state, authenticated: .authenticated.value, rejection: .rejection.reason}'; }`
Enter
Wait
Type `reach`
Enter
Wait
Sleep 2s

Hide
Type "clear"
Enter
Wait
Show
Type "# A change on the bar lands in the samples at once, with no poll"
Enter
Wait
Type `omniglass component simulation fire "$bar" --surface http-api --method POST --path /api/audio/volume --body '{"volume": 37}' | jq -c .status`
Enter
Wait
Hide
Type `demo_until 3 '[ "$(omniglass component metric sample list "$bar" volume --limit 1 | jq -r ".samples[0].value")" = 37 ]'`
Enter
Wait@10s
Show
Type `omniglass component metric sample list "$bar" volume --limit 1 | jq -c '.samples[0] | {instance, value}'`
Enter
Wait
Sleep 2s

Hide
Type "clear"
Enter
Wait
Show
Type "# The bar drops the stream, and the volume changes twice while it is gone"
Enter
Wait
Type `omniglass component simulation fire "$bar" --surface http-api --method POST --path /api/admin/streams/drop | jq -c '.body | fromjson'`
Enter
Wait
Type `for v in 38 39; do omniglass component simulation fire "$bar" --surface http-api --method POST --path /api/audio/volume --body "{\"volume\": $v}" | jq -c .status; done`
Enter
Wait
Hide
Type `demo_until 30 '[ "$(omniglass component metric sample list "$bar" volume --limit 1 | jq -r ".samples[0].value")" = 39 ]'`
Enter
Wait@40s
Show
Type "# It waited the bar's retry, resumed from its last event id, and landed each change once"
Enter
Wait
Type `omniglass component property sample list "$bar" session-state --limit 4 | jq -c '[.samples[] | .value] | reverse'`
Enter
Wait
Type `omniglass component metric sample list "$bar" volume | jq -c '[.samples[] | select(.instance == "https") | .value][:3] | reverse'`
Enter
Wait
Sleep 2s

Hide
Type `demo_hush 'login=$(omniglass secret list | jq -r ".secrets[] | select(.name == \"demo-bar-login\") | .id")'`
Enter
Wait
Type "clear"
Enter
Wait
Show
Type "# A wrong password: the stream reads rejected, with its reason, and is not tried again"
Enter
Wait
Type `omniglass secret update "$login" --fields '{"password":"wrong-password"}' | jq -c '{name}'`
Enter
Wait
Hide
Type `demo_until 60 'omniglass component reachability list "$bar" | jq -e ".endpoints[] | select(.endpoint == \"https\") | .session.state == \"rejected\""'`
Enter
Wait@90s
Show
Type `reach`
Enter
Wait
Sleep 2s

Type "# The secret fixed in place: the stream comes back, and a change lands again"
Enter
Wait
Type `omniglass secret update "$login" --fields '{"password":"kestrel"}' | jq -c '{name}'`
Enter
Wait
Hide
Type `demo_until 60 'omniglass component reachability list "$bar" | jq -e ".endpoints[] | select(.endpoint == \"https\") | .session.state == \"up\" and .rejection == null"'`
Enter
Wait@90s
Show
Type `reach`
Enter
Wait
Type `omniglass component simulation fire "$bar" --surface http-api --method POST --path /api/audio/volume --body '{"volume": 40}' | jq -c .status`
Enter
Wait
Hide
Type `demo_until 10 '[ "$(omniglass component metric sample list "$bar" volume --limit 1 | jq -r ".samples[0].value")" = 40 ]'`
Enter
Wait@20s
Show
Type `omniglass component metric sample list "$bar" volume | jq -c '[.samples[] | select(.instance == "https") | .value][:4] | reverse'`
Enter
Wait
Sleep 3s
