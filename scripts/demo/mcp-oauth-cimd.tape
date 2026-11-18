# An MCP client signs in by its client id metadata document
#
# A client whose client_id is the https URL of its own metadata document needs
# no registration: the server fetches the document through the egress
# contract, checks it names itself, and sends the code only to a redirect URI
# it lists, on whatever loopback port the client is listening. Here the client
# is Claude Code, by the document it publishes. A URL on a private address is
# refused without a request to it (#2074).

Hide
Type `http() { python3 -c 'exec("import os,sys,urllib.request as u,urllib.error as e\na=sys.argv[1:]\nr=u.Request(os.environ[\"OMNIGLASS_SERVER\"]+a[1],data=a[2].encode() if len(a)>2 else None,method=a[0],headers={\"Content-Type\":a[3] if len(a)>3 else \"application/json\",\"Accept\":\"application/json, text/event-stream\"})\ntry:\n print(u.urlopen(r).read().decode())\nexcept e.HTTPError as x:\n print(\"HTTP\",x.code,x.read().decode());sys.exit(22)")' "$@"; }`
Enter
Wait
Type `authorize() { python3 -c 'exec("import os,sys,urllib.request as u,urllib.error as e,urllib.parse as p\nclass N(u.HTTPRedirectHandler):\n def redirect_request(*a): return None\nq=p.urlencode(dict(response_type=\"code\",client_id=sys.argv[1],redirect_uri=sys.argv[2],code_challenge=sys.argv[3],code_challenge_method=\"S256\",resource=os.environ[\"OMNIGLASS_SERVER\"]+\"/mcp\",state=\"s1\"))\ntry:\n u.build_opener(N).open(os.environ[\"OMNIGLASS_SERVER\"]+\"/oauth/authorize?\"+q)\nexcept e.HTTPError as x:\n l=x.headers.get(\"Location\");print(p.parse_qs(p.urlparse(l).query)[\"client_id\"][0] if l else \"HTTP %d %s\"%(x.code,x.read().decode().strip()))")' "$@"; }`
Enter
Wait
Type `pkce() { verifier=$(openssl rand -hex 32); challenge=$(printf %s "$verifier" | openssl dgst -sha256 -binary | basenc --base64url | tr -d =); }`
Enter
Wait
Type "clear"
Enter
Wait
Show

Type "# The server takes a metadata document URL as a client id"
Enter
Wait
Type `http GET /.well-known/oauth-authorization-server | jq -c '{client_id_metadata_document_supported}'`
Enter
Wait

Type "# Claude Code publishes its document; that URL is its client id"
Enter
Wait
Type `doc=https://claude.ai/oauth/claude-code-client-metadata; pkce`
Enter
Wait

Type "# It asks to sign in with no registration, listening on a port of its own"
Enter
Wait
Type `client=$(authorize "$doc" http://127.0.0.1:53682/callback "$challenge")`
Enter
Wait
Type "# The consent page shows its name, and the host it cannot choose"
Enter
Wait
Type `omniglass auth oauth-client get "$client" | jq -c '{client_name, metadata_host}'`
Enter
Wait

Type "# Its person approves viewer, and it exchanges the code by its URL"
Enter
Wait
Type `back=$(omniglass auth oauth-client authorize "$client" --approved --roles '["viewer"]' --redirect-uri http://127.0.0.1:53682/callback --code-challenge "$challenge" --code-challenge-method S256 --resource "$OMNIGLASS_SERVER/mcp" --state s1 | jq -r .redirect_to); code=$(jq -rn --arg u "$back" '$u | capture("code=(?<c>[^&]+)").c')`
Enter
Wait
Type `tok=$(http POST /oauth/token "grant_type=authorization_code&client_id=$(jq -rn --arg d "$doc" '$d|@uri')&code=$code&code_verifier=$verifier&redirect_uri=http%3A%2F%2F127.0.0.1%3A53682%2Fcallback" application/x-www-form-urlencoded); jq -c '{token_type, expires_in}' <<<"$tok"`
Enter
Wait
Type `OMNIGLASS_TOKEN=$(jq -r .access_token <<<"$tok") omniglass mcp tools | jq length`
Enter
Wait

Type "# A client id on a private address is refused, and never fetched"
Enter
Wait
Type `authorize https://127.0.0.1:8443/client.json http://127.0.0.1:53682/callback "$challenge"`
Enter
Wait
