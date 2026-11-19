# An install sets what an agent may do
#
# An admin sets always, ask or never for each class of operation an agent
# may call (read, write, destructive, credential destination). The defaults
# refuse nothing; a never class is refused by the server to every agent,
# audited as the agent's, while the person keeps their own reach (#2063).

Hide
Type `agent=$(omniglass auth create-token --description "policy demo" --agent claude-code --ttl-days 7 | jq -r .token)`
Enter
Wait
Show

Type "# The agent reads its policy: reads run, everything else asks its person first"
Enter
Wait
Type `OMNIGLASS_TOKEN="$agent" omniglass auth agent-policy | jq -c '{applies, classes: [.classes[] | "\(.class)=\(.policy)"]}'`
Enter
Wait

Type "# The admin rules out writes for every agent on this install"
Enter
Wait
Type `omniglass setting update agent --body '{"write":"never"}' >/dev/null && omniglass setting list | jq -c .values.agent`
Enter
Wait

Type "# The agent's write is refused by the server; its read still runs"
Enter
Wait
Type `OMNIGLASS_TOKEN="$agent" omniglass command-type create --name policy-demo --label "Policy demo" 2>&1 || true`
Enter
Wait
Type `OMNIGLASS_TOKEN="$agent" omniglass command-type list | jq 'length > 0'`
Enter
Wait

Type "# The person's own write is untouched"
Enter
Wait
Type `omniglass command-type create --name policy-demo --label "Policy demo" | jq -c '{name, label}'`
Enter
Wait

Type "# The refusal is in the audit trail, as the agent's"
Enter
Wait
Type `omniglass audit-log list --verb agent_refused | jq -c '.events[] | {actor_name, agent, verb, resource_id}'`
Enter
Wait

Type "# Back to the defaults"
Enter
Wait
Type `omniglass setting delete agent >/dev/null && OMNIGLASS_TOKEN="$agent" omniglass auth agent-policy | jq -c '[.classes[] | "\(.class)=\(.policy)"]'`
Enter
Wait
