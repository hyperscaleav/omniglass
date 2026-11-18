# An MCP client signs its person in with OAuth 2.1
#
# A remote MCP client meets a 401 at /mcp, discovers the authorization server
# from it, registers itself, and sends its person to consent with a PKCE
# challenge. The person approves one role; the client exchanges the code for
# an agent token linked to it, accepted on the MCP channel only. Its work is
# audited as the client's, and revoking the token signs the client out
# (#2048). The consent page is the console's; here the person answers it with
# the CLI, which calls the same route.

Hide
Type `http() { python3 -c 'exec("import os,sys,urllib.request as u,urllib.error as e\na=sys.argv[1:]\nr=u.Request(os.environ[\"OMNIGLASS_SERVER\"]+a[1],data=a[2].encode() if len(a)>2 else None,method=a[0],headers={\"Content-Type\":a[3] if len(a)>3 else \"application/json\",\"Accept\":\"application/json, text/event-stream\"})\ntry:\n print(u.urlopen(r).read().decode())\nexcept e.HTTPError as x:\n print(\"HTTP\",x.code);w=x.headers.get(\"WWW-Authenticate\");w and print(\"WWW-Authenticate:\",w);sys.exit(22)")' "$@"; }`
Enter
Wait
Type "clear"
Enter
Wait
Show

Type "# A client with no token meets a 401 that names where to learn about this server"
Enter
Wait
Type `http POST /mcp '{"jsonrpc":"2.0","id":1,"method":"initialize"}' || true`
Enter
Wait
Type `http GET /.well-known/oauth-protected-resource/mcp | jq -c '{resource, authorization_servers}'`
Enter
Wait
Type `http GET /.well-known/oauth-authorization-server | jq -c '{registration_endpoint, grant_types_supported, code_challenge_methods_supported}'`
Enter
Wait

Type "# It registers itself: a public client, a loopback redirect"
Enter
Wait
Type `client=$(http POST /oauth/register '{"client_name":"Claude Desktop","redirect_uris":["http://127.0.0.1:33418/callback"]}' | jq -r .client_id)`
Enter
Wait
Type "# Its PKCE pair: it keeps the verifier and sends only the challenge"
Enter
Wait
Type `verifier=$(openssl rand -hex 32); challenge=$(printf %s "$verifier" | openssl dgst -sha256 -binary | basenc --base64url | tr -d =)`
Enter
Wait

Type "# The person consents to one role (the console's consent page calls this same route)"
Enter
Wait
Type `omniglass auth oauth-client get "$client" | jq -c '{client_name, redirect_uris}'`
Enter
Wait
Type `back=$(omniglass auth oauth-client authorize "$client" --approved --roles '["viewer"]' --redirect-uri http://127.0.0.1:33418/callback --code-challenge "$challenge" --code-challenge-method S256 --resource "$OMNIGLASS_SERVER/mcp" --state s1 | jq -r .redirect_to)`
Enter
Wait
Type `code=$(jq -rn --arg u "$back" '$u | capture("code=(?<c>[^&]+)").c')`
Enter
Wait

Type "# The client exchanges the code and its verifier for a token linked to it"
Enter
Wait
Type `tok=$(http POST /oauth/token "grant_type=authorization_code&client_id=$client&code=$code&code_verifier=$verifier&redirect_uri=http%3A%2F%2F127.0.0.1%3A33418%2Fcallback&resource=$OMNIGLASS_SERVER/mcp" application/x-www-form-urlencoded | jq -r .access_token)`
Enter
Wait
Type `OMNIGLASS_TOKEN="$tok" omniglass mcp tools | jq length`
Enter
Wait
Type `OMNIGLASS_TOKEN="$tok" omniglass mcp call list-alarms --args '{"limit":1}' | jq -c '{alarms: (.alarms | length)}'`
Enter
Wait
Type "# It holds only the role approved, viewer cannot create; and its token is good on MCP only"
Enter
Wait
Type `OMNIGLASS_TOKEN="$tok" omniglass mcp call create-location --args '{"name":"oauth-demo","location_type":"campus"}' 2>/dev/null | jq -c '{status, detail}' || true`
Enter
Wait
Type `OMNIGLASS_TOKEN="$tok" omniglass command-type list 2>/dev/null | jq -c '{status, detail}' || true`
Enter
Wait

Type "# The person sees the client among their sessions, and revoking it signs the client out"
Enter
Wait
Type `omniglass session list | jq -c '.sessions[] | select(.oauth_client) | {agent, agent_roles}'`
Enter
Wait
Type `omniglass session revoke "$(omniglass session list | jq -r '.sessions[] | select(.oauth_client) | .id')" >/dev/null`
Enter
Wait
Type `OMNIGLASS_TOKEN="$tok" omniglass mcp tools 2>&1 | head -1 || true`
Enter
Wait
