# A group is told in the console inbox, each member only what they can read
#
# The on-call group becomes a notification target on the inbox channel, and an action rule pages it
# on anything high or worse. Two alarms open: one on the depot's video bar, one on Touch Panel 2.
# dana reads the whole fleet and is told about both; eli reads Touch Panel 2 alone and is never
# told about the video bar. The recipient's name is withheld from anyone but them and an admin, a
# lost grant empties the inbox entry at once, and the group cannot be deleted while a target names
# it (#1341, #1352).

Hide
Type `demo_hush 'omniglass principal create --username dana --email dana@example.com'`
Enter
Wait
Type `demo_hush 'omniglass principal grant create dana --role viewer --scope-kind all'`
Enter
Wait
Type `demo_hush 'omniglass principal create --username eli --email eli@example.com'`
Enter
Wait
Type `demo_hush 'omniglass principal grant create eli --role viewer --scope-kind component --scope-id panel-2'`
Enter
Wait
Type `demo_hush 'omniglass principal-group create --name on-call'`
Enter
Wait
Type `demo_hush 'omniglass principal-group member create on-call --principal-id dana'`
Enter
Wait
Type `demo_hush 'omniglass principal-group member create on-call --principal-id eli'`
Enter
Wait
Type "clear"
Enter
Wait
Show

Type "# The on-call group: dana reads the whole fleet, eli reads Touch Panel 2 alone"
Enter
Wait
Type `for u in dana eli; do omniglass principal get "$u" | jq -c '{user: .human.username, grants: [.grants[] | {role, scope_kind}]}'; done`
Enter
Wait
Type "# The group becomes a notification target in the console inbox, paged on any alarm raised by hand, high or worse"
Enter
Wait
Type `omniglass notification-target create --name on-call-page --kind principal_group --principal-group on-call --channels '["inbox"]' | jq -c '{name, kind, principal_group, channels}'`
Enter
Wait
Type `omniglass action-rule create --name page-on-call --on '["raised"]' --when 'severity_rank >= 40 && rule == ""' --targets '["on-call-page"]' | jq -c '{name, on, when, targets}'`
Enter
Wait
Type "# Two alarms open: the depot's video bar, and Touch Panel 2"
Enter
Wait
Type `bar='depot.bay-1.$comp.videobar-1'`
Enter
Wait
Type `omniglass component alarm create "$bar" --severity high --message 'no video on HDMI 1' | jq -c '{component, severity, message}'`
Enter
Wait
Type `omniglass component alarm create panel-2 --severity high --message 'touch unresponsive' | jq -c '{component, severity, message}'`
Enter
Wait

Hide
Type `demo_hush 'dana=$(omniglass principal impersonate dana --mode act_as | jq -r .token)'`
Enter
Wait
Type `demo_hush 'eli=$(omniglass principal impersonate eli --mode act_as | jq -r .token)'`
Enter
Wait
Type "clear"
Enter
Wait
Show
Type "# dana reads both components, so both alarms reached her inbox"
Enter
Wait
Type `OMNIGLASS_TOKEN=$dana omniglass auth me notification list | jq -c '{unread, entries: [.notifications[] | {component, subject}]}'`
Enter
Wait
Type "# eli reads Touch Panel 2 alone: the video bar's alarm never reached him, on any channel"
Enter
Wait
Type `OMNIGLASS_TOKEN=$eli omniglass auth me notification list | jq -c '{unread, entries: [.notifications[] | {component, subject}]}'`
Enter
Wait
Type "# Who an entry was for is shown to that person and to admins, withheld from everyone else"
Enter
Wait
Type `OMNIGLASS_TOKEN=$eli omniglass notification list --target on-call-page | jq -c '.notifications[] | {component, recipient, recipient_withheld}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Type `demo_hush 'grant=$(omniglass principal get eli | jq -r ".grants[0].id")'`
Enter
Wait
Show
Type "# eli is moved from Touch Panel 2 to the video bar: the entry about the panel is gone at once"
Enter
Wait
Type `omniglass principal grant create eli --role viewer --scope-kind component --scope-id "$bar" | jq -c '{role, scope_kind}'`
Enter
Wait
Type `omniglass principal grant delete eli "$grant"`
Enter
Wait
Type `OMNIGLASS_TOKEN=$eli omniglass auth me notification list | jq -c '{unread, entries: [.notifications[] | {component}]}'`
Enter
Wait
Type "# The group cannot be deleted while a target notifies it; the refusal names the target"
Enter
Wait
Type `omniglass principal-group delete on-call 2>/dev/null | jq -r .detail || true`
Enter
Wait
