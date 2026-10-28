# A webhook notification target, sent through the egress primitive
#
# A receiver on the demo's own host keeps every post and checks its signature with a shared key.
# An admin binds that key as a platform hmac-key secret and points a webhook target at the
# receiver: plain http to an internal address is refused until the egress allow list names it.
# Once it does, a test send arrives signed over the exact bytes received, and the URL reads masked
# until it is revealed under an audit row (#618, #1320). No uuid is typed.

Hide
Type `printf '%s\n' 'import hashlib, hmac, http.server, json, os' 'KEY = b"demo-618-signing-key"' 'class Rx(http.server.BaseHTTPRequestHandler):' '    def do_POST(self):' '        body = self.rfile.read(int(self.headers["Content-Length"]))' '        ts = self.headers.get("X-Omniglass-Timestamp", "")' '        want = "sha256=" + hmac.new(KEY, ts.encode() + b"." + body, hashlib.sha256).hexdigest()' '        code = 503 if os.path.exists("/tmp/rx-fail") else 200' '        env = json.loads(body)' '        seen = {"subject": env["subject"], "transition": env["transition"], "verified": hmac.compare_digest(want, self.headers.get("X-Omniglass-Signature", "")), "answered": code, "key": self.headers.get("Idempotency-Key")}' '        open("/tmp/rx.log", "a").write(json.dumps(seen) + "\n")' '        self.send_response(code)' '        self.end_headers()' '    def log_message(self, *args):' '        pass' 'http.server.ThreadingHTTPServer(("", 8618), Rx).serve_forever()' > /tmp/rx.py`
Enter
Wait
Type `demo_hush 'nohup python3 /tmp/rx.py >/dev/null 2>&1 &'`
Enter
Wait
Type `demo_hush 'rx=$(python3 -c "import socket; print(socket.gethostbyname(socket.gethostname()))")'`
Enter
Wait
Type `demo_until 20 'python3 -c "import socket; socket.create_connection((\"127.0.0.1\", 8618))"'`
Enter
Wait@30s
Type "clear"
Enter
Wait
Show

Type "# The receiver's signing key, as a platform secret an admin binds to a target"
Enter
Wait
Type `omniglass secret create --name ops-chat-signing --owner-kind platform --secret-type hmac-key --fields '{"key":"demo-618-signing-key"}' | jq -c '{name, secret_type}'`
Enter
Wait
Type "# The receiver is plain http on an internal address: refused until the egress allow list names it"
Enter
Wait
Type `omniglass notification-target create --name ops-chat --kind webhook --url "http://$rx:8618/hook" --secret ops-chat-signing 2>/dev/null | jq -r .detail || true`
Enter
Wait

Hide
Type `demo_hush 'python3 -c "import json, os, sys, urllib.request; r = urllib.request.Request(os.environ[\"OMNIGLASS_SERVER\"] + \"/api/v1/settings/notification\", data=json.dumps({\"egress_allow\": [sys.argv[1]]}).encode(), method=\"PATCH\", headers={\"Authorization\": \"Bearer \" + os.environ[\"OMNIGLASS_TOKEN\"], \"Content-Type\": \"application/json\"}); urllib.request.urlopen(r)" "$rx"'`
Enter
Wait
Type "clear"
Enter
Wait
Show
Type "# An admin names the receiver's address in notification.egress_allow (one install-wide setting)"
Enter
Wait
Type `omniglass setting list | jq -c '.values.notification'`
Enter
Wait
Type `omniglass notification-target create --name ops-chat --kind webhook --url "http://$rx:8618/hook" --secret ops-chat-signing | jq -c '{name, kind, url, auth}'`
Enter
Wait
Type "# A test send: the receiver answers, and checks the signature over the exact bytes it got"
Enter
Wait
Type `omniglass notification-target test ops-chat | jq -c '{ok, status}'`
Enter
Wait
Type `tail -1 /tmp/rx.log | jq -c '{subject, transition, verified}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# The URL reads masked; revealing it is its own permission and its own audit row"
Enter
Wait
Type `omniglass notification-target get ops-chat | jq -c '{name, url, enabled}'`
Enter
Wait
Type `omniglass notification-target reveal ops-chat | jq -c '{url}'`
Enter
Wait
Type `omniglass audit-log list --resource notification_target | jq -c '[.events[] | .verb]'`
Enter
Wait
