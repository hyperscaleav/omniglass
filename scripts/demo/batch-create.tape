# Locations, systems and components each take a batch create
#
# omniglass location batchCreate (POST /locations:batchCreate) takes a list of
# the single create's own bodies and lands them in one transaction, in order,
# every row the ordinary create in the caller's scope. --validate-only rolls it
# back and reports what it would make; a write returns the batch id every audit
# row it wrote carries, and a systems batch then lands at those locations by
# name. A refused row refuses the whole batch, named by its position (#2368).

Type "# A floor under the West Building, and a room on it named by the same batch"
Enter
Wait
Type `LOCS='[{"name":"level-9","location_type":"floor","parent":"hq.west"},{"name":"room-901","location_type":"room","parent":"hq.west.level-9"}]'`
Enter
Wait
Type "# A dry run: what the write would make, and nothing written"
Enter
Wait
Type `omniglass location batchCreate --validate-only --requests "$LOCS" | jq -c '{validate_only, locations: [.locations[] | {name, parent}]}'`
Enter
Wait
Type `omniglass location get hq.west.level-9 2>/dev/null | jq -r .detail || true`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# The write lands both, under one batch id"
Enter
Wait
Type `omniglass location batchCreate --requests "$LOCS" | jq -c '{has_batch: (.batch_id | length > 0), locations: [.locations[] | {name, parent}]}'`
Enter
Wait
Type "# Then a systems batch at those locations, by name"
Enter
Wait
Type `omniglass system batchCreate --requests '[{"system_type_id":"meeting","location":"hq.west.level-9.room-901"},{"system_type_id":"meeting","location":"hq.west.level-9"}]' | jq -c '{has_batch: (.batch_id | length > 0), systems: [.systems[] | {name, location}]}'`
Enter
Wait
Type "# A bad row refuses the whole batch, named by its position"
Enter
Wait
Type `omniglass location batchCreate --requests '[{"name":"level-10","location_type":"floor","parent":"hq.west"},{"name":"oops","location_type":"no-such-type","parent":"hq.west"}]' 2>/dev/null | jq -r .detail || true`
Enter
Wait
Type `omniglass location get hq.west.level-10 2>/dev/null | jq -r .detail || true`
Enter
Wait
