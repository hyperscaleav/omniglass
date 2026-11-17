# A system keeps its staffing when its standard changes
#
# A room built to one standard is staffed, then moved to another: the bar
# carries across to the new standard's role of the same name, the display's
# role is gone with the old standard so it is released, still a member and
# free to unbind, and the room cannot redeclare a name its standard declares.
# Then a technician who may edit the room, and read but not edit the fleet's
# components, is refused when it staffs a component outside its scope
# (#503, #506, #510).

Type "# Two standards: both declare a table mic, only v1 a room display"
Enter
Wait
Type `omniglass standard create --name staff-huddle-v1 --label "Huddle v1" >/dev/null && omniglass standard create --name staff-huddle-v2 --label "Huddle v2" | jq -c '{name}'`
Enter
Wait
Type `for s in v1 v2; do omniglass standard role update staff-huddle-$s table-mic --accepted-types '["video-bar"]' | jq -c '{name, accepted_types}'; done`
Enter
Wait
Type `omniglass standard role update staff-huddle-v1 room-display --accepted-types '["display"]' | jq -c '{name, accepted_types}'`
Enter
Wait

Type "# A room built to v1, staffed with a video bar and a display"
Enter
Wait
Type `omniglass system create --name staff-room --standard-id staff-huddle-v1 | jq -c '{name, standard}'`
Enter
Wait
Type `omniglass component create --name staff-bar --product kestrel-vroom >/dev/null && omniglass component create --name staff-display --product boreal-edge-55 | jq -c '{name, product}'`
Enter
Wait
Type `omniglass system role assignment update staff-room table-mic staff-bar && omniglass system role assignment update staff-room room-display staff-display`
Enter
Wait
Type `omniglass system role list staff-room | jq -c '.roles[] | {name, assigned_to}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# Move the room to v2: the bar carries across, the display's role left with v1"
Enter
Wait
Type `omniglass system update staff-room --standard-id staff-huddle-v2 | jq -c '{name, standard}'`
Enter
Wait
Type `omniglass system role list staff-room | jq -c '.roles[] | {name, from_standard, assigned_to}'`
Enter
Wait
Type "# The display is still a member with no role, so it is free to unbind"
Enter
Wait
Type `omniglass system member delete staff-room staff-display && omniglass system member list staff-room | jq -c '[.members[].component]'`
Enter
Wait
Type "# A role name means one role in a room: the room cannot redeclare its standard's"
Enter
Wait
Type `omniglass system role update staff-room table-mic --label "Second mic" 2>/dev/null | jq -r .detail || true`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# A technician who may edit this room, and read (not edit) every component"
Enter
Wait
Type `omniglass principal create --username staff-tech --password 'staffing-demo-passphrase' | jq -c '{username: .human.username}'`
Enter
Wait
Type `omniglass principal grant create staff-tech --role deploy --scope-kind system --scope-id staff-room | jq -c '{role, scope_kind}'`
Enter
Wait
Type `omniglass principal grant create staff-tech --role viewer --scope-kind all | jq -c '{role, scope_kind}'`
Enter
Wait
Type `omniglass component create --name staff-other-bar --product kestrel-vroom | jq -c '{name, product}'`
Enter
Wait
Hide
Type `demo_hush 'tech=$(omniglass principal impersonate staff-tech --mode act_as | jq -r .token)'`
Enter
Wait
Show
Type "# As the technician: the room itself is editable"
Enter
Wait
Type `OMNIGLASS_TOKEN=$tech omniglass system update staff-room --label "Huddle 4" | jq -c '{name, label}'`
Enter
Wait
Type "# but staffing writes onto the component too, and that one is outside its scope"
Enter
Wait
Type `OMNIGLASS_TOKEN=$tech omniglass system role assignment update staff-room table-mic staff-other-bar 2>/dev/null | jq -r .detail || true`
Enter
Wait
Type `omniglass component membership list staff-other-bar | jq -c '[.memberships[].system]'`
Enter
Wait
