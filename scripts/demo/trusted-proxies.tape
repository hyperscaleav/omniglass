# The client address believes only a trusted proxy
#
# A client behind the stack's network posts with a forged X-Forwarded-For whose leftmost hop claims
# to be someone else. The server believes the header only because the peer (the stack's own network)
# is in its trusted-proxy list, and even then reads it from the right: the token minted and the
# stranger's webhook both record the rightmost hop outside the list, never the forged leftmost one.
# The boot log names the list the server believes (OMNIGLASS_TRUSTED_PROXIES, #1185).

Type "# The server says at boot which proxies it believes a forwarded header from"
Enter
Wait
Type `grep -o 'trusted proxies.*' /stack/logs/server.log | tail -1`
Enter
Wait
Type "# Mint a token with a forged leftmost hop (203.0.113.66) ahead of the real client (198.51.100.7)"
Enter
Wait
Type `curl -s -X POST "$OMNIGLASS_SERVER/api/v1/auth/me/tokens" -H "Authorization: Bearer $OMNIGLASS_TOKEN" -H 'Content-Type: application/json' -H 'X-Forwarded-For: 203.0.113.66, 198.51.100.7' -d '{"description":"behind-the-proxy"}' | jq -c '{description, prefix}'`
Enter
Wait
Type `omniglass session list | jq -c '.sessions[] | select(.description == "behind-the-proxy") | {kind, description, client_ip}'`
Enter
Wait
Sleep 2s
Type "# A stranger's webhook through the same proxy: its inbox row is the same client"
Enter
Wait
Type `curl -s -o /dev/null -w '%{http_code}\n' -X POST "$OMNIGLASS_SERVER/api/v1/webhooks/AAAAAAAAAAAAAAAAAAAAAA" -H 'X-Forwarded-For: 203.0.113.66, 198.51.100.7' -d '{"event":"proxy-probe"}'`
Enter
Wait
Hide
Type `demo_until 20 'omniglass unclaimed-source list | jq -e "[.sources[] | select(.last_message | contains(\"proxy-probe\"))] | length > 0"'`
Enter
Wait@30s
Show
Type `omniglass unclaimed-source list | jq -c '.sources[] | select(.last_message | contains("proxy-probe")) | {transport, source, count}'`
Enter
Wait
Sleep 4s
