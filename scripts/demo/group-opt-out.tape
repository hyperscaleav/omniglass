# A member opts out of a group notification the admin allows
#
# The weekly-review group is paged on every warning. An admin may let members opt out of a rule;
# until then an opt-out is refused. Allowed, kim opts out and is told nothing by it while lee still
# is, and opting back in restores her (#1341, #1354).

Hide
Type `demo_hush 'for u in kim lee; do omniglass principal create --username $u && omniglass principal grant create $u --role viewer --scope-kind all; done'`
Enter
Wait
Type `demo_hush 'omniglass principal-group create --name weekly-review && omniglass principal-group member create weekly-review --principal-id kim && omniglass principal-group member create weekly-review --principal-id lee'`
Enter
Wait
Type `demo_hush 'omniglass notification-target create --name review-inbox --kind principal_group --principal-group weekly-review --channels "[\"inbox\"]"'`
Enter
Wait
Type `demo_hush 'kim=$(omniglass principal impersonate kim --mode act_as | jq -r .token)'`
Enter
Wait
Type `demo_hush 'lee=$(omniglass principal impersonate lee --mode act_as | jq -r .token)'`
Enter
Wait
Type "clear"
Enter
Wait
Show

Type "# A rule pages the weekly-review group about warnings"
Enter
Wait
Type `omniglass action-rule create --name review-warnings --on '["raised"]' --when 'message == "fan noise"' --targets '["review-inbox"]' | jq -c '{name, targets, opt_out_allowed}'`
Enter
Wait
Type "# Opting out is refused until an admin allows it on the rule"
Enter
Wait
Type `OMNIGLASS_TOKEN=$kim omniglass action-rule optOut review-warnings 2>/dev/null | jq -r .detail || true`
Enter
Wait
Type `omniglass action-rule update review-warnings --opt-out-allowed=true | jq -c '{name, opt_out_allowed}'`
Enter
Wait
Type `OMNIGLASS_TOKEN=$kim omniglass action-rule optOut review-warnings | jq -c .`
Enter
Wait
Type `omniglass component alarm create panel-2 --severity warning --message 'fan noise' | jq -c '{component, severity, message}'`
Enter
Wait
Type "# kim opted out and was told nothing; lee was"
Enter
Wait
Type `for t in "$kim" "$lee"; do OMNIGLASS_TOKEN=$t omniglass auth me notification list | jq -c '{unread}'; done`
Enter
Wait
Type "# Opting back in restores her from the next alarm"
Enter
Wait
Type `OMNIGLASS_TOKEN=$kim omniglass action-rule optIn review-warnings | jq -c .`
Enter
Wait
