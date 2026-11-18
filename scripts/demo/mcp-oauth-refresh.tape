# An OAuth-connected MCP client refreshes its short-lived token
#
# A client signs its person in with OAuth 2.1 and gets an access token good
# for an hour plus a refresh token. It refreshes without its person: the pair
# rotates, the old access token stops working and the same session row stays.
# Presenting a spent refresh token again means it leaked, so the grant ends,
# and the audit log shows each step as the client's (#2073).

Hide
Type `http() { python3 -c 'exec("import os,sys,urllib.request as u,urllib.error as e\na=sys.argv[1:]\nr=u.Request(os.environ[\"OMNIGLASS_SERVER\"]+a[1],data=a[2].encode() if len(a)>2 else None,method=a[0],headers={\"Content-Type\":a[3] if len(a)>3 else \"application/json\",\"Accept\":\"application/json, text/event-stream\"})\ntry:\n print(u.urlopen(r).read().decode())\nexcept e.HTTPError as x:\n print(\"HTTP\",x.code,x.read().decode());w=x.headers.get(\"WWW-Authenticate\");w and print(\"WWW-Authenticate:\",w);sys.exit(22)")' "$@"; }`
Enter
Wait
Type `pkce() { verifier=$(openssl rand -hex 32); challenge=$(printf %s "$verifier" | openssl dgst -sha256 -binary | basenc --base64url | tr -d =); }`
Enter
Wait
Type "clear"
Enter
Wait
Show

Type "# The server's metadata offers the refresh grant"
Enter
Wait
Type `http GET /.well-known/oauth-authorization-server | jq -c '{grant_types_supported}'`
Enter
Wait

Type "# A client registers for both grants, and its person consents to viewer"
Enter
Wait
Type `client=$(http POST /oauth/register '{"client_name":"Claude Desktop","redirect_uris":["http://127.0.0.1:33418/callback"],"grant_types":["authorization_code","refresh_token"]}' | jq -r .client_id); pkce`
Enter
Wait
Type `back=$(omniglass auth oauth-client authorize "$client" --approved --roles '["viewer"]' --redirect-uri http://127.0.0.1:33418/callback --code-challenge "$challenge" --code-challenge-method S256 --resource "$OMNIGLASS_SERVER/mcp" --state s1 | jq -r .redirect_to); code=$(jq -rn --arg u "$back" '$u | capture("code=(?<c>[^&]+)").c')`
Enter
Wait

Type "# The exchange: an access token for an hour, and a refresh token"
Enter
Wait
Type `first=$(http POST /oauth/token "grant_type=authorization_code&client_id=$client&code=$code&code_verifier=$verifier&redirect_uri=http%3A%2F%2F127.0.0.1%3A33418%2Fcallback" application/x-www-form-urlencoded)`
Enter
Wait
Type `jq -c '{token_type, expires_in, refresh_token: (.refresh_token | length > 0)}' <<<"$first"`
Enter
Wait

Type "# Later, the client refreshes on its own: a new pair, the session row unchanged"
Enter
Wait
Type `row=$(omniglass session list | jq -r '.sessions[] | select(.oauth_client) | .id')`
Enter
Wait
Type `second=$(http POST /oauth/token "grant_type=refresh_token&client_id=$client&refresh_token=$(jq -r .refresh_token <<<"$first")" application/x-www-form-urlencoded); jq -c '{expires_in, rotated: (.refresh_token != ($f | fromjson).refresh_token)}' --arg f "$first" <<<"$second"`
Enter
Wait
Type `[ "$(omniglass session list | jq -r '.sessions[] | select(.oauth_client) | .id')" = "$row" ] && echo "same session row"`
Enter
Wait
Type `OMNIGLASS_TOKEN=$(jq -r .access_token <<<"$second") omniglass mcp tools | jq length`
Enter
Wait
Type "# The old access token stopped working the moment it rotated"
Enter
Wait
Type `OMNIGLASS_TOKEN=$(jq -r .access_token <<<"$first") omniglass mcp tools 2>&1 | head -1 || true`
Enter
Wait

Type "# A spent refresh token presented again means a copy exists: the grant ends"
Enter
Wait
Type `http POST /oauth/token "grant_type=refresh_token&client_id=$client&refresh_token=$(jq -r .refresh_token <<<"$first")" application/x-www-form-urlencoded || true`
Enter
Wait
Type `http POST /oauth/token "grant_type=refresh_token&client_id=$client&refresh_token=$(jq -r .refresh_token <<<"$second")" application/x-www-form-urlencoded || true`
Enter
Wait
Type `omniglass session list | jq '[.sessions[] | select(.oauth_client)] | length'`
Enter
Wait

Type "# The trail: the rotation and the revoke, each taken by the client"
Enter
Wait
Type `omniglass audit-log list --agent "Claude Desktop" --resource credential | jq -c '.events[] | {actor_name, agent, verb}'`
Enter
Wait
