# A telemetry sample names the namespace its key is read in
#
# Every catalog key has a namespace: og for the platform's, local for the operator's (#318). A
# pushed item may now name one (#1433). Omitted, the name walks the search path, local then og.
# Named, the name is read in that namespace only: og reaches the platform's row even where an
# operator's row of the same name would shadow it, and a namespace holding no such row rejects the
# item, saying which namespace it searched.

Type "# A panel of this demo's own to push for"
Enter
Wait
Type `omniglass component create --name ns-demo-panel --product generic-device --location harbor.pavilion-b.suite-b4 | jq -c '{name}'`
Enter
Wait
Type `c='harbor.pavilion-b.suite-b4.$comp.ns-demo-panel'`
Enter
Wait

Type "# room-temperature is the platform's (official), so it lives in og"
Enter
Wait
Type `omniglass metric-type get room-temperature | jq -c '{name, official}'`
Enter
Wait

Type "# One push, four items: bare, named og, and named in two namespaces that hold no room-temperature"
Enter
Wait
Type `omniglass telemetry push --owner "{\"kind\":\"component\",\"ref\":\"$c\"}" --metrics '[{"name":"room-temperature","value":21},{"namespace":"og","name":"room-temperature","value":22},{"namespace":"local","name":"room-temperature","value":23},{"namespace":"acme","name":"room-temperature","value":24}]' | jq -c '{accepted: .accepted.metrics, rejected}'`
Enter
Wait
Hide
Type `demo_until 30 'omniglass component metric sample list "$c" room-temperature | jq -e "[.samples[]] | length == 2"'`
Enter
Wait@40s
Type "clear"
Enter
Wait
Show

Type "# Both accepted samples landed on og's room-temperature; local and acme hold none, so nothing fell back"
Enter
Wait
Type `omniglass component metric sample list "$c" room-temperature | jq -c '[.samples[].value] | sort'`
Enter
Wait
