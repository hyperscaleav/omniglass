# A member outside a slot degrades its system
#
# A hand-built system has no standard, so no slots: until #2459 an alarm on its component left the
# system and every place above it healthy. A technician builds one in Bay 5 with one amplifier, powers the
# amplifier off with a high alarm, and reads the system and the place: degraded, never outage, with
# the member, its alarm and the reason named. Clearing the alarm brings both back (ADR-2459).

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
Type "# The system reads degraded, and says why: the member is outside a slot"
Enter
Wait
Type `omniglass system health list bay-5-rack | jq -c '{health, reason, members: [.members[] | {name, health, outside_slot, alarms: [.alarms[].message]}]}'`
Enter
Wait
Type "# The place above rolls it up unchanged"
Enter
Wait
Type `omniglass location health list bay-5 | jq -c '{health, systems: [.systems[] | select(.name == "bay-5-rack")]}'`
Enter
Wait
Type "# Power back: the alarm clears and both recover"
Enter
Wait
Type `omniglass component alarm delete bay-5-amp "$a" && omniglass system health list bay-5-rack | jq -c '{health, edges: [.transitions[].health]}'`
Enter
Wait
