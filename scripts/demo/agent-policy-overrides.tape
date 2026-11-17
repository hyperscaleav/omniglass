# Role and tool overrides narrow the agent policy
#
# On top of the install's per-class policy, an admin can narrow it for the
# agents of anyone granted a role, or for one operation. Each override only
# makes an agent stricter: the strictest of the class, the person's roles and
# the tool wins, and the policy and every refusal name the layer that set it
# (#2088).

Hide
Type `demo_hush 'omniglass principal create --username ops --email ops@example.com && omniglass principal grant create ops --role operator --scope-kind all'`
Enter
Wait
Type `demo_hush 'ops=$(OMNIGLASS_TOKEN=$(omniglass principal impersonate ops --mode act_as | jq -r .token) omniglass auth create-token --description "ops agent" --agent claude-code --ttl-days 7 | jq -r .token)'`
Enter
Wait
Type `demo_hush 'mine=$(omniglass auth create-token --description "admin agent" --agent claude-code --ttl-days 7 | jq -r .token)'`
Enter
Wait
Type "clear"
Enter
Wait
Show

Type "# Operators' agents may not write; the override names the role"
Enter
Wait
Type `omniglass setting update agent --body '{"roles":{"operator":{"write":"never"}}}' | jq -c .values.agent.roles`
Enter
Wait

Type "# The operator's agent resolves write to never, from the role; the admin's agent still asks"
Enter
Wait
Type `OMNIGLASS_TOKEN="$ops" omniglass auth agent-policy | jq -c '.classes[] | select(.class=="write") | {policy, source, role}'`
Enter
Wait
Type `OMNIGLASS_TOKEN="$mine" omniglass auth agent-policy | jq -c '.classes[] | select(.class=="write") | {policy, source}'`
Enter
Wait

Type "# So the server refuses the operator's agent, and runs the admin's"
Enter
Wait
Type `OMNIGLASS_TOKEN="$ops" omniglass command-type create --name override-demo --label "Override demo" 2>&1 || true`
Enter
Wait
Type `OMNIGLASS_TOKEN="$mine" omniglass command-type create --name override-demo --label "Override demo" | jq -c '{name, label}'`
Enter
Wait

Type "# One operation refused for every agent, while its class still runs"
Enter
Wait
Type `omniglass setting update agent --body '{"tools":{"delete-command-type":"never"}}' | jq -c .values.agent.tools`
Enter
Wait
Type `OMNIGLASS_TOKEN="$mine" omniglass command-type delete override-demo 2>&1 || true`
Enter
Wait
Type `OMNIGLASS_TOKEN="$mine" omniglass command-type update override-demo --label "Still editable" | jq -c '{name, label}'`
Enter
Wait
Type `OMNIGLASS_TOKEN="$mine" omniglass auth agent-policy | jq -c '.tools[]'`
Enter
Wait

Type "# A typo cannot hide in the policy: an unknown tool is refused when written"
Enter
Wait
Type `omniglass setting update agent --body '{"tools":{"delete-comand-type":"never"}}' 2>&1 || true`
Enter
Wait

Type "# Both refusals are in the audit trail, each under its agent"
Enter
Wait
Type `omniglass audit-log list --verb agent_refused | jq -c '.events[] | {actor_name, agent, resource_id}'`
Enter
Wait

Type "# Back to the defaults"
Enter
Wait
Type `omniglass setting delete agent >/dev/null && omniglass command-type delete override-demo && echo restored`
Enter
Wait
