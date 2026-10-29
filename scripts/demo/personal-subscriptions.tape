# A user subscribes themselves to the alarms they can see
#
# ivy reads Touch Panel 2 alone. She subscribes herself with a personal rule whose when is true, so
# it would match anything; it still only ever matches an alarm on a component she can read, judged
# before the when runs. She tests it against her own recent alarms first. An alarm on the depot's
# video bar never reaches her; one on Touch Panel 2 does. Her rule is hers alone: another user and
# the platform's rule list never see it (#1341, #1353).

Hide
Type `demo_hush 'omniglass principal create --username ivy --email ivy@example.com && omniglass principal grant create ivy --role viewer --scope-kind component --scope-id panel-2'`
Enter
Wait
Type `demo_hush 'omniglass principal create --username jon && omniglass principal grant create jon --role viewer --scope-kind all'`
Enter
Wait
Type `demo_hush 'ivy=$(omniglass principal impersonate ivy --mode act_as | jq -r .token)'`
Enter
Wait
Type `demo_hush 'jon=$(omniglass principal impersonate jon --mode act_as | jq -r .token)'`
Enter
Wait
Type `demo_hush 'omniglass component alarm create panel-2 --severity average --message "slow to wake"'`
Enter
Wait
Type "clear"
Enter
Wait
Show

Type "# ivy, a viewer of Touch Panel 2 alone, subscribes herself: every alarm, in her inbox"
Enter
Wait
Type `OMNIGLASS_TOKEN=$ivy omniglass auth me action-rule create --name everything --on '["raised"]' --when 'true' --channels '["inbox"]' | jq -c '{name, on, when, channels}'`
Enter
Wait
Type "# She tests a stricter draft against her own recent alarms before she saves it"
Enter
Wait
Type `OMNIGLASS_TOKEN=$ivy omniglass auth me action-rule test everything --when 'severity_rank >= 40' | jq -c '.matches[] | {component, severity, matched}'`
Enter
Wait
Type "# Two alarms open: the depot's video bar, which she cannot read, and Touch Panel 2"
Enter
Wait
Type `omniglass component alarm create 'depot.bay-1.$comp.videobar-1' --severity high --message 'lens cap on' | jq -c '{component, message}'`
Enter
Wait
Type `omniglass component alarm create panel-2 --severity high --message 'touch unresponsive' | jq -c '{component, message}'`
Enter
Wait
Type "# Even with a when of true, only Touch Panel 2 reached her"
Enter
Wait
Type `OMNIGLASS_TOKEN=$ivy omniglass auth me notification list | jq -c '[.notifications[] | {component, subject}]'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# Her rule is hers alone: jon cannot read it, and the platform's rule list does not show it"
Enter
Wait
Type `OMNIGLASS_TOKEN=$jon omniglass auth me action-rule get everything 2>/dev/null | jq -r .detail || true`
Enter
Wait
Type `omniglass action-rule list | jq -c '[.action_rules[].name]'`
Enter
Wait
Type "# She pauses it when she goes on leave"
Enter
Wait
Type `OMNIGLASS_TOKEN=$ivy omniglass auth me action-rule update everything --enabled=false | jq -c '{name, enabled}'`
Enter
Wait
