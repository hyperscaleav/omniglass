# Omnibot asks before it writes
#
# omniglass omnibot runs an assistant in the terminal: it mints a short-lived
# agent token of its own, talks to the MCP server with it, runs a read on its
# own, and asks the person before each write. A recorded model stands in for a
# live one so the run is the same every time. The person allows one tag and
# declines the other; the audit trail files the write under the omnibot
# channel, as the agent's, and the session token is revoked when the run ends
# (#1166).

Hide
Type `demo_hush "printf '%s' '{\"turns\":[{\"tool_calls\":[{\"name\":\"get-component\",\"arguments\":{\"name\":\"hq.west.level-4.meeting-405.\$comp.videobar-1\"}}]},{\"tool_calls\":[{\"name\":\"set-component-tag\",\"arguments\":{\"name\":\"hq.west.level-4.meeting-405.\$comp.videobar-1\",\"key\":\"asset-id\",\"value\":\"AV-0415\"}}]},{\"tool_calls\":[{\"name\":\"set-component-tag\",\"arguments\":{\"name\":\"hq.west.level-4.meeting-405.\$comp.videobar-1\",\"key\":\"category\",\"value\":\"conferencing\"}}]},{\"text\":\"Tagged the meeting 405 video bar as AV-0415. The category stays as it was.\"}]}' > /tmp/lobby.json"`
Enter
Wait
Show

Type "# Ask omnibot to tag a video bar; the read runs on its own, each write asks first"
Enter
Wait
Type `omniglass omnibot --replay /tmp/lobby.json "tag the meeting 405 video bar with its asset id"`
Enter
Wait+Screen /Run it\?/
Type "y"
Enter
Wait+Screen /"category"/
Type "n"
Enter
Wait

Type "# The write is filed under the omnibot channel, as the agent's"
Enter
Wait
Type `omniglass audit-log list --channel omnibot --limit 3 | jq -c '.events[] | {actor_name, agent, channel, verb, resource}'`
Enter
Wait

Type "# And the token omnibot minted for the run is already revoked"
Enter
Wait
Type `omniglass session list | jq '[.sessions[] | select(.description == "omnibot session")] | length'`
Enter
Wait
