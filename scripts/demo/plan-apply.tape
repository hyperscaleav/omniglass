# A plan of locations, systems and components lands in one call
#
# omniglass plan apply (POST /plans:apply) takes a plan a client built and
# lands it in one transaction, every piece the ordinary create in the
# caller's scope. --validate-only rolls it back and reports what it would
# make; an apply returns the import id every audit row it wrote carries, and
# a refused piece names its path in the plan (#2368).

Type "# A floor under the West Building, with a meeting room staffed on it"
Enter
Wait
Type `LOCS='[{"key":"f9","parent":"hq.west","name":"level-9","type":"floor"}]'`
Enter
Wait
Type `SYS='[{"location_key":"f9","standard":"meeting-room","type":"meeting","components":[{"slot":"video-bar","product":"kestrel-vroom"},{"slot":"main-display","product":"boreal-edge-55"}]}]'`
Enter
Wait
Type "# A dry run: what the apply would make, and nothing written"
Enter
Wait
Type `omniglass plan apply --validate-only --standards '["meeting-room"]' --locations "$LOCS" --systems "$SYS" | jq -c '{validate_only, counts, locations: [.locations[].location]}'`
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
Type "# The apply lands it, under one import id"
Enter
Wait
Type `omniglass plan apply --standards '["meeting-room"]' --locations "$LOCS" --systems "$SYS" | jq -c '{has_import: (.import_id | length > 0), counts, systems: [.systems[] | {system, components: [.components[] | "\(.slot) \(.component)"]}]}'`
Enter
Wait
Type "# A bad piece refuses the whole plan, named by its path"
Enter
Wait
Type `omniglass plan apply --locations '[{"key":"f","parent":"hq.west","name":"level-10","type":"floor"}]' --components '[{"location_key":"f","product":"boreal-edge-55"},{"location_key":"f","product":"no-such-product"}]' 2>/dev/null | jq -r .detail || true`
Enter
Wait
Type `omniglass location get hq.west.level-10 2>/dev/null | jq -r .detail || true`
Enter
Wait
