# The properties read reconciles every instance of a property
#
# A display declared to show HDMI 1 is observed through two of its endpoints, each reporting the
# input it sees and naming itself as the instance; a push naming an instance that is no endpoint
# of the display is refused. The display's properties read carries each property's
# reconciliation, resolving every observed instance worst case: drift while any endpoint
# disagrees with the declaration, the value shown naming the instance it came from, and no drift
# once every endpoint agrees (#456, ADR-0456). A property only observed is listed too, with
# nothing wanted and nothing to drift from (#1297).

Type "# A display of this demo's own in Suite B4, declared to show HDMI 1"
Enter
Wait
Type `omniglass component create --name two-path-display --product generic-device --location harbor.pavilion-b.suite-b4 | jq -c '{name, product}'`
Enter
Wait
Type `d='harbor.pavilion-b.suite-b4.$comp.two-path-display'`
Enter
Wait
Type `omniglass component property update "$d" video-input --value '"hdmi1"' | jq -c '{property: .property_type_name, value}'`
Enter
Wait
Type "# Two endpoints observe it: a webhook a cloud service posts to, and its ssh control port"
Enter
Wait
Type `omniglass endpoint create --component "$d" --transport webhook | jq -c '{name}'`
Enter
Wait
Type `omniglass endpoint create --component "$d" --transport ssh --params '{"target":"192.0.2.40:22"}' | jq -c '{name}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# An instance is the endpoint an observation came through: one naming no endpoint is refused"
Enter
Wait
Type `omniglass telemetry push --owner "{\"kind\":\"component\",\"ref\":\"$d\"}" --properties '[{"name":"video-input","value":"hdmi3","instance":"rx-2"}]' 2>/dev/null | jq -r .detail || true`
Enter
Wait

Type "# The webhook reports HDMI 3, naming itself as the instance"
Enter
Wait
Type `omniglass telemetry push --owner "{\"kind\":\"component\",\"ref\":\"$d\"}" --properties '[{"name":"video-input","value":"hdmi3","instance":"webhook"}]' | jq -c .accepted`
Enter
Wait
Hide
Type `demo_until 15 'omniglass component property list "$d" | jq -e ".properties[] | select(.property_type_name == \"video-input\") | .reconciliation.is == \"hdmi3\""'`
Enter
Wait@30s
Show
Type "# want and is disagree: drift, and is names the instance it came from"
Enter
Wait
Type `omniglass component property list "$d" | jq -c '.properties[] | select(.property_type_name == "video-input") | .reconciliation | {want, is, is_instance, drift}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# The ssh port reports HDMI 1: one instance agrees, the webhook still does not, so drift holds"
Enter
Wait
Type `omniglass telemetry push --owner "{\"kind\":\"component\",\"ref\":\"$d\"}" --properties '[{"name":"video-input","value":"hdmi1","instance":"ssh"}]' | jq -c .accepted`
Enter
Wait
Hide
Type `demo_until 15 '[ "$(omniglass component property sample list "$d" video-input | jq "[.samples[] | select(.instance == \"ssh\")] | length")" -gt 0 ]'`
Enter
Wait@30s
Show
Type `omniglass component property list "$d" | jq -c '.properties[] | select(.property_type_name == "video-input") | .reconciliation | {want, is, is_instance, drift}'`
Enter
Wait
Type "# The webhook reports HDMI 1 too: every instance agrees, and drift clears"
Enter
Wait
Type `omniglass telemetry push --owner "{\"kind\":\"component\",\"ref\":\"$d\"}" --properties '[{"name":"video-input","value":"hdmi1","instance":"webhook"}]' | jq -c .accepted`
Enter
Wait
Hide
Type `demo_until 15 'omniglass component property list "$d" | jq -e ".properties[] | select(.property_type_name == \"video-input\") | .reconciliation.drift == false"'`
Enter
Wait@30s
Show
Type `omniglass component property list "$d" | jq -c '.properties[] | select(.property_type_name == "video-input") | .reconciliation | {want, is, is_instance, drift}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# A property nothing declares is listed once it is observed: nothing wanted, so nothing drifts"
Enter
Wait
Type `omniglass telemetry push --owner "{\"kind\":\"component\",\"ref\":\"$d\"}" --properties '[{"name":"serial-number","value":"SN-4471","instance":"webhook"}]' | jq -c .accepted`
Enter
Wait
Hide
Type `demo_until 15 'omniglass component property list "$d" | jq -e ".properties[] | select(.property_type_name == \"serial-number\") | .reconciliation.is == \"SN-4471\""'`
Enter
Wait@30s
Show
Type `omniglass component property list "$d" | jq -c '.properties[] | select(.property_type_name == "serial-number") | {property: .property_type_name, value, reconciliation}'`
Enter
Wait
