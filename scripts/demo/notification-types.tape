# A notification type says what a message about an alarm says
#
# The two official notification types, read-only; an operator's own, whose template the sandbox
# refuses when it reaches for printf and keeps when it reads the alarm facts; and a preview that
# renders it against a real alarm on a Kestrel bar in the depot, exactly as a delivery would carry
# it, writing nothing (#618, #1319). No uuid is typed.

Type "# The official notification types: one for a raised alarm, one for a cleared one"
Enter
Wait
Type `omniglass notification-type list | jq -c '.notification_types[] | {name, official, subject}'`
Enter
Wait
Type "# Official means read-only, even to an admin"
Enter
Wait
Type `omniglass notification-type update alarm-raised --subject mine 2>/dev/null | jq -r .detail || true`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# An operator's own type: the sandbox refuses printf when the template is written"
Enter
Wait
Type `omniglass notification-type create --name page-oncall --subject '{{printf "%s" .Message}}' --text x 2>/dev/null | jq -r .detail || true`
Enter
Wait
Type `omniglass notification-type create --name page-oncall --label 'Page on-call' --subject '{{.SeverityLabel | upper}} {{.Component}}{{if .Place}} at {{.Place}}{{end}}' --text '{{.Message}} since {{.RaisedAt}}' | jq -c '{name, official}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# A bar in the depot raises an alarm"
Enter
Wait
Type `bar='depot.bay-1.$comp.videobar-1'`
Enter
Wait
Type `omniglass component alarm create "$bar" --severity high --message 'no video on HDMI 1' | jq -c '{component, severity, message}'`
Enter
Wait
Type "# Preview renders the type against that alarm, as a delivery would carry it, and writes nothing"
Enter
Wait
Type `omniglass notification-type preview page-oncall --component "$bar" | jq -c '{subject, text, transition}'`
Enter
Wait
Type "# The facts a template may read, every one present"
Enter
Wait
Type `omniglass notification-type preview page-oncall --component "$bar" | jq -c '[.facts[] | .name]'`
Enter
Wait
