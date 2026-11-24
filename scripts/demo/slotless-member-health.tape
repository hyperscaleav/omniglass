# A system shows the alarms of members outside its slots
#
# A hand-built system has no standard, so no slots. A technician puts one amplifier in a system in
# Bay 5 and powers it off with a high alarm. The system's health read lists the amplifier, outside a
# slot, with its alarm, and still reads healthy, as does the place above it: only rules somebody
# declared, the slots and their impact, decide a system's health (ADR-2459). Clearing the alarm
# empties the list.

Hide
Type `demo_hush 'omniglass user create --username tech --email tech@example.com && omniglass user grant create tech --role admin --scope-kind all'`
Enter
Wait
Type `demo_hush 'export OMNIGLASS_TOKEN=$(omniglass user impersonate tech --mode act_as | jq -r .token)'`
Enter
Wait
Type "clear"
Enter
Wait
Show

Type "# A hand-built system in Bay 5, one amplifier in it and no slots"
Enter
Wait
Type `omniglass system create --name bay-5-rack --label "Bay 5 rack" --location bay-5 | jq -c '{name, standard}'`
Enter
Wait
Type `omniglass component create --name bay-5-amp --product generic-device --system bay-5-rack | jq -c '{name}'`
Enter
Wait
Type "# The amplifier powers off"
Enter
Wait
Type `a=$(omniglass component alarm create bay-5-amp --severity high --message 'amplifier powered off' --dedup-key power | jq -r .id)`
Enter
Wait
Type "# The system shows the alarm, marks the member outside a slot, and its health does not move"
Enter
Wait
Type `omniglass system health list bay-5-rack | jq -c '{health, reason, members: [.members[] | {name, health, outside_slot, alarms: [.alarms[].message]}]}'`
Enter
Wait
Type "# Nor does the place above it"
Enter
Wait
Type `omniglass location health list bay-5 | jq -c '{health, systems: [.systems[] | select(.name == "bay-5-rack")]}'`
Enter
Wait
Type "# Power back: the alarm clears"
Enter
Wait
Type `omniglass component alarm delete bay-5-amp "$a" && omniglass system health list bay-5-rack | jq -c '{health, members: [.members[] | {name, alarms: (.alarms | length)}]}'`
Enter
Wait
