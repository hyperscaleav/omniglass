# An alarm is emailed through the install's relay, proven against the mail sim
#
# `omniglass sim mail` stands in for the relay on the demo's own host: it keeps what it is sent and
# sends nothing on. The notification settings point at it. A shared mailbox is an email target; the
# on-call group is told by email too, each member at their directory address. The relay goes down:
# the emails are retried. While they wait, a member leaves the group, and their email is judged
# again at send and refused, while the other's is delivered when the relay comes back (#1317).

Hide
Type `echo up > /tmp/mail-mode`
Enter
Wait
Type `demo_hush 'nohup omniglass sim mail --listen 0.0.0.0:2525 --out /tmp/mail.jsonl --down-file /tmp/mail-mode >/dev/null 2>&1 &'`
Enter
Wait
Type `demo_hush 'rx=$(python3 -c "import socket; print(socket.gethostbyname(socket.gethostname()))")'`
Enter
Wait
Type `demo_until 20 'python3 -c "import socket; socket.create_connection((\"127.0.0.1\", 2525))"'`
Enter
Wait@30s
Type `demo_hush 'python3 -c "import json, os, sys, urllib.request; r = urllib.request.Request(os.environ[\"OMNIGLASS_SERVER\"] + \"/api/v1/settings/notification\", data=json.dumps({\"egress_allow\": [sys.argv[1]], \"mail_relay\": sys.argv[1] + \":2525\", \"mail_security\": \"none\", \"mail_from\": \"Omniglass <alerts@example.com>\"}).encode(), method=\"PATCH\", headers={\"Authorization\": \"Bearer \" + os.environ[\"OMNIGLASS_TOKEN\"], \"Content-Type\": \"application/json\"}); urllib.request.urlopen(r)" "$rx"'`
Enter
Wait
Type `demo_hush 'for u in fay gus; do omniglass principal create --username $u --email $u@example.com && omniglass principal grant create $u --role viewer --scope-kind all; done'`
Enter
Wait
Type `demo_hush 'omniglass principal-group create --name night-shift && omniglass principal-group member create night-shift --principal-id fay && omniglass principal-group member create night-shift --principal-id gus'`
Enter
Wait
Type "clear"
Enter
Wait
Show

Type "# The relay: a mail sim on this host, named in the egress allow list, so plain SMTP is admitted"
Enter
Wait
Type `omniglass setting list | jq -c '.values.notification | {mail_relay, mail_security, mail_from}'`
Enter
Wait
Type "# A shared mailbox is an email target; test sends one synthetic message through the relay"
Enter
Wait
Type `omniglass notification-target create --name ops-mailbox --kind email --addresses '["ops@example.com"]' | jq -c '{name, kind, addresses}'`
Enter
Wait
Type `omniglass notification-target test ops-mailbox | jq -c '{ok, status}'`
Enter
Wait
Type "# The night shift is told by email too, each member at their directory address"
Enter
Wait
Type `omniglass notification-target create --name night-page --kind principal_group --principal-group night-shift --channels '["email"]' | jq -c '{name, principal_group, channels}'`
Enter
Wait
Type `omniglass action-rule create --name mail-night --on '["raised"]' --when 'message == "no audio from the ceiling mics"' --targets '["night-page","ops-mailbox"]' | jq -c '{name, targets}'`
Enter
Wait
Type `omniglass component alarm create panel-2 --severity high --message 'no audio from the ceiling mics' | jq -c '{component, severity, message}'`
Enter
Wait

Hide
Type `demo_until 30 'test "$(grep -c "ceiling" /tmp/mail.jsonl)" -ge 3'`
Enter
Wait@40s
Show
Type "# The sim received the rendered subject and text, one message per recipient"
Enter
Wait
Type `jq -c 'select(.subject | test("ceiling")) | {to, subject}' /tmp/mail.jsonl`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# The relay goes down, and another alarm opens: its emails wait to be retried"
Enter
Wait
Type `echo down > /tmp/mail-mode`
Enter
Wait
Type `omniglass component alarm create "depot.bay-1.\$comp.videobar-1" --severity high --message 'no audio from the ceiling mics' | jq -c '{component, message}'`
Enter
Wait

Hide
Type `demo_until 60 'omniglass notification list --status queued | jq -e "[.notifications[] | select(.attempts >= 1)] | length >= 3"'`
Enter
Wait@70s
Show
Type `omniglass notification list --status queued | jq -c '.notifications[] | {channel, recipient, attempts, last_error}'`
Enter
Wait
Type "# While they wait, gus leaves the night shift; then the relay comes back"
Enter
Wait
Type `omniglass principal-group member delete night-shift gus`
Enter
Wait
Type `echo up > /tmp/mail-mode`
Enter
Wait

Hide
Type `demo_until 90 'omniglass notification list --status queued | jq -e ".notifications | length == 0"'`
Enter
Wait@100s
Show
Type "# Judged again at send: fay and the mailbox are emailed, gus is refused with the reason"
Enter
Wait
Type `omniglass notification list --target night-page | jq -c '.notifications[] | select(.attempts > 1 or .status == "failed") | {recipient, status, last_error}'`
Enter
Wait
