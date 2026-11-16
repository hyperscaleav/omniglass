# An MCP server whose tools are generated from the API
#
# omniglass mcp serves the API's MCP tools over stdio for a local agent, relaying
# to the server's /mcp with the user's token. A stdio MCP client (omniglass mcp
# tools and call) lists the generated tools, reads a simulated panel's
# reachability, finds the fleet's worst alarm in one read and acknowledges it,
# and the audit row shows the acknowledgement as the user's own, arrived over
# MCP. No tool carries a credential in plaintext. No uuid is typed (#1162,
# #2025).

Type "# The tools, generated from the API, each saying what a client may assume of it"
Enter
Wait
Type `omniglass mcp tools | jq length`
Enter
Wait
Type `omniglass mcp tools | jq -c '.[] | select(.name | test("^(get-component-reachability|list-alarms|acknowledge-component-alarm|update-endpoint|issue-component-command)$")) | {name, read_only, destructive}'`
Enter
Wait

Type "# A read tool over stdio: a simulated panel's reachability, as the route answers it"
Enter
Wait
Type `panel='hq.west.level-2.huddle.$comp.panel-1'`
Enter
Wait
Hide
Type `demo_until 120 'omniglass mcp call get-component-reachability --args "{\"name\":\"$panel\"}" | jq -e ".endpoints | length > 0"'`
Enter
Wait@150s
Show
Type `omniglass mcp call get-component-reachability --args "{\"name\":\"$panel\"}" | jq -c '.endpoints[] | {endpoint, transport, node}'`
Enter
Wait
Type "# A refusal is the route's own: the tool errors with its detail"
Enter
Wait
Type `omniglass mcp call get-component --args '{"name":"mcp-server-no-such-panel"}' | jq -r .detail || true`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# Something goes wrong on the panel"
Enter
Wait
Type `a=$(omniglass component alarm create "$panel" --severity disaster --message 'no video' --dedup-key mcp-demo | jq -r .id)`
Enter
Wait
Type "# The agent's one read: every active alarm it may see, worst first, without walking the fleet"
Enter
Wait
Type `omniglass mcp call list-alarms --args '{"unacknowledged":true}' | jq -c --arg a "$a" '.alarms[] | select(.id == $a) | {component, severity, message, acknowledged}'`
Enter
Wait
Type "# A write tool: it acknowledges the alarm through the route, with the user's token"
Enter
Wait
Type `omniglass mcp call acknowledge-component-alarm --args "{\"name\":\"$panel\",\"id\":\"$a\"}" | jq -c '{message, acknowledged, acknowledged_by}'`
Enter
Wait

Type "# The audit trail: the acknowledgement is the user's, and says how it arrived"
Enter
Wait
Type `omniglass audit-log list --resource alarm --verb acknowledge --limit 1 | jq -c '.events[] | {verb, actor_name, channel, client: (.client // "" | split("/")[0])}'`
Enter
Wait
Type "# A tool never carries a credential in plaintext: creating or revealing a secret is not one"
Enter
Wait
Type `omniglass mcp tools | jq -c '[.[].name] | {create_secret: index("create-secret"), reveal_secret: index("reveal-secret")}'`
Enter
Wait
