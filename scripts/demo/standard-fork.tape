# A standard forks from a template
#
# The boot seed plants no standard any more: a standard is forked from a
# template in the catalog, into a row the operator owns outright, and nothing
# points back at the template (#1324). This lab's dev fleet forked what its
# systems conform to; fork one more under a name of our own.

Type "# Fork the meeting-room template into a standard of our own"
Enter
Wait
Type `omniglass standard fork --template meeting-room --name hq-meeting --label 'HQ Meeting' | jq -c '{name, label, official}'`
Enter
Wait
Type "# It carries the template's roles, written in the same transaction"
Enter
Wait
Type `omniglass standard role list hq-meeting | jq -r '.roles[] | "\(.name) accepts \(.accepted_types | join(","))"'`
Enter
Wait
Type "# The template's name lives in the audit detail, and nowhere else"
Enter
Wait
Type `omniglass audit-log list --verb fork | jq -c '.events[0] | {verb, resource, template: .new.template, name: .new.Name}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# The same name again is taken"
Enter
Wait
Type `omniglass standard fork --template meeting-room --name hq-meeting 2>/dev/null | jq -r .detail || true`
Enter
Wait
Type "# A variant hangs off a standard we hold, or is refused naming what to fork first"
Enter
Wait
Type `omniglass standard fork --template mr55 --name hq-mr55 --parent hq-meeting | jq -c '{name, parent_standard}'`
Enter
Wait
Type `omniglass standard fork --template ds75 --name lobby-sign --parent nowhere 2>/dev/null | jq -r .detail || true`
Enter
Wait
Type "# Nothing points back at the template, so the standard is ours to rename"
Enter
Wait
Type `omniglass standard rename hq-meeting --name hq-boardroom | jq -c '{name, label}'`
Enter
Wait
