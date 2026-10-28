# An alarm transition queues notifications and a tick delivers them
#
# A receiver on the demo's own host stands in for the chat server, behind the ops-chat webhook
# target. An action rule pages it on any high-or-worse alarm. Unplugging a simulated touch panel
# makes the official reachability rule raise a High alarm, whose message the tick delivers signed.
# Then the chat server goes down as the panel comes back: the clear's message is retried, gives
# up when the target is switched off, and is retried by hand once both are back, under the same
# Idempotency-Key. The alarm's timeline shows every message beside the edges (#618, #1321).

Hide
Type `printf '%s\n' 'import hashlib, hmac, http.server, json' 'KEY = b"demo-618-signing-key"' 'class Rx(http.server.BaseHTTPRequestHandler):' '    def do_POST(self):' '        body = self.rfile.read(int(self.headers["Content-Length"]))' '        ts = self.headers.get("X-Omniglass-Timestamp", "")' '        want = "sha256=" + hmac.new(KEY, ts.encode() + b"." + body, hashlib.sha256).hexdigest()' '        code = 503 if open("/tmp/rx-mode").read().strip() == "down" else 200' '        env = json.loads(body)' '        seen = {"subject": env["subject"], "transition": env["transition"], "verified": hmac.compare_digest(want, self.headers.get("X-Omniglass-Signature", "")), "answered": code, "key": self.headers.get("Idempotency-Key")}' '        open("/tmp/rx.log", "a").write(json.dumps(seen) + "\n")' '        self.send_response(code)' '        self.end_headers()' '    def log_message(self, *args):' '        pass' 'http.server.ThreadingHTTPServer(("", 8618), Rx).serve_forever()' > /tmp/rx.py`
Enter
Wait
Type `echo up > /tmp/rx-mode`
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
Type `demo_hush 'python3 -c "import json, os, sys, urllib.request; r = urllib.request.Request(os.environ[\"OMNIGLASS_SERVER\"] + \"/api/v1/settings/notification\", data=json.dumps({\"egress_allow\": [sys.argv[1]]}).encode(), method=\"PATCH\", headers={\"Authorization\": \"Bearer \" + os.environ[\"OMNIGLASS_TOKEN\"], \"Content-Type\": \"application/json\"}); urllib.request.urlopen(r)" "$rx"'`
Enter
Wait
Type `demo_hush 'omniglass secret create --name ops-chat-signing --owner-kind platform --secret-type hmac-key --fields "{\"key\":\"demo-618-signing-key\"}"'`
Enter
Wait
Type `demo_hush 'omniglass notification-target create --name ops-chat --kind webhook --url "http://$rx:8618/hook" --secret ops-chat-signing'`
Enter
Wait
Type "clear"
Enter
Wait
Show

Type "# The ops-chat target, signed, and a receiver standing in for the chat server"
Enter
Wait
Type `omniglass notification-target list | jq -c '.notification_targets[] | {name, url, auth}'`
Enter
Wait
Type "# An action rule: page the chat on any alarm high or worse, when it opens and when it clears"
Enter
Wait
Type `omniglass action-rule create --name page-ops --on '["raised","cleared"]' --when 'severity_rank >= 40' --targets '["ops-chat"]' | jq -c '{name, on, when, targets}'`
Enter
Wait
Type "# Unplug Touch Panel 2: the official reachability rule raises a High alarm once the down holds 15s"
Enter
Wait
Type `omniglass component impairment set panel-2 --unplugged | jq -c '{unplugged}'`
Enter
Wait

Hide
Type `demo_until 120 'omniglass notification list --status sent | jq -e ".notifications | length > 0"'`
Enter
Wait@130s
Show
Type "# The raise queued a message in its own transaction, and the tick delivered it"
Enter
Wait
Type `omniglass notification list | jq -c '.notifications[] | {component, transition, target, status, subject}'`
Enter
Wait
Type "# The receiver got it, signed over the exact bytes it received"
Enter
Wait
Type `tail -1 /tmp/rx.log | jq -c '{subject, verified, answered}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# The chat server goes down as the panel is plugged back in: the clear's message is refused"
Enter
Wait
Type `echo down > /tmp/rx-mode`
Enter
Wait
Type `omniglass component impairment set panel-2 | jq -c '{unplugged}'`
Enter
Wait

Hide
Type `demo_until 60 'omniglass notification list --status queued | jq -e ".notifications[0].attempts >= 1"'`
Enter
Wait@70s
Show
Type "# Still queued: it is tried again 10s later, then 20s, doubling"
Enter
Wait
Type `omniglass notification list --status queued | jq -c '.notifications[] | {transition, status, attempts, last_status, last_error}'`
Enter
Wait
Type "# The target is switched off before the next attempt: the message gives up, saying why"
Enter
Wait
Type `omniglass notification-target update ops-chat --enabled=false | jq -c '{name, enabled}'`
Enter
Wait

Hide
Type `demo_until 60 'omniglass notification list --status failed | jq -e ".notifications | length > 0"'`
Enter
Wait@70s
Type `demo_hush 'id=$(omniglass notification list --status failed | jq -r ".notifications[0].id")'`
Enter
Wait
Show
Type `omniglass notification list --status failed | jq -c '.notifications[] | {transition, status, attempts, last_error}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# Both come back: the target on, the chat server up, and the failed message retried by hand"
Enter
Wait
Type `echo up > /tmp/rx-mode`
Enter
Wait
Type `omniglass notification-target update ops-chat --enabled=true | jq -c '{name, enabled}'`
Enter
Wait
Type `omniglass notification retry "$id" | jq -c '{transition, status, attempts}'`
Enter
Wait

Hide
Type `demo_until 30 'omniglass notification get "$id" | jq -e ".status == \"sent\""'`
Enter
Wait@40s
Show
Type "# Delivered: every attempt at the clear carried one Idempotency-Key, so the receiver can drop a repeat"
Enter
Wait
Type `jq -c 'select(.transition == "cleared") | {answered, verified}' /tmp/rx.log`
Enter
Wait
Type `jq -s '[.[] | select(.transition == "cleared") | .key] | unique | length' /tmp/rx.log`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Type `demo_hush 'alarm=$(omniglass component alarm list panel-2 --include-cleared | jq -r ".alarms[0].id")'`
Enter
Wait
Show
Type "# The alarm's timeline: the rule's edges, and every message about them"
Enter
Wait
Type `omniglass component alarm timeline panel-2 "$alarm" | jq -c '.entries[] | {kind, what, target, error}'`
Enter
Wait
