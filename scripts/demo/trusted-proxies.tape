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
Type `grep 'trusted proxies' /stack/logs/server.log | tail -1 | grep -o 'networks=[^ ]*'`
Enter
Wait
Hide
Type `demo_hush 'post_via_proxy() { local hp="${OMNIGLASS_SERVER#*://}"; hp="${hp%%/*}"; local h="${hp%:*}" p=80; if [[ $hp == *:* ]]; then p="${hp##*:}"; fi; exec 3<>"/dev/tcp/$h/$p"; printf "POST %s HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nX-Forwarded-For: %s\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s" "$1" "$hp" "$OMNIGLASS_TOKEN" "$2" "${#3}" "$3" >&3; head -1 <&3 | tr -d "\r"; exec 3<&-; }'`
Enter
Wait
Show
Type "# post_via_proxy sends a raw POST carrying the X-Forwarded-For a proxy would, from this terminal"
Enter
Wait
Type "# Mint a token with a forged leftmost hop (203.0.113.66) ahead of the real client (198.51.100.7)"
Enter
Wait
Type `post_via_proxy /api/v1/auth/me/tokens '203.0.113.66, 198.51.100.7' '{"description":"behind-the-proxy"}'`
Enter
Wait
Type `omniglass session list | jq -c '.sessions[] | select(.description == "behind-the-proxy") | {kind, description, client_ip}'`
Enter
Wait
Sleep 2s
Type "# A stranger's webhook through the same proxy: its inbox row is the same client"
Enter
Wait
Type `post_via_proxy /api/v1/webhooks/AAAAAAAAAAAAAAAAAAAAAA '203.0.113.66, 198.51.100.7' '{"event":"proxy-probe"}'`
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
