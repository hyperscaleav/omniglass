# Reconciliation resolves every instance of a property
#
# A display declared to show HDMI 1 has two receivers reporting the input each one sees, each
# naming its instance. The reconciliation read resolves every observed instance, worst case:
# drift while any receiver disagrees with the declaration, the value shown naming the instance
# it came from, and no drift once every receiver agrees (#456, ADR-0456).

Type "# A display of this demo's own in Suite B4, declared to show HDMI 1"
Enter
Wait
Type `omniglass component create --name dual-rx-display --product generic-device --location harbor.pavilion-b.suite-b4 | jq -c '{name, product}'`
Enter
Wait
Type `d='harbor.pavilion-b.suite-b4.$comp.dual-rx-display'`
Enter
Wait
Type `omniglass component property update "$d" video-input --value '"hdmi1"' | jq -c '{property: .property_type_name, value}'`
Enter
Wait
Sleep 2s

Type "# Receiver rx-2 reports HDMI 3, naming itself as the instance"
Enter
Wait
Type `omniglass telemetry push --owner "{\"kind\":\"component\",\"ref\":\"$d\"}" --properties '[{"name":"video-input","value":"hdmi3","instance":"rx-2"}]' | jq -c .accepted`
Enter
Wait
Hide
Type `demo_until 15 'omniglass component reconciliation list "$d" | jq -e ".properties[] | select(.property == \"video-input\") | .is == \"hdmi3\""'`
Enter
Wait@30s
Show
Type "# want and is disagree: drift, and is names the instance it came from"
Enter
Wait
Type `omniglass component reconciliation list "$d" | jq -c '.properties[] | select(.property == "video-input") | {want, is, is_instance, drift}'`
Enter
Wait
Sleep 3s

Hide
Type "clear"
Enter
Wait
Show
Type "# Receiver rx-1 reports HDMI 1: one instance agrees, rx-2 still does not, so drift holds"
Enter
Wait
Type `omniglass telemetry push --owner "{\"kind\":\"component\",\"ref\":\"$d\"}" --properties '[{"name":"video-input","value":"hdmi1","instance":"rx-1"}]' | jq -c .accepted`
Enter
Wait
Hide
Type `demo_until 15 '[ "$(omniglass component property sample list "$d" video-input | jq "[.samples[] | select(.instance == \"rx-1\")] | length")" -gt 0 ]'`
Enter
Wait@30s
Show
Type `omniglass component reconciliation list "$d" | jq -c '.properties[] | select(.property == "video-input") | {want, is, is_instance, drift}'`
Enter
Wait
Sleep 2s
Type "# rx-2 switches to HDMI 1 too: every instance agrees, and drift clears"
Enter
Wait
Type `omniglass telemetry push --owner "{\"kind\":\"component\",\"ref\":\"$d\"}" --properties '[{"name":"video-input","value":"hdmi1","instance":"rx-2"}]' | jq -c .accepted`
Enter
Wait
Hide
Type `demo_until 15 'omniglass component reconciliation list "$d" | jq -e ".properties[] | select(.property == \"video-input\") | .drift == false"'`
Enter
Wait@30s
Show
Type `omniglass component reconciliation list "$d" | jq -c '.properties[] | select(.property == "video-input") | {want, is, is_instance, drift}'`
Enter
Wait
Sleep 4s
