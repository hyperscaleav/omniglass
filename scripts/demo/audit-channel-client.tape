# The audit trail says how an action arrived
#
# Every audit row carries the channel a write came in on (a plain API call, or
# MCP) and the client that declared itself over MCP, beside the agent its token
# was minted for. An agent sets a tag over MCP; the audit list then filters to
# that channel and that client, and a plain API call stays apart as direct
# (#1178).

Type "# A person's agent, holding a token for the MCP channel only"
Enter
Wait
Type `agent=$(omniglass auth create-token --description "audit demo agent" --agent claude-code --ttl-days 1 --channels '["mcp"]' | jq -r .token)`
Enter
Wait

Type "# The agent tags a video bar over MCP, through the CLI's own MCP client"
Enter
Wait
Type `OMNIGLASS_TOKEN="$agent" omniglass mcp call set-component-tag --args '{"name":"hq.west.level-4.meeting-405.$comp.videobar-1","key":"asset-id","value":"AV-0415"}' | jq -c '{key, value}'`
Enter
Wait

Type "# The person corrects the asset id themselves, over the API"
Enter
Wait
Type `omniglass component setTag 'hq.west.level-4.meeting-405.$comp.videobar-1' --key asset-id --value AV-0416 | jq -c '{key, value}'`
Enter
Wait

Type "# Filter the trail to what arrived over MCP: the channel, the client it declared, the agent"
Enter
Wait
Type `omniglass audit-log list --channel mcp --limit 3 | jq -c '.events[] | {actor_name, agent, channel, client: (.client | sub("/.*"; "/...")), verb, resource}'`
Enter
Wait

Type "# Or by the client's name, case-insensitively"
Enter
Wait
Type `omniglass audit-log list --client OMNIGLASS-CLI --limit 3 | jq -c '.events[] | {channel, verb, resource}'`
Enter
Wait

Type "# A plain API call has no channel: filter it as direct"
Enter
Wait
Type `omniglass audit-log list --channel direct --resource tag_binding --limit 1 | jq -c '.events[] | {actor_name, channel, verb, resource}'`
Enter
Wait

Type "# And an unknown channel is refused, not silently empty"
Enter
Wait
Type `omniglass audit-log list --channel smoke-signal | jq -c '{status, errors: [.errors[].message]}' || true`
Enter
Wait
