# A credential handed to omnibot never reaches the model
#
# In its chat, /secret hands omnibot a credential: the terminal reads the value
# without echo, the CLI creates the secret with the person's own token, and the
# model is told only the secret's name. A recorded model stands in for a live
# one; it binds the secret by that name to a new SNMP endpoint, asking first as
# a credential destination does. The audit trail files the secret under the
# person and the bind under omnibot, and the value is nowhere the model could
# read it (#2112).

Hide
Type `demo_hush "printf '%s' '{\"turns\":[{\"tool_calls\":[{\"name\":\"create-endpoint\",\"arguments\":{\"component\":\"hq.west.level-4.meeting-405.\$comp.videobar-1\",\"driver\":\"snmp-generic\",\"inputs\":{\"host\":\"192.0.2.45\",\"community\":\"room-405-community\"}}}]},{\"text\":\"The meeting 405 video bar now polls over SNMP with room-405-community.\"}]}' > /tmp/bind.json"`
Enter
Wait
Show

Type "# Open an omnibot chat; /secret reads the value without echo"
Enter
Wait
Type `omniglass omnibot --replay /tmp/bind.json`
Enter
Wait+Screen /never read\./
Type "/secret room-405-community snmp-community"
Enter
Wait+Screen /community:/
Type "pub1ic-r0om-405"
Enter
Wait+Screen /not its value/
Type "poll the meeting 405 video bar over SNMP"
Enter
Wait+Screen /Run it\?/
Type "y"
Enter
Wait+Screen /now polls/
Type "/exit"
Enter
Wait

Type "# The secret is the person's, the bind is omnibot's"
Enter
Wait
Type `omniglass audit-log list --verb create --limit 2 | jq -c '.events[] | {actor_name, agent, verb, resource}'`
Enter
Wait

Type "# The endpoint holds the reference; the value never left the vault"
Enter
Wait
Type `omniglass endpoint get 'hq.west.level-4.meeting-405.$comp.videobar-1/snmp' | jq -c '{driver, inputs}'`
Enter
Wait
