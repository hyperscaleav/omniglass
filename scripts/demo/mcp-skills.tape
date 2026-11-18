# MCP skills: whole jobs an agent starts from
#
# The MCP server serves embedded skills as prompts beside its tools, so an
# agent starts a whole job (triage an alarm, troubleshoot a component) from one
# prompt instead of picking among a hundred tools. omniglass mcp skill lists
# them and prints one; every tool a skill names is a tool the server serves.

Type "# The skills the server serves as prompts, each a whole job"
Enter
Wait
Type `omniglass mcp skill | jq -c '.[]'`
Enter
Wait

Type "# One skill: its arguments, then the steps an agent follows"
Enter
Wait
Type `omniglass mcp skill triage-alarm | sed -n '1,22p'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# Every tool a skill names is one the server serves"
Enter
Wait
Type `named=$(omniglass mcp skill triage-alarm | grep -o 'tool:[a-z-]*' | sed 's/tool://' | sort -u)`
Enter
Wait
Type `echo "$named" | wc -l`
Enter
Wait
Type `omniglass mcp tools | jq -r '.[].name' | sort | comm -13 - <(echo "$named") | wc -l`
Enter
Wait

Type "# A skill the registry lacks is refused, naming the ones it has"
Enter
Wait
Type `omniglass mcp skill no-such-skill || true`
Enter
Wait
