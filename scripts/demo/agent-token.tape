# An agent token acts for its person and never reveals
#
# A person mints a token for an AI agent: named, expiring, accepted on the
# channels they choose. The agent works with the person's grants, but it can
# never reveal a secret or mint another token. It can bind a secret it never
# reads to a target (secret:bind, #2047), and every change it makes is
# audited as the person's, taken through that agent (#1179).

Hide
Type `demo_hush 'omniglass secret create --name agent-demo-login --owner-kind platform --secret-type basic-auth --fields "{\"username\":\"admin\",\"password\":\"hunter2\"}"'`
Enter
Wait
Type `demo_hush 'omniglass secret create --name agent-demo-signing --owner-kind platform --secret-type hmac-key --fields "{\"key\":\"s3cr3t-signing-key\"}"'`
Enter
Wait
Show

Type "# Mint a token for the agent: a name, a required expiry, the API channel only"
Enter
Wait
Type `minted=$(omniglass auth create-token --description "claude code on my laptop" --agent claude-code --ttl-days 7 --channels '["api"]')`
Enter
Wait
Type `jq -c '{agent, channels}' <<<"$minted"; agent=$(jq -r .token <<<"$minted")`
Enter
Wait

Type "# The agent knows who it acts for"
Enter
Wait
Type `OMNIGLASS_TOKEN="$agent" omniglass auth me | jq -c '{agent, acting_for: .human.username}'`
Enter
Wait

Type "# It works with its person's grants"
Enter
Wait
Type `OMNIGLASS_TOKEN="$agent" omniglass command-type create --name agent-demo-restart --label "Restart (agent demo)" | jq -c '{name, label}'`
Enter
Wait

Type "# But it can never decrypt a credential, nor mint itself another token"
Enter
Wait
Type `OMNIGLASS_TOKEN="$agent" omniglass secret reveal agent-demo-login 2>&1 || true`
Enter
Wait
Type `OMNIGLASS_TOKEN="$agent" omniglass auth create-token --description child 2>&1 || true`
Enter
Wait

Type "# Yet it can hand a credential to a target without ever reading it: that takes secret:bind"
Enter
Wait
Type `OMNIGLASS_TOKEN="$agent" omniglass notification-target create --name agent-demo-hook --kind webhook --url https://hooks.example.com/ops --secret agent-demo-signing | jq -c '{name, kind, secret}'`
Enter
Wait

Type "# The audit names the person as actor and the agent beside them"
Enter
Wait
Type `omniglass audit-log list --agent claude-code | jq -c '.events[] | {actor_name, agent, verb, resource}'`
Enter
Wait
