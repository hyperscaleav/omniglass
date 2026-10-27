# An MCP server whose tools are generated from the API
#
# omniglass mcp serves the API's MCP tools over stdio for a local agent, relaying
# to the server's /mcp with the user's token. A stdio MCP client (omniglass mcp
# tools and call) lists the generated tools, reads a simulated panel's
# reachability, writes a secret, and the audit row shows the write as the
# user's own, arrived over MCP. No uuid is typed (#1162).

Type "# The tools, generated from the API, each saying what a client may assume of it"
Enter
Wait
Type `omniglass mcp tools | jq length`
Enter
Wait
Type `omniglass mcp tools | jq -c '.[] | select(.name | test("^(get-component-reachability|create-secret|update-endpoint|issue-component-command)$")) | {name, read_only, destructive}'`
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
Type "# A write tool: a secret, created through the stdio client with the user's token"
Enter
Wait
Type `omniglass mcp call create-secret --args '{"name":"mcp-server-tape-key","secret_type":"api-key","owner_kind":"platform","fields":{"key":"demo-value"}}' | jq -c '{name, owner_kind, fields}'`
Enter
Wait
Type "# The same kind of write, made directly with the CLI"
Enter
Wait
Type `omniglass secret create --name mcp-server-tape-direct --owner-kind platform --secret-type api-key --fields '{"key":"demo-value"}' | jq -c '{name, owner_kind}'`
Enter
Wait

Type "# The audit trail: both writes are the user's; the MCP one says how it arrived"
Enter
Wait
Type `omniglass audit-log list --resource secret --verb create --limit 2 | jq -c '.events[] | {secret: .new.name, actor_name, channel, client: (.client // "" | split("/")[0])}'`
Enter
Wait
